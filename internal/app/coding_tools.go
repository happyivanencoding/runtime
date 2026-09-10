// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/uvwt/agentdock/internal/coding"
)

func codingToolSpecs() []ToolSpec {
	entries := []struct {
		name, title, description string
		readOnly                 bool
	}{
		{"project_registry", "Project Registry", "Register, inspect or list repositories on this machine. Registration discovers Git root, remote and stack; an existing id cannot silently move to another repository. devices/deployment are declared metadata, not live probes.", false},
		{"work_on_project", "Work on project", "Start or resume native coding work using the existing persistent Task identity. interactive-owner preserves the current checkout; delegated-task creates an isolated managed worktree. Pass returned task_id to later file/command/Git tools. Never changes global cwd or starts ACP/Codex.", false},
		{"coding_task", "Inspect coding task", "Refresh the existing Task with workspace, command/session evidence, validation logs, artifacts and closeout. Missing unfinished sessions are reported interrupted, never rerun. Persists newly observed command completion.", false},
		{"git_status", "Git status", "Return structured branch, HEAD, upstream, ahead/behind and staged/unstaged/untracked files for the Task workspace.", true},
		{"git_diff", "Git diff", "Return a bounded native Git diff. scope=task compares the saved starting commit to the current workspace, including commits. staged and unstaged are also available. Untracked contents are not in Git diff; use git_changed_files/read_file.", true},
		{"git_changed_files", "Changed files", "List current changed/untracked files plus files committed since task start. Initial dirty files stay visible and are not claimed as changes authored by this Task.", true},
		{"git_commits", "Git commits", "Read up to 100 recent commits as structured records.", true},
		{"git_branches", "Git branches", "List local branches, current branch, commits and upstreams as structured records.", true},
		{"git_worktree", "Managed worktree", "List worktrees or explicitly remove this Task's managed worktree without force. Owner checkouts cannot be removed. Branches remain. Create through work_on_project execution_mode=delegated-task.", false},
		{"git_commit", "Commit explicit files", "Commit only explicitly named repository-relative files, preserving unrelated staged changes. No implicit add-all, push, merge or deployment. Use only when the user authorizes committing.", false},
		{"validation_run", "Run focused validation", "Execute real validation through the existing command/session engine. Before running, purpose says what failure it detects and on_failure what changes on failure. Records exit code, duration and log artifact. Observe the same session after disconnection.", false},
		{"finish_coding_task", "Finish coding task", "Persist changed files, Git state/diff artifact, validation result and remaining issues. ready_for_review is an explicit declaration, not human acceptance. Does not commit, push, merge, deploy, install, remove worktrees or complete the Task. Missing checks are not reported passed.", false},
	}
	specs := make([]ToolSpec, 0, len(entries))
	for _, entry := range entries {
		name := entry.name
		annotations := mutatingToolAnnotations(false, false)
		if entry.readOnly {
			annotations = readOnlyToolAnnotations(false)
		}
		if name == "git_commit" || name == "git_worktree" || name == "validation_run" {
			annotations = mutatingToolAnnotations(true, false)
		}
		specs = append(specs, ToolSpec{Name: name, Title: entry.title, Description: entry.description, Annotations: annotations, Handler: func(ctx context.Context, r *Runtime, args map[string]any) (Result, error) {
			return r.callCoding(ctx, name, args)
		}})
	}
	return specs
}

func codingObject(value any) (Result, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result Result
	err = json.Unmarshal(data, &result)
	return result, err
}

