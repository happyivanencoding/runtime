package app

import (
	"testing"

	"github.com/uvwt/agentdock/internal/computer/desktop"
	toolstartlux "github.com/uvwt/agentdock/internal/tool/startlux"
)

func TestStartLuxPolicyExecutionGate(t *testing.T) {
	base := toolstartlux.PolicyResult{
		NextAction:       toolstartlux.ChoiceAnswer{Choice: "press_001", Confidence: 0.9},
		TaskDone:         0.05,
		ActionRisk:       toolstartlux.ScoreAnswer{Score: 0.2, Confidence: 0.8},
		NeedConfirmation: 0.05,
		ContextState:     toolstartlux.ChoiceAnswer{Choice: "ready", Confidence: 0.9},
	}
	if got := startluxPolicyExecutionGate(base, true); got != "execute" {
		t.Fatalf("gate = %q, want execute", got)
	}
	confirm := base
	confirm.NeedConfirmation = 0.8
	if got := startluxPolicyExecutionGate(confirm, true); got != "confirm_required" {
		t.Fatalf("confirmation gate = %q, want confirm_required", got)
	}
	done := base
	done.TaskDone = 0.9
	if got := startluxPolicyExecutionGate(done, true); got != "done" {
		t.Fatalf("done gate = %q, want done", got)
	}
	blocked := base
	blocked.ContextState = toolstartlux.ChoiceAnswer{Choice: "blocked", Confidence: 0.9}
	if got := startluxPolicyExecutionGate(blocked, true); got != "blocked" {
		t.Fatalf("blocked gate = %q, want blocked", got)
	}
	uncertain := base
	uncertain.NextAction.Confidence = 0.2
	if got := startluxPolicyExecutionGate(uncertain, true); got != "uncertain" {
		t.Fatalf("uncertain gate = %q, want uncertain", got)
	}
	noneProbability := 0.45
	missing := base
	missing.NextAction.NoneProbability = &noneProbability
	if got := startluxPolicyExecutionGate(missing, true); got != "no_matching_action" {
		t.Fatalf("none gate = %q, want no_matching_action", got)
	}
	if got := startluxPolicyExecutionGate(base, false); got != "no_matching_action" {
		t.Fatalf("no action gate = %q, want no_matching_action", got)
	}
	complete := missing
	complete.TaskDone = 0.91
	if got := startluxPolicyExecutionGate(complete, false); got != "done" {
		t.Fatalf("completed task gate = %q, want done", got)
	}
}

func TestBuildStartLuxDesktopCandidatesUsesNativePatterns(t *testing.T) {
	nodes := []desktop.Node{
		{RuntimeID: "button", Name: "Save", ControlType: "button", Enabled: true, Patterns: []string{"invoke"}},
		{RuntimeID: "edit", Name: "Search", ControlType: "edit", Enabled: true, Patterns: []string{"value"}},
		{RuntimeID: "check", Name: "Enable", ControlType: "checkbox", Enabled: true, Patterns: []string{"toggle"}},
	}
	candidates := buildStartLuxDesktopCandidates(42, nodes, "query", true)
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.action != nil {
			seen[candidate.action.Action] = true
			if candidate.action.Selector.RuntimeID == "" {
				t.Fatalf("candidate %q lacks runtime_id selector", candidate.id)
			}
		}
	}
	for _, action := range []string{"press", "type", "focus", "toggle"} {
		if !seen[action] {
			t.Fatalf("missing %s candidate: %#v", action, candidates)
		}
	}
}
