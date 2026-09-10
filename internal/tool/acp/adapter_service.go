// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package acp

import (
	"context"
	"strings"
	"sync"
	"time"

	acpruntime "github.com/uvwt/agentdock/internal/acp"
)

// AdapterService exposes the compact Runtime ACP surface. It is deliberately
// lazy: status/discovery never launches an agent process.
type AdapterService struct {
	registry           *acpruntime.AdapterRegistry
	mu                 sync.Mutex
	latestRunBySession map[string]string
}

func NewAdapterService(registry *acpruntime.AdapterRegistry) *AdapterService {
	return &AdapterService{registry: registry, latestRunBySession: map[string]string{}}
}

func (s *AdapterService) Start(ctx context.Context, args map[string]any) (Result, error) {
	manager, adapter, err := s.registry.Manager(
		stringArg(args, "agent", "codex"), stringArg(args, "command", ""),
		stringSliceArg(args, "args"), stringMapArg(args, "env_from_env"),
	)
	if err != nil {
		return nil, acpToolError(err)
	}
	session, err := manager.NewSession(ctx, stringArg(args, "cwd", ""), stringSliceArg(args, "additional_directories"))
	if err != nil {
		return nil, acpToolError(err)
	}
	result := Result{"action": "start", "adapter": adapter, "session": session.Session, "agent": session.Agent, "status": session.Session.Status}
	if session.Modes != nil {
		result["modes"] = session.Modes
	}
	if session.ConfigOptions != nil {
		result["config_options"] = session.ConfigOptions
	}
	if prompt := strings.TrimSpace(stringArg(args, "prompt", "")); prompt != "" {
		run, err := manager.StartPrompt(ctx, session.Session.ID, prompt)
		if err != nil {
			return nil, acpToolError(err)
		}
		s.rememberRun(session.Session.ID, run.RunID)
		result["run_id"] = run.RunID
		result["status"] = run.Status
		result["started_at"] = run.StartedAt
	}
	return result, nil
}

func (s *AdapterService) Resume(ctx context.Context, args map[string]any) (Result, error) {
	sessionID := stringArg(args, "session_id", "")
	manager, adapter, _, err := s.registry.ManagerForSession(
		sessionID, stringArg(args, "command", ""), stringSliceArg(args, "args"), stringMapArg(args, "env_from_env"),
	)
	if err != nil {
		return nil, acpToolError(err)
	}
	session, err := manager.ResumeSession(ctx, sessionID)
	if err != nil {
		return nil, acpToolError(err)
	}
	result := Result{"action": "resume", "adapter": adapter, "session": session.Session, "agent": session.Agent, "status": session.Session.Status}
	if prompt := strings.TrimSpace(stringArg(args, "prompt", "")); prompt != "" {
		run, err := manager.StartPrompt(ctx, sessionID, prompt)
		if err != nil {
			return nil, acpToolError(err)
		}
		s.rememberRun(sessionID, run.RunID)
		result["run_id"] = run.RunID
		result["status"] = run.Status
		result["started_at"] = run.StartedAt
	}
	return result, nil
}

func (s *AdapterService) Status(ctx context.Context, args map[string]any) (Result, error) {
	sessionID := strings.TrimSpace(stringArg(args, "session_id", ""))
	if sessionID == "" {
		sessions, err := s.registry.ListSessions()
		if err != nil {
			return nil, acpToolError(err)
		}
		return Result{
			"action": "status", "default_active": false,
			"adapters": s.registry.AdapterStatuses(), "sessions": sessions, "count": len(sessions),
		}, nil
	}
	session, err := s.registry.FindSession(sessionID)
	if err != nil {
		return nil, acpToolError(err)
	}
	result := Result{"action": "status", "session": session, "status": session.Status}
	manager := s.registry.ExistingManager(session.Agent)
	runID := strings.TrimSpace(stringArg(args, "run_id", ""))
	if runID == "" {
		runID = s.latestRun(sessionID)
	}
	if manager != nil {
		pending := manager.ListInteractions(sessionID, true)
		result["interactions"] = pending
		result["interaction_count"] = len(pending)
		if runID != "" {
			after := intArg(args, "after_seq", 0)
			if after < 0 {
				return nil, validationError("ACP_AFTER_SEQ_INVALID", "after_seq must not be negative", nil)
			}
			limit := intArg(args, "limit", 100)
			if limit < 1 {
				limit = 1
			}
			if limit > 200 {
				limit = 200
			}
			waitMS := intArg(args, "wait_ms", 0)
			if waitMS < 0 {
				waitMS = 0
			}
			if waitMS > 25000 {
				waitMS = 25000
			}
			events, err := manager.PromptEvents(ctx, runID, uint64(after), limit, time.Duration(waitMS)*time.Millisecond)
			if err != nil {
				return nil, acpToolError(err)
			}
			result["run_id"] = events.RunID
			result["status"] = events.Status
			result["events"] = events.Events
			result["next_seq"] = events.NextSeq
			result["has_more"] = events.HasMore
			result["truncated"] = events.Truncated
			result["stop_reason"] = events.StopReason
			result["error_code"] = events.ErrorCode
			result["message"] = events.Message
			result["started_at"] = events.StartedAt
			if events.EndedAt != nil {
				result["ended_at"] = events.EndedAt
			}
		}
	}
	return result, nil
}

func (s *AdapterService) Stop(ctx context.Context, args map[string]any) (Result, error) {
	sessionID := stringArg(args, "session_id", "")
	manager, adapter, _, err := s.registry.ManagerForSession(
		sessionID, stringArg(args, "command", ""), stringSliceArg(args, "args"), stringMapArg(args, "env_from_env"),
	)
	if err != nil {
		return nil, acpToolError(err)
	}
	runID := strings.TrimSpace(stringArg(args, "run_id", ""))
	if runID == "" {
		runID = s.latestRun(sessionID)
	}
	_ = manager.CancelPrompt(ctx, sessionID, runID)
	session, err := manager.CloseSession(ctx, sessionID)
	if err != nil {
		return nil, acpToolError(err)
	}
	s.mu.Lock()
	delete(s.latestRunBySession, sessionID)
	s.mu.Unlock()
	return Result{"action": "stop", "adapter": adapter, "session": session, "status": session.Status, "stopped": true}, nil
}

func (s *AdapterService) rememberRun(sessionID, runID string) {
	s.mu.Lock()
	s.latestRunBySession[sessionID] = runID
	s.mu.Unlock()
}
func (s *AdapterService) latestRun(sessionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latestRunBySession[sessionID]
}

func stringMapArg(args map[string]any, key string) map[string]string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	result := map[string]string{}
	switch value := raw.(type) {
	case map[string]string:
		for k, v := range value {
			result[k] = v
		}
	case map[string]any:
		for k, v := range value {
			if text, ok := v.(string); ok {
				result[k] = text
			}
		}
	}
	return result
}

func (s *AdapterService) Close() error {
	if s == nil || s.registry == nil {
		return nil
	}
	return s.registry.Close()
}