func (r *Runtime) callCoding(ctx context.Context, name string, args map[string]any) (Result, error) {
	switch name {
	case "project_registry":
		var req struct {
			Action  string         `json:"action"`
			ID      string         `json:"id"`
			Project coding.Project `json:"project"`
		}
		if err := coding.Decode(args, &req); err != nil {
			return nil, err
		}
		switch req.Action {
		case "register":
			p, err := r.coding.Registry.Register(ctx, req.Project)
			return Result{"project": p}, err
		case "get":
			p, err := r.coding.Registry.Get(req.ID)
			return Result{"project": p}, err
		case "list":
			projects, err := r.coding.Registry.List()
			return Result{"projects": projects, "machine": r.coding.Registry.Machine}, err
		default:
			return nil, errors.New("project_registry action must be register, get or list")
		}
	case "work_on_project":
		var req coding.WorkRequest
		if err := coding.Decode(args, &req); err != nil {
			return nil, err
		}
		result, err := r.coding.Work(ctx, req)
		if err != nil {
			return nil, err
		}
		return codingObject(result)
	case "finish_coding_task":
		var req coding.FinishRequest
		if err := coding.Decode(args, &req); err != nil {
			return nil, err
		}
		task, err := r.coding.Finish(ctx, req)
		return Result{"task_id": task.ID, "task": task}, err
	}
	var req struct {
		TaskID  string   `json:"task_id"`
		Scope   string   `json:"scope"`
		Paths   []string `json:"paths"`
		Limit   int      `json:"limit"`
		Message string   `json:"message"`
		Action  string   `json:"action"`
	}
	if err := coding.Decode(args, &req); err != nil {
		return nil, err
	}
	task, err := r.coding.Task(req.TaskID)
	if err != nil {
		return nil, err
	}
	c := task.Coding
	if name == "coding_task" {
		task, err := r.coding.Refresh(req.TaskID)
		return Result{"task_id": task.ID, "task": task}, err
	}
	if name != "git_worktree" && (!c.WorkspaceReady || c.WorkspaceRemoved) {
		return nil, errors.New("task workspace is not available")
	}
	switch name {
	case "git_status":
		state, err := coding.GitStatus(ctx, c.Workspace)
		if err != nil {
			return nil, err
		}
		return codingObject(state)
	case "git_diff":
		if req.Scope == "" {
			req.Scope = "task"
		}
		diff, err := coding.GitDiff(ctx, c.Workspace, req.Scope, c.BaseCommit, req.Paths)
		if err != nil {
			return nil, err
		}
		return codingObject(diff)
	case "git_changed_files":
		files, err := coding.GitChangedFiles(ctx, c.Workspace, c.BaseCommit)
		return Result{"files": files}, err
	case "git_commits":
		if req.Limit == 0 {
			req.Limit = 20
		}
		commits, err := coding.GitCommits(ctx, c.Workspace, req.Limit)
		return Result{"commits": commits}, err
	case "git_branches":
		branches, err := coding.GitBranches(ctx, c.Workspace)
		return Result{"branches": branches}, err
	case "git_worktree":
		if req.Action == "list" {
			trees, err := coding.GitWorktrees(ctx, c.Repository)
			return Result{"worktrees": trees}, err
		}
		if req.Action == "remove" {
			task, err := r.coding.RemoveWorktree(ctx, req.TaskID)
			return Result{"task_id": task.ID, "task": task}, err
		}
		return nil, errors.New("git_worktree action must be list or remove; create through work_on_project delegated-task")
	case "git_commit":
		if _, err := r.coding.CheckWorkspace(ctx, *c); err != nil {
			return nil, err
		}
		if _, err := r.tasks.InvalidateCodingCloseout(req.TaskID); err != nil {
			return nil, err
		}
		commit, err := coding.GitCommit(ctx, c.Workspace, req.Message, req.Paths)
		return Result{"commit": commit}, err
	case "validation_run":
		var validation struct {
			Purpose   string `json:"purpose"`
			OnFailure string `json:"on_failure"`
		}
		if err := coding.Decode(args, &validation); err != nil {
			return nil, err
		}
		resolved, err := r.coding.ResolveArguments(name, args)
		if err != nil {
			return nil, err
		}
		delete(resolved, "purpose")
		delete(resolved, "on_failure")
		return r.coding.Execute(ctx, req.TaskID, resolved, true, validation.Purpose, validation.OnFailure)
	default:
		return nil, fmt.Errorf("unsupported Coding tool %s", name)
	}
}

func codingContextTool(name string) bool {
	switch name {
	case "read_file", "list_dir", "search_text", "file_edit", "exec_command", "file_publish":
		return true
	}
	return false
}

func (r *Runtime) callInCodingWorkspace(ctx context.Context, spec ToolSpec, args map[string]any) (Result, error) {
	id, _ := args["task_id"].(string)
	resolved, err := r.coding.ResolveArguments(spec.Name, args)
	if err != nil {
		return nil, err
	}
	if spec.Name == "exec_command" {
		return r.coding.Execute(ctx, id, resolved, false, "", "")
	}
	if spec.Name == "file_edit" && args["dry_run"] != true {
		if _, err = r.tasks.InvalidateCodingCloseout(id); err != nil {
			return nil, err
		}
	}
	result, err := spec.Handler(ctx, r, resolved)
	if err == nil && spec.Name == "file_publish" {
		if artifactID, ok := result["artifact_id"].(string); ok {
			if attachErr := r.coding.AttachArtifact(id, artifactID, "published file"); attachErr != nil {
				return result, attachErr
			}
		}
	}
	return result, err
}
