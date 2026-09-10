// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package coding

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/publicartifacts"
	"github.com/uvwt/agentdock/internal/taskstate"
)

func fixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitText(context.Background(), dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func fixtureWrite(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func fixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	fixtureGit(t, dir, "init", "-b", "main")
	fixtureGit(t, dir, "config", "user.name", "Runtime Test")
	fixtureGit(t, dir, "config", "user.email", "runtime-test@example.invalid")
	fixtureGit(t, dir, "config", "commit.gpgsign", "false")
	fixtureGit(t, dir, "config", "core.autocrlf", "false")
	fixtureWrite(t, dir, "tracked.txt", "original\n")
	fixtureWrite(t, dir, "other.txt", "other\n")
	fixtureGit(t, dir, "add", "--", "tracked.txt", "other.txt")
	fixtureGit(t, dir, "commit", "-m", "initial")
	return dir
}
func fixtureService(t *testing.T) *Service {
	t.Helper()
	home := t.TempDir()
	tasks, err := taskstate.New(filepath.Join(home, "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(home, tasks, nil, publicartifacts.New(home, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	return service
}
func fixtureRegister(t *testing.T, s *Service, id, repo string) {
	t.Helper()
	if _, err := s.Registry.Register(context.Background(), Project{ID: id, RepoPath: repo}); err != nil {
		t.Fatal(err)
	}
}

func TestCodingGitStatusRenamesAndUnicode(t *testing.T) {
	repo := fixtureRepo(t)
	fixtureGit(t, repo, "mv", "tracked.txt", "改名 with space.txt")
	fixtureWrite(t, repo, "untracked[1].txt", "new\n")
	state, err := GitStatus(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if state.Branch != "main" || state.Clean || len(state.Files) != 2 {
		t.Fatalf("unexpected status: %+v", state)
	}
	var rename, untracked bool
	for _, f := range state.Files {
		if f.Path == "改名 with space.txt" {
			rename = f.OriginalPath == "tracked.txt" && f.Index == "R"
		}
		if f.Path == "untracked[1].txt" {
			untracked = f.Index == "?" && f.Worktree == "?"
		}
	}
	if !rename || !untracked {
		t.Fatalf("lost rename/untracked metadata: %+v", state.Files)
	}
}

func TestCodingCommitPreservesUnrelatedIndexAndLiteralPaths(t *testing.T) {
	repo := fixtureRepo(t)
	base := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureWrite(t, repo, "other.txt", "user staged work\n")
	fixtureGit(t, repo, "add", "--", "other.txt")
	fixtureWrite(t, repo, "a[1].txt", "selected\n")
	fixtureWrite(t, repo, "a1.txt", "not selected\n")
	commit, err := GitCommit(context.Background(), repo, "selected file", []string{"a[1].txt"})
	if err != nil {
		t.Fatal(err)
	}
	if commit == base {
		t.Fatal("commit was not created")
	}
	names := fixtureGit(t, repo, "show", "--format=", "--name-only", "HEAD")
	if names != "a[1].txt" {
		t.Fatalf("commit included unrelated files: %q", names)
	}
	if staged := fixtureGit(t, repo, "diff", "--cached", "--name-only"); staged != "other.txt" {
		t.Fatalf("unrelated staging was altered: %q", staged)
	}
	files, err := GitChangedFiles(context.Background(), repo, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("lost committed or untracked changes: %+v", files)
	}
	diff, err := GitDiff(context.Background(), repo, "task", base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff.Text, "selected") || !strings.Contains(diff.Text, "user staged work") {
		t.Fatalf("task diff omitted changes: %s", diff.Text)
	}
	commits, err := GitCommits(context.Background(), repo, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 || commits[0].Subject != "selected file" {
		t.Fatalf("bad log: %+v", commits)
	}
	branches, err := GitBranches(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || !branches[0].Current || branches[0].Name != "main" {
		t.Fatalf("bad branches: %+v", branches)
	}
}

func TestCodingRegistryPersistsAndRejectsRetarget(t *testing.T) {
	s := fixtureService(t)
	repo := fixtureRepo(t)
	fixtureRegister(t, s, "demo", repo)
	second, err := NewRegistry(s.home)
	if err != nil {
		t.Fatal(err)
	}
	project, err := second.Get("demo")
	if err != nil || !samePath(project.RepoPath, repo) {
		t.Fatalf("registry did not persist: %+v %v", project, err)
	}
	other := fixtureRepo(t)
	if _, err := second.Register(context.Background(), Project{ID: "demo", RepoPath: other}); err == nil {
		t.Fatal("silently retargeted project")
	}
	project, err = second.Get("demo")
	if err != nil || !samePath(project.RepoPath, repo) {
		t.Fatal("existing project was not preserved")
	}
}

func TestCodingOwnerAndDelegatedWorktreeResume(t *testing.T) {
	ctx := context.Background()
	s := fixtureService(t)
	repo := fixtureRepo(t)
	fixtureRegister(t, s, "demo", repo)
	fixtureWrite(t, repo, "tracked.txt", "owner work\n")
	fixtureGit(t, repo, "add", "--", "tracked.txt")
	owner, err := s.Work(ctx, WorkRequest{Project: "demo", Task: "owner coding"})
	if err != nil {
		t.Fatal(err)
	}
	if owner.ExecutionMode != InteractiveOwner || !samePath(owner.Workspace, repo) || owner.Git.Clean {
		t.Fatalf("wrong owner context: %+v", owner)
	}
	if owner.Task.Coding.InitialGit.Clean {
		t.Fatal("pre-existing user work was not captured")
	}
	worker, err := s.Work(ctx, WorkRequest{Project: "demo", Task: "delegated coding", ExecutionMode: DelegatedTask, Assignee: "Yifeng"})
	if err != nil {
		t.Fatal(err)
	}
	if samePath(worker.Workspace, repo) || !worker.Git.Clean {
		t.Fatal("delegated work did not get an isolated clean worktree")
	}
	if data, _ := os.ReadFile(filepath.Join(worker.Workspace, "tracked.txt")); string(data) != "original\n" {
		t.Fatal("owner dirty files leaked into worktree")
	}
	resumed, err := s.Work(ctx, WorkRequest{TaskID: worker.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.TaskID != worker.TaskID || resumed.Workspace != worker.Workspace {
		t.Fatal("resume started a second execution")
	}
	tasks, err := s.Tasks.List("", 20)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("duplicate tasks: %d %v", len(tasks), err)
	}
	fixtureWrite(t, worker.Workspace, "tracked.txt", "delegated work\n")
	if _, err = s.RemoveWorktree(ctx, worker.TaskID); err == nil {
		t.Fatal("removed a dirty managed worktree")
	}
	result, err := s.Finish(ctx, FinishRequest{TaskID: worker.TaskID, Summary: "documentation-only fixture", ReadyForReview: true, ValidationNote: "This fixture validates closeout mechanics; no project executable check applies."})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != taskstate.StatusActive || result.Coding.Closeout.ValidationResult != "not_run" || result.Coding.Closeout.DiffArtifactID == "" {
		t.Fatalf("invalid closeout: %+v", result.Coding.Closeout)
	}
	if _, err = os.Stat(worker.Workspace); err != nil {
		t.Fatal("finish removed the managed worktree")
	}
	if fixtureGit(t, repo, "diff", "--cached", "--name-only") != "tracked.txt" {
		t.Fatal("owner staging was changed")
	}
	fixtureWrite(t, worker.Workspace, "tracked.txt", "original\n")
	removed, err := s.RemoveWorktree(ctx, worker.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if !removed.Coding.WorkspaceRemoved {
		t.Fatal("removal was not persisted")
	}
	if _, err := s.RemoveWorktree(ctx, owner.TaskID); err == nil {
		t.Fatal("owner checkout could be removed")
	}
}

func TestCodingValidationSummariesRetainHistory(t *testing.T) {
	zero, one := 0, 1
	commands := []taskstate.CodingCommand{{Command: "check", Workdir: "repo", Validation: true, Status: "exited", ExitCode: &one}}
	if validationResult(commands) != "failed" {
		t.Fatal("failure was hidden")
	}
	commands = append(commands, taskstate.CodingCommand{Command: "check", Workdir: "repo", Validation: true, Status: "exited", ExitCode: &zero})
	if validationResult(commands) != "passed" {
		t.Fatal("successful rerun did not supersede same validation failure")
	}
	commands = append(commands, taskstate.CodingCommand{Command: "other", Workdir: "repo", Validation: true, Status: "interrupted"})
	if validationResult(commands) != "interrupted" {
		t.Fatal("interruption was hidden")
	}
	if validationResult(nil) != "not_run" {
		t.Fatal("missing validation reported as success")
	}
}

func TestCodingBootstrapFailureDoesNotCreateWork(t *testing.T) {
	s := fixtureService(t)
	repo := fixtureRepo(t)
	fixtureRegister(t, s, "demo", repo)
	if _, err := s.Work(context.Background(), WorkRequest{Project: "demo", Task: "bad mode", ExecutionMode: "codex"}); err == nil {
		t.Fatal("unsupported mode accepted")
	}
	if _, err := s.Work(context.Background(), WorkRequest{Project: "demo", Task: "bad ref", ExecutionMode: DelegatedTask, BaseRef: "missing-base"}); err == nil {
		t.Fatal("missing base accepted")
	}
	tasks, err := s.Tasks.List("", 20)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("failed bootstrap created tasks: %+v %v", tasks, err)
	}
}
