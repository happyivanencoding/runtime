// Copyright 2026 Jingxuan Li and runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package taskstate

import (
	"errors"
	"time"
)

// CodingState extends the existing Task aggregate; it has no separate identity.
type CodingState struct {
	ProjectID        string           `json:"project_id"`
	Repository       string           `json:"repository"`
	Workspace        string           `json:"workspace"`
	MachineID        string           `json:"machine_id"`
	Mode             string           `json:"execution_mode"`
	Assignee         string           `json:"assignee,omitempty"`
	Branch           string           `json:"branch"`
	BaseCommit       string           `json:"base_commit"`
	Managed          bool             `json:"managed"`
	WorkspaceReady   bool             `json:"workspace_ready"`
	WorkspaceRemoved bool             `json:"workspace_removed,omitempty"`
	InitialGit       GitState         `json:"initial_git"`
	Commands         []CodingCommand  `json:"commands,omitempty"`
	Artifacts        []CodingArtifact `json:"artifacts,omitempty"`
	Closeout         *CodingCloseout  `json:"closeout,omitempty"`
}

type ChangedFile struct {
	Path         string `json:"path"`
	OriginalPath string `json:"original_path,omitempty"`
	Index        string `json:"index"`
	Worktree     string `json:"worktree"`
	Source       string `json:"source"`
}

type GitState struct {
	Branch   string        `json:"branch"`
	Head     string        `json:"head"`
	Upstream string        `json:"upstream,omitempty"`
	Ahead    int           `json:"ahead"`
	Behind   int           `json:"behind"`
	Clean    bool          `json:"clean"`
	Files    []ChangedFile `json:"files"`
}

type CodingCommand struct {
	SessionID       string    `json:"session_id"`
	Command         string    `json:"command"`
	Workdir         string    `json:"workdir"`
	Purpose         string    `json:"purpose,omitempty"`
	OnFailure       string    `json:"on_failure,omitempty"`
	Validation      bool      `json:"validation"`
	StartedAt       time.Time `json:"started_at"`
	Status          string    `json:"status"`
	ExitCode        *int      `json:"exit_code,omitempty"`
	DurationMS      int64     `json:"duration_ms"`
	Output          string    `json:"output,omitempty"`
	OutputTruncated bool      `json:"output_truncated,omitempty"`
	LogArtifactID   string    `json:"log_artifact_id,omitempty"`
	Error           string    `json:"error,omitempty"`
}

type CodingArtifact struct {
	ArtifactID string `json:"artifact_id"`
	Purpose    string `json:"purpose"`
}

type CodingCloseout struct {
	Summary          string        `json:"summary"`
	ChangedFiles     []ChangedFile `json:"changed_files"`
	Git              GitState      `json:"git"`
	DiffArtifactID   string        `json:"diff_artifact_id,omitempty"`
	DiffTruncated    bool          `json:"diff_truncated"`
	ValidationResult string        `json:"validation_result"`
	ValidationNote   string        `json:"validation_note,omitempty"`
	RemainingIssues  []string      `json:"remaining_issues"`
	ReadyForReview   bool          `json:"ready_for_review"`
	FinishedAt       time.Time     `json:"finished_at"`
}

func (s *Store) BindCoding(id string, state CodingState) (Task, error) {
	return s.mutate(id, func(t *Task, now time.Time) error {
		if err := requireActive(t); err != nil {
			return err
		}
		if t.Coding != nil {
			return errors.New("task already has a coding workspace; resume it instead")
		}
		if t.Project != "" && t.Project != state.ProjectID {
			return errors.New("task project does not match coding project")
		}
		t.Project, t.Device = state.ProjectID, state.MachineID
		t.Coding = &state
		t.Phase = PhaseExecute
		appendTaskEvent(t, Event{Type: "coding.workspace_preparing", Summary: state.Workspace, CreatedAt: now})
		return nil
	})
}

// UpdateCoding participates in the same file/process lock and atomic task write.
func (s *Store) UpdateCoding(id, event, summary string, update func(*CodingState) error) (Task, error) {
	return s.mutate(id, func(t *Task, now time.Time) error {
		if err := requireMutable(t); err != nil {
			return err
		}
		if t.Coding == nil {
			return errors.New("task has no coding workspace")
		}
		if err := update(t.Coding); err != nil {
			return err
		}
		if event != "" {
			appendTaskEvent(t, Event{Type: event, Summary: summary, CreatedAt: now})
		}
		return nil
	})
}

func (s *Store) InvalidateCodingCloseout(id string) (Task, error) {
	return s.mutate(id, func(t *Task, now time.Time) error {
		if err := requireActive(t); err != nil {
			return err
		}
		if t.Coding == nil {
			return errors.New("task has no coding workspace")
		}
		if t.Coding.Closeout != nil {
			t.Coding.Closeout = nil
			t.FinalReview = nil
			t.Phase = PhaseExecute
			appendTaskEvent(t, Event{Type: "coding.changed", Summary: "new execution invalidated previous closeout", CreatedAt: now})
		}
		return nil
	})
}

func (s *Store) FinishCoding(id string, result CodingCloseout) (Task, error) {
	return s.mutate(id, func(t *Task, now time.Time) error {
		if err := requireActive(t); err != nil {
			return err
		}
		if t.Coding == nil {
			return errors.New("task has no coding workspace")
		}
		result.FinishedAt = now
		t.Coding.Closeout = &result
		t.Summary = result.Summary
		t.Phase = PhaseCloseout
		event := "coding.finished"
		if result.ReadyForReview {
			event = "coding.ready_for_review"
		}
		appendTaskEvent(t, Event{Type: event, Summary: result.Summary, CreatedAt: now})
		// Deliberately not Task.Complete: readiness is not human acceptance.
		return nil
	})
}

// MarkCodingWorkspaceRemoved changes resource metadata only. Completed task goals,
// review evidence and acceptance remain immutable when a worktree is cleaned up.
func (s *Store) MarkCodingWorkspaceRemoved(id string) (Task, error) {
	return s.mutate(id, func(t *Task, now time.Time) error {
		if t.Coding == nil || !t.Coding.Managed {
			return errors.New("task has no managed workspace")
		}
		if !t.Coding.WorkspaceRemoved {
			t.Coding.WorkspaceRemoved = true
			appendTaskEvent(t, Event{Type: "coding.workspace_removed", Summary: "managed worktree removed; branch retained", CreatedAt: now})
		}
		return nil
	})
}
