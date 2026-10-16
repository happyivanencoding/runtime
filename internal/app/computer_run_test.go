package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	toolbrowser "github.com/uvwt/agentdock/internal/tool/browser"
)

func exampleComputerResult(domain, gate, action, choice, state string) Result {
	r := Result{
		"execution_gate": gate, "executed": gate == "execute",
		"startlux_model":  "startlux-decision-27b-autothink",
		"startlux_choice": choice, "startlux_confidence": 0.94,
		"startlux_none_probability": 0.02,
		"selected_action":           map[string]any{"action": action, "selector": "#action", "target": "Test action"},
	}
	if domain == "browser" {
		r["browser_ok"] = true
		r["text"] = state
		r["dom"] = "<main>" + state + "</main>"
		r["url"] = "https://test.invalid/"
		r["interactive_elements"] = []string{"button"}
	} else {
		r["elements"] = []map[string]any{{"name": state, "automation_id": "label"}}
	}
	return r
}

func TestComputerRunCompletesAndReturnsOnlyLastObservation(t *testing.T) {
	for _, domain := range []string{"browser", "desktop"} {
		t.Run(domain, func(t *testing.T) {
			n := 0
			args := map[string]any{"max_steps": 8, "max_duration_ms": 60000, "goal": "Finish", "session_id": "browser-test", "window_handle": uint64(42)}
			result, err := runComputerSteps(context.Background(), domain, args, func(_ context.Context, input map[string]any) (Result, error) {
				n++
				if _, found := input["max_steps"]; found {
					t.Fatal("run-only options passed to single-step executor")
				}
				if domain == "browser" {
					if _, exists := input["timeout_ms"]; !exists {
						t.Fatal("browser step timeout was not bounded")
					}
				}
				if n == 4 {
					return exampleComputerResult(domain, "done", "done", "done", "Finished"), nil
				}
				return exampleComputerResult(domain, "execute", "click", fmt.Sprintf("click_%d", n), fmt.Sprintf("State %d", n)), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if result["status"] != "completed" || result["stop_reason"] != "model_done" {
				t.Fatalf("expected complete, got %#v", result)
			}
			if result["steps_executed"] != 3 || result["steps_observed"] != 4 {
				t.Fatalf("unexpected counters: %#v", result)
			}
			if result["completion_source"] != "startlux_policy" {
				t.Fatalf("missing model-declared completion source: %#v", result)
			}
			if _, exists := result["session_id"]; !exists && domain == "browser" {
				t.Fatal("missing browser session identifier")
			}
			if _, ok := result["last_result"].(Result); !ok {
				t.Fatal("last_result is not present")
			}
		})
	}
}

func TestComputerRunStopsBeforeUnsafeExecution(t *testing.T) {
	for _, gate := range []string{"confirm_required", "no_matching_action", "blocked", "uncertain"} {
		t.Run(gate, func(t *testing.T) {
			n := 0
			result, err := runComputerSteps(context.Background(), "desktop", map[string]any{"max_steps": 16, "window_handle": uint64(42)}, func(_ context.Context, _ map[string]any) (Result, error) {
				n++
				return exampleComputerResult("desktop", gate, "press", "press_001", "Ready"), nil
			})
			if err != nil || n != 1 || result["stop_reason"] != gate || result["steps_executed"] != 0 {
				t.Fatalf("gate %s resulted in %#v, error %v, calls %d", gate, result, err, n)
			}
		})
	}
}

func TestComputerRunStopsAfterRepeatedUnproductiveAction(t *testing.T) {
	for _, domain := range []string{"browser", "desktop"} {
		t.Run(domain, func(t *testing.T) {
			result, err := runComputerSteps(context.Background(), domain, map[string]any{"max_steps": 12}, func(_ context.Context, _ map[string]any) (Result, error) {
				return exampleComputerResult(domain, "execute", "click", "click_001", "unchanged"), nil
			})
			if err != nil || result["stop_reason"] != "no_progress" || result["steps_observed"] != 3 {
				t.Fatalf("unexpected no-progress result %#v (err %v)", result, err)
			}
		})
	}
}

func TestComputerRunAllowsRepeatedScrollButRespectsStepLimit(t *testing.T) {
	result, err := runComputerSteps(context.Background(), "browser", map[string]any{"max_steps": 4}, func(_ context.Context, _ map[string]any) (Result, error) {
		return exampleComputerResult("browser", "execute", "scroll", "scroll_down", "unchanged"), nil
	})
	if err != nil || result["stop_reason"] != "max_steps" || result["steps_executed"] != 4 {
		t.Fatalf("unexpected max-step result %#v (err %v)", result, err)
	}
}

func TestComputerRunReturnsPartialResultOnError(t *testing.T) {
	n := 0
	result, err := runComputerSteps(context.Background(), "desktop", map[string]any{"max_steps": 8}, func(_ context.Context, _ map[string]any) (Result, error) {
		n++
		if n == 1 {
			return exampleComputerResult("desktop", "execute", "press", "press_001", "Open"), nil
		}
		return nil, errors.New("temporary UIA provider error")
	})
	if err != nil || result["status"] != "failed" || result["stop_reason"] != "error" {
		t.Fatalf("expected partial failure, got %#v (%v)", result, err)
	}
	if result["steps_executed"] != 1 || result["steps_observed"] != 2 {
		t.Fatalf("expected completed step preserved, got %#v", result)
	}
}

func TestComputerRunCancelledAndInvalidBoundaries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := runComputerSteps(ctx, "desktop", nil, func(_ context.Context, _ map[string]any) (Result, error) {
		t.Fatal("step started after cancellation")
		return nil, nil
	})
	if err != nil || result["stop_reason"] != "cancelled" || result["steps_observed"] != 0 {
		t.Fatalf("unexpected cancelled result %#v %v", result, err)
	}
	for _, args := range []map[string]any{{"max_steps": 17}, {"max_duration_ms": 86000}, {"timeout_ms": -1}} {
		if _, err := runComputerSteps(context.Background(), "browser", args, func(context.Context, map[string]any) (Result, error) { return nil, nil }); err == nil {
			t.Errorf("invalid run parameters accepted: %#v", args)
		}
	}
}

func TestComputerRunDeadlineBoundsSingleStep(t *testing.T) {
	start := time.Now()
	result, err := runComputerSteps(context.Background(), "browser", map[string]any{"max_duration_ms": 1000}, func(ctx context.Context, opts map[string]any) (Result, error) {
		timeout, ok := opts["timeout_ms"].(int)
		if !ok || timeout > 1000 {
			t.Fatalf("per-step timeout not bounded by overall deadline: %#v", opts)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil || result["status"] == "failed" || result["stop_reason"] != "max_duration" {
		t.Fatalf("deadline not returned cleanly: %#v %v", result, err)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("deadline took too long: %s", elapsed)
	}
}

func TestComputerRunRejectsUnsupportedDesktopKeysAndUnknownBrowserKeys(t *testing.T) {
	r := &Runtime{}
	_, err := r.desktopRun(context.Background(), map[string]any{"window_handle": uint64(42), "goal": "test", "force_click": true})
	if err == nil || !strings.Contains(err.Error(), "force_click") {
		t.Fatalf("unsupported desktop_run argument not rejected: %v", err)
	}
	_, err = r.browserRun(context.Background(), map[string]any{"session_id": "x", "goal": "test", "force_click": true})
	if err == nil {
		t.Fatal("unsupported browser_run argument was accepted")
	}
}

func TestComputerRunPublishedSchemasAndSuccessResults(t *testing.T) {
	for _, tc := range []struct{ domain, tool string }{
		{"browser", "browser_run"},
		{"desktop", "desktop_run"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			input := InputSchema(tc.tool)
			props, _ := input["properties"].(map[string]any)
			for _, key := range []string{"goal", "max_steps", "max_duration_ms"} {
				if _, ok := props[key]; !ok {
					t.Fatalf("%s input schema missing %s", tc.tool, key)
				}
			}
			if tc.domain == "browser" && input["additionalProperties"] != false {
				t.Fatal("browser_run must reject unrecognized arguments")
			}
			out, err := runComputerSteps(context.Background(), tc.domain, map[string]any{
				"session_id": "browser-test", "window_handle": uint64(42),
			}, func(context.Context, map[string]any) (Result, error) {
				return exampleComputerResult(tc.domain, "done", "done", "done", "Task complete"), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			assertToolResultMatchesOutputSchema(t, tc.tool, out)
		})
	}
}

func TestBrowserStepExposesSelectedOptionAndSkipsReselect(t *testing.T) {
	snapshot := toolbrowser.Snapshot{
		InteractiveElements: []toolbrowser.InteractiveElement{{
			Tag: "select", Selector: "#team", ARIAName: "Team",
			Options: []toolbrowser.SelectOption{
				{Value: "Risk", Text: "Risk", Selected: true},
				{Value: "Equity", Text: "Equity"},
			},
		}},
	}
	candidates := buildStartLuxBrowserCandidates(snapshot, "", "", "", nil)
	foundRisk, foundEquity := false, false
	for _, candidate := range candidates {
		if candidate.id == "select_001_01" {
			foundRisk = true
		}
		if candidate.id == "select_001_02" {
			foundEquity = true
		}
	}
	if foundRisk || !foundEquity {
		t.Fatalf("candidate list must exclude the currently selected option")
	}
	state := browserStepStartLuxState("Choose Equity", snapshot, candidates)
	page := state["page"].(map[string]any)
	elems := page["interactive_elements"].([]map[string]any)
	options := elems[0]["options"].([]map[string]any)
	if options[0]["selected"] != true || options[1]["selected"] != false {
		t.Fatalf("selected state missing from model input: %+v", options)
	}
}
