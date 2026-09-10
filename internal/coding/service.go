// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package coding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/publicartifacts"
	"github.com/uvwt/agentdock/internal/taskstate"
	toolcommand "github.com/uvwt/agentdock/internal/tool/command"
)

const InteractiveOwner = "interactive-owner"
const DelegatedTask = "delegated-task"

type Service struct {
	Registry  *Registry
	Tasks     *taskstate.Store
	commands  *toolcommand.Service
	artifacts publicartifacts.Store
	home      string
	starting  sync.Map
	evidence  sync.WaitGroup
	lifecycle sync.Mutex
	closing   bool
}

func New(home string, tasks *taskstate.Store, commands *toolcommand.Service, artifacts publicartifacts.Store) (*Service, error) {
	registry, err := NewRegistry(home)
	if err != nil {
		return nil, err
	}
	return &Service{Registry: registry, Tasks: tasks, commands: commands, artifacts: artifacts, home: home}, nil
}

type WorkRequest struct {
	Project              string                    `json:"project"`
	Task                 string                    `json:"task"`
	TaskID               string                    `json:"task_id"`
	ExecutionMode        string                    `json:"execution_mode"`
	Assignee             string                    `json:"assignee"`
	BaseRef              string                    `json:"base_ref"`
	CompletionConditions []string                  `json:"completion_conditions"`
	Steps                []taskstate.TaskStepInput `json:"steps"`
}

type InstructionFile struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}
type ProjectContext struct {
	TaskID                string             `json:"task_id"`
	Project               Project            `json:"project"`
	Machine               Machine            `json:"machine"`
	Task                  taskstate.Task     `json:"task"`
	Repository            string             `json:"repository"`
	Workspace             string             `json:"workspace"`
	Branch                string             `json:"branch"`
	Assignee              string             `json:"assignee"`
	ExecutionMode         string             `json:"execution_mode"`
	Git                   taskstate.GitState `json:"git"`
	AvailableCapabilities []string           `json:"available_capabilities"`
	Instructions          []InstructionFile  `json:"instruction_files"`
	ConnectedDevices      []Device           `json:"connected_devices"`
	DeviceDiscovery       string             `json:"device_discovery"`
	NextAction            string             `json:"next_action"`
}

