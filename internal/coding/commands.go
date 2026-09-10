// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package coding

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/uvwt/agentdock/internal/publicartifacts"
	"github.com/uvwt/agentdock/internal/taskstate"
	"github.com/uvwt/agentdock/internal/tool/command/session"
	toolcore "github.com/uvwt/agentdock/internal/tool/core"
)

// Execute records intent before execution and uses the existing command/session
// engine. No model, ACP, detached agent or second Job identity is involved.
func (s *Service) Execute(ctx context.Context, id string, args map[string]any, validation bool, purpose, onFailure string) (toolcore.Result, error) {
	if validation && (strings.TrimSpace(purpose) == "" || strings.TrimSpace(onFailure) == "") {
		return nil, errors.New("validation requires purpose and on_failure before running")
	}
	command, _ := args["cmd"].(string)
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("cmd is required")
	}
	s.lifecycle.Lock()
	if s.closing {
		s.lifecycle.Unlock()
		return nil, errors.New("coding execution is shutting down")
	}
	s.evidence.Add(1)
	s.lifecycle.Unlock()
	observing := false
	defer func() {
		if !observing {
			s.evidence.Done()
		}
	}()
	if _, err := s.Tasks.InvalidateCodingCloseout(id); err != nil {
		return nil, err
	}
	preview, err := s.commands.PreparePreview(args, command)
	if err != nil {
		return nil, err
	}
	record := taskstate.CodingCommand{Command: command, Workdir: preview.Workdir, Purpose: purpose, OnFailure: onFailure, Validation: validation, StartedAt: nowUTC(), Status: "starting"}
	index := 0
	// A same-process in-flight start is distinguishable from a lost start recovered
	// after restart. The map holds no independent Task/Job lifecycle.
	key := ""
	_, err = s.Tasks.UpdateCoding(id, "coding.command_started", purpose, func(c *taskstate.CodingState) error {
		index = len(c.Commands)
		key = fmt.Sprintf("%s/%d", id, index)
		s.starting.Store(key, true)
		c.Commands = append(c.Commands, record)
		return nil
	})
	if err != nil {
		if key != "" {
			s.starting.Delete(key)
		}
		return nil, err
	}
	defer func() {
		if !observing {
			s.starting.Delete(key)
		}
	}()
	var observed *session.Session
	result, runErr := s.commands.ExecObserved(ctx, args, func(value *session.Session) { observed = value })
	if runErr != nil {
		record.Status = "failed_to_start"
		record.Error = runErr.Error()
	} else {
		if observed != nil {
			record = commandEvidence(record, observed.Evidence(32768))
		} else {
			record = commandEvidence(record, result)
		}
	}
	if err = s.publishCommandLog(&record); err != nil {
		record.Error = err.Error()
	}
	_, saveErr := s.Tasks.UpdateCoding(id, "", "", func(c *taskstate.CodingState) error { c.Commands[index] = record; return nil })
	// Never hide an already-started execution behind an evidence persistence error.
	if saveErr != nil {
		return result, fmt.Errorf("command may have executed; inspect task %s / session %s rather than rerunning: %w", id, record.SessionID, saveErr)
	}
	if observed != nil && (record.Status == "running" || record.Status == "timeout" && record.ExitCode == nil) {
		// Hold the same native session until its final evidence is persisted, even
		// when session_observe consumes/removes it from the interactive session store.
		observing = true
		go func() {
			defer s.evidence.Done()
			defer s.starting.Delete(key)
			<-observed.Done
			final := commandEvidence(record, observed.Evidence(32768))
			if err := s.publishCommandLog(&final); err != nil {
				final.Error = err.Error()
			}
			_, saveErr := s.Tasks.UpdateCoding(id, "coding.command_finished", final.Status, func(c *taskstate.CodingState) error {
				c.Commands[index] = final
				return nil
			})
			if saveErr != nil {
				slog.Error("persist coding command evidence", "task_id", id, "session_id", final.SessionID, "error", saveErr)
			}
		}()
	}
	return result, runErr
}

