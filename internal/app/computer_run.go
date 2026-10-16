package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Computer runs reuse the existing single-step policy and native execution paths.
// The loop owns no browser session, desktop window, task queue, or global lock.
type computerStepFunc func(context.Context, map[string]any) (Result, error)

func (r *Runtime) browserRun(ctx context.Context, args map[string]any) (Result, error) {
	if err := validateBrowserKeys(args,
		"session_id", "page_id", "goal", "text", "url", "key", "files",
		"full_page", "max_text_chars", "max_dom_chars", "max_interactive_elements",
		"retention_seconds", "timeout_ms", "max_steps", "max_duration_ms",
	); err != nil {
		return nil, err
	}
	if _, err := requiredString(args, "session_id"); err != nil {
		return nil, err
	}
	if _, err := requiredString(args, "goal"); err != nil {
		return nil, err
	}
	return runComputerSteps(ctx, "browser", args, r.browserStep)
}

func (r *Runtime) desktopRun(ctx context.Context, args map[string]any) (Result, error) {
	if _, err := requiredUint64(args, "window_handle"); err != nil {
		return nil, err
	}
	if _, err := requiredString(args, "goal"); err != nil {
		return nil, err
	}
	for key := range args {
		switch key {
		case "window_handle", "goal", "text", "max_depth", "max_nodes", "max_steps", "max_duration_ms":
		default:
			return nil, fmt.Errorf("desktop_run does not support argument %q", key)
		}
	}
	return runComputerSteps(ctx, "desktop", args, r.desktopStep)
}

func runComputerSteps(ctx context.Context, domain string, args map[string]any, step computerStepFunc) (Result, error) {
	maxSteps, err := intArgRange(args, "max_steps", 8, 1, 16)
	if err != nil {
		return nil, err
	}
	maxDuration, err := intArgRange(args, "max_duration_ms", 60000, 1000, 85000)
	if err != nil {
		return nil, err
	}
	base := make(map[string]any, len(args))
	for key, value := range args {
		if key != "max_steps" && key != "max_duration_ms" {
			base[key] = value
		}
	}
	// Only Browser supports the existing per-step timeout argument.
	browserStepTimeout := 30000
	if domain == "browser" {
		browserStepTimeout, err = intArgRange(args, "timeout_ms", 30000, 1, 300000)
		if err != nil {
			return nil, err
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(maxDuration)*time.Millisecond)
	defer cancel()
	started := time.Now()
	trace := make([]map[string]any, 0, maxSteps)
	result := Result{
		"domain":          domain,
		"status":          "stopped",
		"stop_reason":     "max_steps",
		"max_steps":       maxSteps,
		"max_duration_ms": maxDuration,
		"steps":           trace,
	}
	if domain == "browser" {
		result["session_id"] = args["session_id"]
	} else {
		result["window_handle"] = args["window_handle"]
	}
	var last Result
	lastFingerprint, lastAction := "", ""
	stagnantRepeats := 0
	executedCount := 0

	for index := 0; index < maxSteps; index++ {
		if runCtx.Err() != nil {
			result["stop_reason"] = computerContextStopReason(runCtx)
			break
		}
		currentArgs := make(map[string]any, len(base))
		for key, value := range base {
			currentArgs[key] = value
		}
		if domain == "browser" {
			remaining := int(time.Until(started.Add(time.Duration(maxDuration) * time.Millisecond)).Milliseconds())
			if remaining <= 0 {
				result["stop_reason"] = "max_duration"
				break
			}
			if remaining < browserStepTimeout {
				currentArgs["timeout_ms"] = remaining
			} else {
				currentArgs["timeout_ms"] = browserStepTimeout
			}
		}
		stepStart := time.Now()
		next, callErr := step(runCtx, currentArgs)
		record := map[string]any{"step": index + 1, "duration_ms": time.Since(stepStart).Milliseconds()}
		if next != nil {
			last = next
			gate, _ := next["execution_gate"].(string)
			choice, _ := next["startlux_choice"].(string)
			ran, _ := next["executed"].(bool)
			record["choice"] = choice
			record["execution_gate"] = gate
			record["executed"] = ran
			if confidence, ok := next["startlux_confidence"]; ok {
				record["confidence"] = confidence
			}
			if probability, ok := next["startlux_none_probability"]; ok {
				record["none_probability"] = probability
			}
			if selected, ok := next["selected_action"].(map[string]any); ok {
				record["action"] = selected["action"]
			}
		}
		trace = append(trace, record)
		if callErr != nil {
			if runCtx.Err() != nil {
				result["stop_reason"] = computerContextStopReason(runCtx)
			} else {
				result["status"] = "failed"
				result["stop_reason"] = "error"
				result["error"] = map[string]any{"message": callErr.Error()}
			}
			break
		}
		if next == nil || next["error"] != nil || (domain == "browser" && next["browser_ok"] == false) {
			result["status"] = "failed"
			result["stop_reason"] = "error"
			if next != nil {
				result["error"] = next["error"]
			}
			break
		}
		gate, _ := next["execution_gate"].(string)
		if gate == "done" {
			result["status"] = "completed"
			result["stop_reason"] = "model_done"
			result["completion_source"] = "startlux_policy"
			break
		}
		if gate != "execute" {
			if gate == "" {
				result["status"] = "failed"
				result["stop_reason"] = "missing_execution_gate"
			} else {
				result["stop_reason"] = gate
			}
			break
		}
		ran, _ := next["executed"].(bool)
		if !ran {
			result["status"] = "failed"
			result["stop_reason"] = "action_not_executed"
			break
		}
		executedCount++

		fingerprint := computerStateFingerprint(domain, next)
		action := computerActionIdentity(next)
		if fingerprint != "" && lastFingerprint == fingerprint && action != "" && action == lastAction && computerActionStagnationCandidate(next) {
			stagnantRepeats++
		} else {
			stagnantRepeats = 0
		}
		record["state_changed"] = lastFingerprint != "" && lastFingerprint != fingerprint
		lastFingerprint, lastAction = fingerprint, action
		if stagnantRepeats >= 2 {
			result["stop_reason"] = "no_progress"
			break
		}
	}
	if err := runCtx.Err(); err != nil && result["stop_reason"] == "max_steps" {
		result["stop_reason"] = computerContextStopReason(runCtx)
	}
	result["steps"] = trace
	result["steps_observed"] = len(trace)
	result["steps_executed"] = executedCount
	result["elapsed_ms"] = time.Since(started).Milliseconds()
	if last != nil {
		result["last_result"] = last
		if model, ok := last["startlux_model"]; ok {
			result["startlux_model"] = model
		}
	}
	return result, nil
}

func computerContextStopReason(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "max_duration"
	}
	return "cancelled"
}

func computerStateFingerprint(domain string, result Result) string {
	var value any
	if domain == "browser" {
		value = []any{result["url"], result["title"], result["text"], result["dom"], result["interactive_elements"]}
	} else {
		value = result["elements"]
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) == 0 {
		return ""
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:])
}

func computerActionIdentity(result Result) string {
	selected, _ := result["selected_action"].(map[string]any)
	if selected == nil {
		return ""
	}
	return fmt.Sprint(selected["action"], "|", selected["selector"], "|", selected["target"], "|", selected["option"])
}

func computerActionStagnationCandidate(result Result) bool {
	selected, _ := result["selected_action"].(map[string]any)
	if selected == nil {
		return false
	}
	action, _ := selected["action"].(string)
	switch strings.ToLower(action) {
	case "wait", "scroll", "back", "forward", "goto", "reload":
		return false
	default:
		return action != ""
	}
}