func (s *Service) Work(ctx context.Context, req WorkRequest) (ProjectContext, error) {
	var task taskstate.Task
	var err error
	if req.TaskID != "" {
		task, err = s.Tasks.Get(req.TaskID)
		if err != nil {
			return ProjectContext{}, err
		}
		if task.Coding != nil {
			c := task.Coding
			if (req.Project != "" && req.Project != c.ProjectID) || (req.ExecutionMode != "" && req.ExecutionMode != c.Mode) || (req.Assignee != "" && req.Assignee != c.Assignee) || req.BaseRef != "" {
				return ProjectContext{}, errors.New("resume cannot retarget the existing task; use its saved project/mode/workspace")
			}
			if c.WorkspaceRemoved {
				return ProjectContext{}, errors.New("this task's managed workspace was explicitly removed")
			}
			if task.Status == taskstate.StatusCompleted {
				return s.Context(ctx, task.ID)
			}
			if task.Status == taskstate.StatusBlocked {
				if _, err = s.Tasks.Resume(task.ID, "explicit coding continuation"); err != nil {
					return ProjectContext{}, err
				}
			}
			if err = s.prepareWorkspace(ctx, task.ID); err != nil {
				return ProjectContext{}, err
			}
			return s.Context(ctx, task.ID)
		}
		if req.Project == "" {
			req.Project = task.Project
		}
		if req.Task == "" {
			req.Task = task.Goal
		}
	}
	project, err := s.Registry.Get(req.Project)
	if err != nil {
		return ProjectContext{}, err
	}
	if project.MachineID != s.Registry.Machine.ID {
		return ProjectContext{}, errors.New("project belongs to another runner")
	}
	mode := req.ExecutionMode
	if mode == "" {
		mode = InteractiveOwner
	}
	if mode != InteractiveOwner && mode != DelegatedTask {
		return ProjectContext{}, errors.New("execution_mode must be interactive-owner or delegated-task")
	}
	if strings.TrimSpace(req.Task) == "" {
		return ProjectContext{}, errors.New("task instruction is required for new coding work")
	}
	if mode == InteractiveOwner && req.BaseRef != "" {
		return ProjectContext{}, errors.New("interactive-owner uses the current checkout; base_ref is for delegated-task")
	}
	initial, err := GitStatus(ctx, project.RepoPath)
	if err != nil {
		return ProjectContext{}, err
	}
	base := initial.Head
	if mode == DelegatedTask {
		ref := req.BaseRef
		if ref == "" {
			ref = "HEAD"
		}
		base, err = gitText(ctx, project.RepoPath, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
		if err != nil {
			return ProjectContext{}, fmt.Errorf("delegated worktree requires an existing base commit: %w", err)
		}
	}
	if req.TaskID == "" {
		conditions := req.CompletionConditions
		if len(conditions) == 0 {
			conditions = []string{"Requested change is implemented and actual verification and remaining issues are reported."}
		}
		title := req.Task
		if len([]rune(title)) > 120 {
			title = string([]rune(title)[:120])
		}
		task, err = s.Tasks.CreateWithContext(title, req.Task, project.ID, project.MachineID, conditions, req.Steps, nil)
		if err != nil {
			return ProjectContext{}, err
		}
	}
	state := taskstate.CodingState{ProjectID: project.ID, Repository: project.RepoPath, Workspace: project.RepoPath, MachineID: project.MachineID, Mode: mode, Assignee: req.Assignee, Branch: initial.Branch, BaseCommit: base, InitialGit: initial, Managed: mode == DelegatedTask}
	if state.Managed {
		state.Workspace = filepath.Join(s.home, "worktrees", project.ID, task.ID)
		state.Branch = "runtime/" + task.ID
	}
	if _, err = s.Tasks.BindCoding(task.ID, state); err != nil {
		return ProjectContext{}, fmt.Errorf("task %s: %w", task.ID, err)
	}
	if err = s.prepareWorkspace(ctx, task.ID); err != nil {
		return ProjectContext{}, err
	}
	return s.Context(ctx, task.ID)
}

func (s *Service) prepareWorkspace(ctx context.Context, id string) error {
	task, err := s.Tasks.Get(id)
	if err != nil {
		return err
	}
	c := *task.Coding
	if c.Managed {
		err = ensureManagedWorktree(ctx, c)
	}
	if err == nil {
		_, err = s.CheckWorkspace(ctx, c)
	}
	if err != nil {
		_, blockErr := s.Tasks.Block(id, "workspace preparation failed: "+err.Error())
		return fmt.Errorf("task %s retained for explicit resume: %w", id, errors.Join(err, blockErr))
	}
	if !c.WorkspaceReady {
		_, err = s.Tasks.UpdateCoding(id, "coding.started", c.Workspace, func(c *taskstate.CodingState) error { c.WorkspaceReady = true; return nil })
	}
	return err
}

func (s *Service) CheckWorkspace(ctx context.Context, c taskstate.CodingState) (taskstate.GitState, error) {
	if c.WorkspaceRemoved {
		return taskstate.GitState{}, errors.New("managed workspace has been removed")
	}
	root, err := gitText(ctx, c.Workspace, "rev-parse", "--show-toplevel")
	if err != nil {
		return taskstate.GitState{}, err
	}
	if !samePath(root, c.Workspace) {
		return taskstate.GitState{}, errors.New("saved workspace no longer names the repository root")
	}
	state, err := GitStatus(ctx, c.Workspace)
	if err != nil {
		return state, err
	}
	if state.Branch != c.Branch {
		return state, errors.New("workspace branch changed since task start; existing checkout was preserved")
	}
	return state, nil
}

func (s *Service) Task(id string) (taskstate.Task, error) {
	task, err := s.Tasks.Get(id)
	if err != nil {
		return task, err
	}
	if task.Coding == nil {
		return task, errors.New("task has no coding context; call work_on_project")
	}
	if task.Coding.MachineID != s.Registry.Machine.ID {
		return task, errors.New("task workspace belongs to another machine")
	}
	return task, nil
}

func (s *Service) Context(ctx context.Context, id string) (ProjectContext, error) {
	task, err := s.Refresh(id)
	if err != nil {
		return ProjectContext{}, err
	}
	c := task.Coding
	project, err := s.Registry.Get(c.ProjectID)
	if err != nil {
		return ProjectContext{}, err
	}
	state := taskstate.GitState{Files: []taskstate.ChangedFile{}}
	if c.WorkspaceReady && !c.WorkspaceRemoved {
		state, err = s.CheckWorkspace(ctx, *c)
		if err != nil {
			return ProjectContext{}, err
		}
	}
	instructions := []InstructionFile{}
	if !c.WorkspaceRemoved {
		for _, name := range project.InstructionFiles {
			path := name
			if !filepath.IsAbs(path) {
				path = filepath.Join(c.Workspace, path)
			}
			file, err := os.Open(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return ProjectContext{}, err
			}
			data, readErr := io.ReadAll(io.LimitReader(file, 32769))
			_ = file.Close()
			if readErr != nil {
				return ProjectContext{}, readErr
			}
			truncated := len(data) > 32768
			if truncated {
				data = data[:32768]
			}
			instructions = append(instructions, InstructionFile{Path: path, Content: strings.ToValidUTF8(string(data), ""), Truncated: truncated})
		}
	}
	return ProjectContext{TaskID: id, Project: project, Machine: s.Registry.Machine, Task: task, Repository: c.Repository, Workspace: c.Workspace, Branch: c.Branch, Assignee: c.Assignee, ExecutionMode: c.Mode, Git: state, AvailableCapabilities: []string{"files", "shell", "structured_git", "managed_worktree", "validation", "tasks", "artifacts", "dynamic_mcp", "skills"}, Instructions: instructions, ConnectedDevices: []Device{}, DeviceDiscovery: "not_probed; project.devices is operator-declared metadata", NextAction: "Pass this task_id to subsequent tools. Relative paths inherit workspace; no global current project is changed. Inspect applicable nested instructions before editing."}, nil
}

// ResolveArguments gives existing General tools task-local paths without changing
// their implementation or the process-global Workspace default directory.
func (s *Service) ResolveArguments(name string, args map[string]any) (map[string]any, error) {
	id, _ := args["task_id"].(string)
	if id == "" {
		return nil, errors.New("non-empty task_id is required")
	}
	task, err := s.Task(id)
	if err != nil {
		return nil, err
	}
	c := task.Coding
	if !c.WorkspaceReady || c.WorkspaceRemoved {
		return nil, errors.New("task workspace is not available")
	}
	if args["runtime"] == "wsl" {
		return nil, errors.New("this coding workspace is host-native; use a separately registered WSL project in a future runner")
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if k != "task_id" {
			out[k] = v
		}
	}
	resolve := func(key string, defaultPath bool) {
		raw, _ := out[key].(string)
		if raw == "" && !defaultPath {
			return
		}
		if !filepath.IsAbs(raw) && !strings.HasPrefix(raw, "~") && !strings.HasPrefix(raw, "skill://") {
			out[key] = filepath.Join(c.Workspace, raw)
		}
	}
	switch name {
	case "read_file", "list_dir", "search_text":
		resolve("path", true)
	case "file_edit":
		resolve("path", false)
		resolve("new_path", false)
		resolve("workdir", true)
	case "exec_command", "validation_run":
		resolve("workdir", true)
	case "file_publish":
		resolve("path", false)
		resolve("file", false)
	}
	return out, nil
}

func (s *Service) AttachArtifact(id, artifactID, purpose string) error {
	if artifactID == "" {
		return errors.New("artifact_id is required")
	}
	_, err := s.Tasks.UpdateCoding(id, "", "", func(c *taskstate.CodingState) error {
		for _, a := range c.Artifacts {
			if a.ArtifactID == artifactID {
				return nil
			}
		}
		c.Artifacts = append(c.Artifacts, taskstate.CodingArtifact{ArtifactID: artifactID, Purpose: purpose})
		return nil
	})
	return err
}

type FinishRequest struct {
	TaskID          string   `json:"task_id"`
	Summary         string   `json:"summary"`
	RemainingIssues []string `json:"remaining_issues"`
	ReadyForReview  bool     `json:"ready_for_review"`
	ValidationNote  string   `json:"validation_note"`
}

func (s *Service) Finish(ctx context.Context, req FinishRequest) (taskstate.Task, error) {
	if strings.TrimSpace(req.Summary) == "" {
		return taskstate.Task{}, errors.New("summary is required")
	}
	task, err := s.Refresh(req.TaskID)
	if err != nil {
		return task, err
	}
	if task.Status != taskstate.StatusActive {
		return task, errors.New("only an active task can be submitted for review")
	}
	c := task.Coding
	state, err := s.CheckWorkspace(ctx, *c)
	if err != nil {
		return task, err
	}
	files, err := GitChangedFiles(ctx, c.Workspace, c.BaseCommit)
	if err != nil {
		return task, err
	}
	validation := validationResult(c.Commands)
	for _, command := range c.Commands {
		if command.Status == "running" || command.Status == "starting" {
			return task, errors.New("a command is still running; observe the existing session before finishing")
		}
	}
	if req.ReadyForReview {
		if validation == "failed" || validation == "interrupted" {
			return task, errors.New("failed or interrupted validation cannot be declared ready for review")
		}
		if validation == "not_run" && strings.TrimSpace(req.ValidationNote) == "" {
			return task, errors.New("no validation ran; explain why no executable check is applicable in validation_note")
		}
	}
	diff, err := GitDiff(ctx, c.Workspace, "task", c.BaseCommit, nil)
	if err != nil {
		return task, err
	}
	diffID := ""
	if diff.Text != "" {
		artifact, err := s.artifacts.PublishBytes(publicartifacts.PublishBytesRequest{Filename: task.ID + ".diff", Data: []byte(diff.Text), MimeType: "text/plain", RetentionSeconds: 604800})
		if err != nil {
			return task, err
		}
		diffID = artifact.ArtifactID
		if err = s.AttachArtifact(task.ID, diffID, "task diff (untracked files are listed separately)"); err != nil {
			return task, err
		}
	}
	issues := req.RemainingIssues
	if issues == nil {
		issues = []string{}
	}
	result := taskstate.CodingCloseout{Summary: req.Summary, ChangedFiles: files, Git: state, DiffArtifactID: diffID, DiffTruncated: diff.Truncated, ValidationResult: validation, ValidationNote: req.ValidationNote, RemainingIssues: issues, ReadyForReview: req.ReadyForReview}
	return s.Tasks.FinishCoding(task.ID, result)
}

func (s *Service) RemoveWorktree(ctx context.Context, id string) (taskstate.Task, error) {
	task, err := s.Refresh(id)
	if err != nil {
		return task, err
	}
	c := task.Coding
	if !c.Managed {
		return task, errors.New("owner checkout cannot be removed by git_worktree")
	}
	if c.WorkspaceRemoved {
		return task, nil
	}
	for _, command := range c.Commands {
		if command.Status == "running" || command.Status == "starting" {
			return task, errors.New("worktree has an active command")
		}
	}
	trees, err := GitWorktrees(ctx, c.Repository)
	if err != nil {
		return task, err
	}
	registered := false
	for _, tree := range trees {
		if samePath(tree.Path, c.Workspace) {
			registered = true
			break
		}
	}
	if !registered {
		if _, statErr := os.Stat(c.Workspace); errors.Is(statErr, os.ErrNotExist) {
			// Retry after Git succeeded but the metadata write was interrupted.
			return s.Tasks.MarkCodingWorkspaceRemoved(id)
		}
		return task, errors.New("saved workspace is no longer registered in Git; existing directory was preserved")
	}
	if _, err = s.CheckWorkspace(ctx, *c); err != nil {
		return task, err
	}
	// No --force: Git preserves dirty worktrees and valuable untracked files.
	if _, err = gitText(ctx, c.Repository, "worktree", "remove", c.Workspace); err != nil {
		return task, err
	}
	return s.Tasks.MarkCodingWorkspaceRemoved(id)
}

func Decode(args map[string]any, value any) error {
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func validationResult(commands []taskstate.CodingCommand) string {
	latest := map[string]taskstate.CodingCommand{}
	for _, command := range commands {
		if command.Validation {
			latest[command.Workdir+"\x00"+command.Command] = command
		}
	}
	if len(latest) == 0 {
		return "not_run"
	}
	result := "passed"
	for _, command := range latest {
		switch command.Status {
		case "running", "starting":
			return "running"
		case "interrupted":
			result = "interrupted"
		default:
			if command.ExitCode == nil || *command.ExitCode != 0 || command.Status == "timeout" {
				if result != "interrupted" {
					result = "failed"
				}
			}
		}
	}
	return result
}

// Keep time in this domain explicit for evidence; it is not a task scheduler.
func nowUTC() time.Time { return time.Now().UTC() }
