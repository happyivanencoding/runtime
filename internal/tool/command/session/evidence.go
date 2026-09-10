// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package session

import "time"

// Evidence is a non-consuming snapshot of retained command output. Coding
// closeout must not lose logs merely because a client already observed them.
func (s *Session) Evidence(maxBytes int) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := "running"
	end := time.Now()
	if s.completed {
		status = "exited"
		end = s.FinishedAt
	}
	if s.TimedOut {
		status = "timeout"
	}
	stdout, stderr := s.stdout.String(), s.stderr.String()
	result := map[string]any{
		"session_id": s.ID, "status": status, "stdout": trim(stdout, maxBytes), "stderr": trim(stderr, maxBytes),
		"elapsed_ms": end.Sub(s.StartedAt).Milliseconds(), "workdir": s.execution.Workdir,
		"stdout_truncated": s.stdoutDroppedBytes > 0 || len(stdout) > maxBytes,
		"stderr_truncated": s.stderrDroppedBytes > 0 || len(stderr) > maxBytes,
	}
	if s.completed {
		result["exit_code"] = s.exitCode
	}
	return result
}
