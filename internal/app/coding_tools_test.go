// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/taskstate"
)

func codingTestGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func codingTestRepo(t *testing.T, marker string) string {
	t.Helper()
	repo := t.TempDir()
	codingTestGit(t, repo, "init", "-b", "main")
	codingTestGit(t, repo, "config", "user.name", "Runtime Test")
	codingTestGit(t, repo, "config", "user.email", "runtime-test@example.invalid")
	codingTestGit(t, repo, "config", "commit.gpgsign", "false")
	codingTestGit(t, repo, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(repo, "marker.txt"), []byte(marker+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	codingTestGit(t, repo, "add", "--", "marker.txt")
	codingTestGit(t, repo, "commit", "-m", "initial")
	return repo
}
func codingTestCall(t *testing.T, rt *Runtime, name string, args map[string]any) Result {
	t.Helper()
	result, err := rt.Call(context.Background(), name, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	assertToolResultMatchesOutputSchema(t, name, result)
	return result
}
func codingTestStart(t *testing.T, rt *Runtime, id, repo string) string {
	t.Helper()
	codingTestCall(t, rt, "project_registry", map[string]any{"action": "register", "project": map[string]any{"id": id, "repo_path": repo}})
	result := codingTestCall(t, rt, "work_on_project", map[string]any{"project": id, "task": "exercise native coding lifecycle"})
	return result["task_id"].(string)
}
func codingTestTask(t *testing.T, rt *Runtime, id string) taskstate.Task {
	t.Helper()
	result := codingTestCall(t, rt, "coding_task", map[string]any{"task_id": id})
	data, err := json.Marshal(result["task"])
	if err != nil {
		t.Fatal(err)
	}
	var task taskstate.Task
	if err = json.Unmarshal(data, &task); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestCodingNativeContextIsolationAndArtifacts(t *testing.T) {
	rt := newRuntimeValidationTestRuntime(t)
	if rt.acp != nil {
		t.Fatal("coding initialization unexpectedly created an ACP transport")
	}
	for _, name := range rt.ToolNames() {
		if strings.HasPrefix(name, "acp_") {
			t.Fatalf("ACP disabled but %s exposed", name)
		}
	}
	a := codingTestRepo(t, "alpha-body-marker")
	b := codingTestRepo(t, "beta-body-marker")
	alpha := codingTestStart(t, rt, "alpha", a)
	beta := codingTestStart(t, rt, "beta", b)
	originalCWD := rt.Workspace().DefaultCWD()
	for _, test := range []struct{ id, marker string }{{alpha, "alpha-body-marker"}, {beta, "beta-body-marker"}, {alpha, "alpha-body-marker"}} {
		result := codingTestCall(t, rt, "read_file", map[string]any{"task_id": test.id, "path": "marker.txt"})
		data, _ := json.Marshal(result)
		if !strings.Contains(string(data), test.marker) {
			t.Fatalf("task context leaked: %s", data)
		}
	}
	codingTestCall(t, rt, "file_edit", map[string]any{"task_id": alpha, "action": "replace", "path": "marker.txt", "old": "alpha-body-marker", "new": "updated-alpha"})
	if data, _ := os.ReadFile(filepath.Join(b, "marker.txt")); string(data) != "beta-body-marker\n" {
		t.Fatal("edit crossed into another project")
	}
	if rt.Workspace().DefaultCWD() != originalCWD {
		t.Fatal("entry changed global cwd")
	}
	for _, name := range []string{"git_status", "git_diff", "git_changed_files", "git_commits", "git_branches"} {
		codingTestCall(t, rt, name, map[string]any{"task_id": alpha})
	}
	codingTestCall(t, rt, "git_worktree", map[string]any{"task_id": alpha, "action": "list"})
	published := codingTestCall(t, rt, "file_publish", map[string]any{"task_id": alpha, "path": "marker.txt"})
	task := codingTestTask(t, rt, alpha)
	if len(task.Coding.Artifacts) != 1 || task.Coding.Artifacts[0].ArtifactID != published["artifact_id"] {
		t.Fatal("native artifact was not linked to the existing Task")
	}
	// Omitted task_id retains the existing General-tool behavior.
	if _, err := rt.Call(context.Background(), "read_file", map[string]any{"path": "marker.txt"}); err == nil {
		t.Fatal("General tool inherited another conversation's project")
	}
}

func TestCodingValidationRequiresPurposeAndHonestCloseout(t *testing.T) {
	rt := newRuntimeValidationTestRuntime(t)
	repo := codingTestRepo(t, "initial")
	id := codingTestStart(t, rt, "demo", repo)
	if _, err := rt.Call(context.Background(), "validation_run", map[string]any{"task_id": id, "cmd": "echo forbidden-before-purpose"}); err == nil {
		t.Fatal("validation ran without rationale")
	}
	if len(codingTestTask(t, rt, id).Coding.Commands) != 0 {
		t.Fatal("invalid validation request started work")
	}
	if _, err := rt.Call(context.Background(), "finish_coding_task", map[string]any{"task_id": id, "summary": "not tested", "ready_for_review": true}); err == nil {
		t.Fatal("no-check readiness accepted without explanation")
	}
	codingTestCall(t, rt, "file_edit", map[string]any{"task_id": id, "action": "replace", "path": "marker.txt", "old": "initial", "new": "changed"})
	args := map[string]any{"task_id": id, "cmd": "git diff --exit-code -- marker.txt", "purpose": "Detect a changed fixture so failed validation is recorded honestly.", "on_failure": "Restore the fixture, then rerun this same check.", "execution_mode": "sync"}
	failure := codingTestCall(t, rt, "validation_run", args)
	if failure["exit_code"] != 1 {
		t.Fatalf("expected git diff failure, got %v", failure["exit_code"])
	}
	if _, err := rt.Call(context.Background(), "finish_coding_task", map[string]any{"task_id": id, "summary": "failed check", "ready_for_review": true}); err == nil {
		t.Fatal("failed validation declared ready")
	}
	codingTestCall(t, rt, "file_edit", map[string]any{"task_id": id, "action": "replace", "path": "marker.txt", "old": "changed", "new": "initial"})
	success := codingTestCall(t, rt, "validation_run", args)
	if success["exit_code"] != 0 {
		t.Fatalf("expected successful rerun, got %v", success["exit_code"])
	}
	codingTestCall(t, rt, "finish_coding_task", map[string]any{"task_id": id, "summary": "fixture restored and same validation passed", "ready_for_review": true})
	task := codingTestTask(t, rt, id)
	if task.Status != taskstate.StatusActive || task.Coding.Closeout.ValidationResult != "passed" {
		t.Fatalf("bad closeout: %+v", task.Coding.Closeout)
	}
	if len(task.Coding.Commands) != 2 || task.Coding.Commands[0].ExitCode == nil || *task.Coding.Commands[0].ExitCode != 1 || task.Coding.Commands[1].LogArtifactID == "" {
		t.Fatalf("lost real command evidence: %+v", task.Coding.Commands)
	}
	if task.Events[len(task.Events)-1].Type != "coding.ready_for_review" {
		t.Fatal("control-plane event boundary missing")
	}
	codingTestCall(t, rt, "file_edit", map[string]any{"task_id": id, "action": "replace", "path": "marker.txt", "old": "initial", "new": "later edit"})
	if codingTestTask(t, rt, id).Coding.Closeout != nil {
		t.Fatal("editing left stale ready_for_review closeout")
	}
}

func TestCodingDisconnectedRequestUsesExistingSessionEvidence(t *testing.T) {
	rt := newRuntimeValidationTestRuntime(t)
	repo := codingTestRepo(t, "async")
	id := codingTestStart(t, rt, "demo", repo)
	cmd := "sleep 0.3; echo retained-after-observe"
	if goruntime.GOOS == "windows" {
		cmd = "Start-Sleep -Milliseconds 300; Write-Output retained-after-observe"
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := rt.Call(ctx, "exec_command", map[string]any{"task_id": id, "cmd": cmd, "yield_time_ms": 1, "timeout_ms": 10000})
	if err != nil {
		t.Fatal(err)
	}
	sessionID, _ := result["session_id"].(string)
	session, ok := rt.command.Store().Get(sessionID)
	if !ok {
		t.Fatal("execution did not use existing session store")
	}
	select {
	case <-session.Done:
	case <-time.After(10 * time.Second):
		t.Fatal("command failed to complete independently of request")
	}
	codingTestCall(t, rt, "session_observe", map[string]any{"action": "status", "session_id": sessionID})
	rt.coding.WaitEvidence() // Await persistence, not only native process completion.
	task := codingTestTask(t, rt, id)
	if len(task.Coding.Commands) != 1 || task.Coding.Commands[0].Status != "exited" || !strings.Contains(task.Coding.Commands[0].Output, "retained-after-observe") {
		t.Fatalf("observing session lost closeout evidence: %+v", task.Coding.Commands)
	}
	if task.Coding.Commands[0].ExitCode == nil || *task.Coding.Commands[0].ExitCode != 0 {
		t.Fatal("disconnect killed command")
	}
}

func TestCodingRestartRecoversTaskWithoutReplayingCommand(t *testing.T) {
	rt := newRuntimeValidationTestRuntime(t)
	repo := codingTestRepo(t, "restart")
	id := codingTestStart(t, rt, "demo", repo)
	cmd := "sleep 10; echo should-not-replay"
	if goruntime.GOOS == "windows" {
		cmd = "Start-Sleep -Seconds 10; Write-Output should-not-replay"
	}
	codingTestCall(t, rt, "validation_run", map[string]any{"task_id": id, "cmd": cmd, "execution_mode": "async", "timeout_ms": 15000, "purpose": "Exercise interruption recovery.", "on_failure": "Report interruption, do not replay."})
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	saved, err := rt.tasks.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Coding.Commands[0].ExitCode == nil {
		t.Fatal("graceful shutdown did not persist the terminal outcome")
	}
	// Inject the persisted state left by an abrupt crash before the completion
	// callback ran. A new Runtime must not adopt or replay that missing process.
	_, err = rt.tasks.UpdateCoding(id, "", "", func(c *taskstate.CodingState) error {
		c.Commands[0].Status = "running"
		c.Commands[0].SessionID = "missing-after-crash"
		c.Commands[0].ExitCode = nil
		c.Commands[0].LogArtifactID = ""
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := NewRuntime(rt.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	result := codingTestCall(t, restored, "work_on_project", map[string]any{"task_id": id})
	if result["task_id"] != id {
		t.Fatal("restart invented another Task identity")
	}
	task := codingTestTask(t, restored, id)
	if len(task.Coding.Commands) != 1 || task.Coding.Commands[0].Status != "interrupted" {
		t.Fatalf("missing process was misreported: %+v", task.Coding.Commands)
	}
	if len(restored.command.Store().List()) != 0 {
		t.Fatal("recovery silently replayed a command")
	}
	if _, err := restored.Call(context.Background(), "finish_coding_task", map[string]any{"task_id": id, "summary": "interrupted", "ready_for_review": true}); err == nil {
		t.Fatal("interrupted validation declared ready")
	}
}

func TestCodingManagedCleanupAfterExplicitCompletion(t *testing.T) {
	rt := newRuntimeValidationTestRuntime(t)
	repo := codingTestRepo(t, "completed-task")
	codingTestCall(t, rt, "project_registry", map[string]any{"action": "register", "project": map[string]any{"id": "demo", "repo_path": repo}})
	work := codingTestCall(t, rt, "work_on_project", map[string]any{"project": "demo", "task": "inspect an isolated checkout", "execution_mode": "delegated-task"})
	id := work["task_id"].(string)
	codingTestCall(t, rt, "finish_coding_task", map[string]any{"task_id": id, "summary": "read-only fixture inspected", "ready_for_review": true, "validation_note": "No executable change; the fixture asserts resource cleanup behavior."})
	codingTestCall(t, rt, "task_manage", map[string]any{"action": "final_review", "task_id": id, "status": "pass", "summary": "explicit fixture acceptance", "verified": []string{"No implementation changes were requested."}})
	codingTestCall(t, rt, "task_manage", map[string]any{"action": "complete", "task_id": id})
	for range 2 {
		codingTestCall(t, rt, "git_worktree", map[string]any{"task_id": id, "action": "remove"})
	}
	task := codingTestTask(t, rt, id)
	if task.Status != taskstate.StatusCompleted || task.FinalReview == nil || !task.Coding.WorkspaceRemoved {
		t.Fatalf("cleanup changed completion or lost metadata: %+v", task)
	}
	if _, err := os.Stat(repo); err != nil {
		t.Fatal("cleanup touched owner checkout")
	}
	codingTestCall(t, rt, "git_worktree", map[string]any{"task_id": id, "action": "list"})
}