// WaitEvidence is called after the existing command engine has stopped its
// children, so graceful shutdown retains terminal outcomes before returning.
func (s *Service) WaitEvidence() {
	s.lifecycle.Lock()
	s.closing = true
	s.lifecycle.Unlock()
	s.evidence.Wait()
}

func (s *Service) Refresh(id string) (taskstate.Task, error) {
	task, err := s.Task(id)
	if err != nil {
		return task, err
	}
	if task.Status == taskstate.StatusCompleted {
		return task, nil
	}
	updates := map[int]taskstate.CodingCommand{}
	for index, record := range task.Coding.Commands {
		if record.Status != "starting" && record.Status != "running" {
			continue
		}
		if _, inFlight := s.starting.Load(fmt.Sprintf("%s/%d", id, index)); inFlight {
			continue
		}
		if session, ok := s.commands.Store().Get(record.SessionID); ok {
			record = commandEvidence(record, session.Evidence(32768))
		} else {
			record.Status = "interrupted"
			record.Error = "command session is unavailable after runtime restart or retention expiry; outcome is unknown, never automatically rerun"
		}
		if err = s.publishCommandLog(&record); err != nil {
			record.Error = err.Error()
		}
		updates[index] = record
	}
	if len(updates) == 0 {
		return task, nil
	}
	return s.Tasks.UpdateCoding(id, "", "", func(c *taskstate.CodingState) error {
		for index, record := range updates {
			if index < len(c.Commands) && c.Commands[index].SessionID == record.SessionID {
				c.Commands[index] = record
			}
		}
		return nil
	})
}

func commandEvidence(record taskstate.CodingCommand, result map[string]any) taskstate.CodingCommand {
	if id, ok := result["session_id"].(string); ok {
		record.SessionID = id
	}
	if status, ok := result["status"].(string); ok {
		record.Status = status
	}
	if dir, ok := result["workdir"].(string); ok && dir != "" {
		record.Workdir = dir
	}
	if n, ok := result["exit_code"]; ok {
		value := int(number(n))
		record.ExitCode = &value
	}
	record.DurationMS = number(result["elapsed_ms"])
	stdout, _ := result["stdout"].(string)
	stderr, _ := result["stderr"].(string)
	record.Output = stdout
	if stderr != "" {
		record.Output += "\n[stderr]\n" + stderr
	}
	a, _ := result["stdout_truncated"].(bool)
	b, _ := result["stderr_truncated"].(bool)
	record.OutputTruncated = a || b
	if len(record.Output) > 65536 {
		record.Output = strings.ToValidUTF8(record.Output[:65536], "")
		record.OutputTruncated = true
	}
	if message, ok := result["command_error"].(string); ok {
		record.Error = message
	}
	return record
}

func number(value any) int64 {
	switch n := value.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func (s *Service) publishCommandLog(record *taskstate.CodingCommand) error {
	if !record.Validation || record.LogArtifactID != "" || record.Status == "starting" || record.Status == "running" || record.ExitCode == nil && record.Status != "interrupted" && record.Status != "failed_to_start" {
		return nil
	}
	text := fmt.Sprintf("Command: %s\nPurpose: %s\nOn failure: %s\nWorkdir: %s\nStatus: %s\nDuration ms: %d\nOutput truncated: %t\n\n%s\n%s\n", record.Command, record.Purpose, record.OnFailure, record.Workdir, record.Status, record.DurationMS, record.OutputTruncated, record.Output, record.Error)
	if record.ExitCode != nil {
		text = fmt.Sprintf("Exit code: %d\n", *record.ExitCode) + text
	}
	artifact, err := s.artifacts.PublishBytes(publicartifacts.PublishBytesRequest{Filename: "validation.log", Data: []byte(text), MimeType: "text/plain", RetentionSeconds: 604800})
	if err == nil {
		record.LogArtifactID = artifact.ArtifactID
	}
	return err
}
