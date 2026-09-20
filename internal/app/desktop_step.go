package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/uvwt/agentdock/internal/computer/desktop"
	tooljev "github.com/uvwt/agentdock/internal/tool/jev"
)

type jevDesktopCandidate struct {
	id          string
	description string
	action      *desktop.Request
	summary     map[string]any
}

func (r *Runtime) desktopStep(ctx context.Context, args map[string]any) (Result, error) {
	if r.jev == nil || !r.jev.Configured() {
		return nil, errors.New("TypeSafe Jev is not configured for Runtime desktop control")
	}
	windowHandle, err := requiredUint64(args, "window_handle")
	if err != nil {
		return nil, err
	}
	goal, err := requiredString(args, "goal")
	if err != nil {
		return nil, err
	}
	maxDepth, err := intArgRange(args, "max_depth", 8, 1, 20)
	if err != nil {
		return nil, err
	}
	maxNodes, err := intArgRange(args, "max_nodes", 180, 1, 1000)
	if err != nil {
		return nil, err
	}
	textValue, _ := args["text"].(string)
	_, textSupplied := args["text"]

	window := r.desktopWindowSummary(ctx, windowHandle)
	tree, nodes, err := r.desktopTree(ctx, windowHandle, maxDepth, maxNodes)
	if err != nil {
		return nil, err
	}
	candidates := buildJevDesktopCandidates(windowHandle, nodes, textValue, textSupplied)
	criteria := make(map[string]string, len(candidates))
	candidateByID := make(map[string]jevDesktopCandidate, len(candidates))
	for _, candidate := range candidates {
		criteria[candidate.id] = candidate.description
		candidateByID[candidate.id] = candidate
	}

	policy, err := r.jev.Policy(ctx, tooljev.PolicyRequest{
		State:                    desktopStepJevState(goal, window, nodes, candidates),
		ActionCriteria:           criteria,
		ActionInstructions:       "Choose exactly one next Windows desktop UI Automation action that best advances the user's goal. Prefer direct task progress. Choose done only when the goal is already satisfied. Choose only from the listed Runtime UIA actions.",
		DoneInstructions:         "Is the user's desktop goal already satisfied in the current window state, so no further UI Automation action is needed?",
		RiskInstructions:         "Rate the operational risk of executing the best next Windows desktop action from the available Runtime UIA candidates.",
		ConfirmationInstructions: "Should Runtime require explicit human confirmation before executing the best next desktop action because it is sensitive, externally visible, destructive, financial, authorization-related, privacy-sensitive, installation/system-setting related, or otherwise consequential?",
		ContextStateInstructions: "Classify the current Windows application state for deciding what Runtime should do next.",
		ContextStateCriteria: map[string]string{
			"ready":     "The window is usable and exposes enough controls for a normal next action",
			"busy":      "The application appears busy, loading, processing, or not yet ready for the intended action",
			"blocked":   "Progress is blocked by login, permission, confirmation, missing user input, inaccessible controls, or another condition requiring intervention",
			"error":     "The application is visibly in an error or failed state",
			"completed": "The user's stated desktop goal is visibly satisfied",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("Jev desktop policy: %w", err)
	}
	selected, ok := candidateByID[policy.NextAction.Choice]
	if !ok {
		return nil, fmt.Errorf("Jev desktop policy returned unknown choice %q", policy.NextAction.Choice)
	}

	gate := jevPolicyExecutionGate(policy, selected.action != nil)
	execute := gate == "execute"
	var actionResult map[string]any
	if execute {
		actionResult, err = r.desktop.Act(ctx, *selected.action)
		if err != nil {
			return nil, err
		}
		if refreshed, refreshedNodes, inspectErr := r.desktopTree(ctx, windowHandle, maxDepth, maxNodes); inspectErr == nil {
			tree = refreshed
			nodes = refreshedNodes
		}
	}

	result := Result{
		"backend":          "windows-uia+jev",
		"window_handle":    windowHandle,
		"window":           window,
		"elements":         tree["elements"],
		"visited":          tree["visited"],
		"skipped_elements": tree["skipped_elements"],
		"truncated":        tree["truncated"],
		"jev_model":        policy.Model,
		"jev_choice":       policy.NextAction.Choice,
		"jev_confidence":   policy.NextAction.Confidence,
		"jev_policy": map[string]any{
			"next_action": map[string]any{
				"choice": policy.NextAction.Choice, "confidence": policy.NextAction.Confidence,
				"probabilities": policy.NextAction.Probabilities,
			},
			"task_done": policy.TaskDone,
			"action_risk": map[string]any{
				"score": policy.ActionRisk.Score, "confidence": policy.ActionRisk.Confidence,
				"probabilities": policy.ActionRisk.Probabilities, "legend": policy.ActionRisk.Legend,
			},
			"need_confirmation": policy.NeedConfirmation,
			"desktop_state": map[string]any{
				"choice": policy.ContextState.Choice, "confidence": policy.ContextState.Confidence,
				"probabilities": policy.ContextState.Probabilities,
			},
		},
		"selected_action":  selected.summary,
		"execution_gate":   gate,
		"confirm_required": gate == "confirm_required",
		"executed":         execute,
	}
	if actionResult != nil {
		result["action_result"] = actionResult
	}
	return result, nil
}

func (r *Runtime) desktopTree(ctx context.Context, windowHandle uint64, maxDepth, maxNodes int) (map[string]any, []desktop.Node, error) {
	tree, err := r.desktop.Inspect(ctx, desktop.Request{
		Action: "tree", WindowHandle: windowHandle, MaxDepth: maxDepth, MaxNodes: maxNodes,
	})
	if err != nil {
		return nil, nil, err
	}
	nodes, ok := tree["elements"].([]desktop.Node)
	if !ok {
		return nil, nil, errors.New("desktop inspection returned an unexpected element shape")
	}
	return tree, nodes, nil
}

func (r *Runtime) desktopWindowSummary(ctx context.Context, handle uint64) map[string]any {
	result, err := r.desktop.Inspect(ctx, desktop.Request{Action: "windows"})
	if err != nil {
		return map[string]any{"window_handle": handle}
	}
	windows, ok := result["windows"].([]desktop.Window)
	if !ok {
		return map[string]any{"window_handle": handle}
	}
	for _, window := range windows {
		if window.Handle == handle {
			return map[string]any{
				"window_handle": window.Handle,
				"application":   window.Application,
				"title":         window.Title,
				"class_name":    window.ClassName,
				"foreground":    window.Foreground,
			}
		}
	}
	return map[string]any{"window_handle": handle}
}

func buildJevDesktopCandidates(windowHandle uint64, nodes []desktop.Node, text string, textSupplied bool) []jevDesktopCandidate {
	const maxCandidates = 240
	candidates := make([]jevDesktopCandidate, 0, 96)
	appendCandidate := func(candidate jevDesktopCandidate) {
		if len(candidates) < maxCandidates-1 {
			candidates = append(candidates, candidate)
		}
	}
	for index, node := range nodes {
		if !node.Enabled || node.Offscreen || strings.TrimSpace(node.RuntimeID) == "" {
			continue
		}
		position := index + 1
		label := desktopNodeLabel(node)
		selector := desktop.Selector{RuntimeID: node.RuntimeID}
		has := func(pattern string) bool {
			for _, current := range node.Patterns {
				if current == pattern {
					return true
				}
			}
			return false
		}
		if has("invoke") {
			action := desktop.Request{Action: "press", WindowHandle: windowHandle, Selector: selector}
			appendCandidate(jevDesktopCandidate{
				id:          fmt.Sprintf("press_%03d", position),
				description: fmt.Sprintf("Invoke control %d (%s): %s", position, node.ControlType, label),
				action:      &action,
				summary:     map[string]any{"action": "press", "selector": map[string]any{"runtime_id": node.RuntimeID}, "target": label},
			})
		}
		if has("value") && textSupplied {
			action := desktop.Request{Action: "type", WindowHandle: windowHandle, Selector: selector, Text: text}
			appendCandidate(jevDesktopCandidate{
				id:          fmt.Sprintf("type_%03d", position),
				description: fmt.Sprintf("Replace the value of control %d (%s) with caller-supplied text: %s", position, node.ControlType, label),
				action:      &action,
				summary:     map[string]any{"action": "type", "selector": map[string]any{"runtime_id": node.RuntimeID}, "target": label, "input_supplied": true},
			})
		}
		if has("toggle") {
			action := desktop.Request{Action: "toggle", WindowHandle: windowHandle, Selector: selector}
			appendCandidate(jevDesktopCandidate{
				id:          fmt.Sprintf("toggle_%03d", position),
				description: fmt.Sprintf("Toggle control %d (%s): %s", position, node.ControlType, label),
				action:      &action,
				summary:     map[string]any{"action": "toggle", "selector": map[string]any{"runtime_id": node.RuntimeID}, "target": label},
			})
		}
		if has("selection_item") {
			action := desktop.Request{Action: "select", WindowHandle: windowHandle, Selector: selector}
			appendCandidate(jevDesktopCandidate{
				id:          fmt.Sprintf("select_%03d", position),
				description: fmt.Sprintf("Select control %d (%s): %s", position, node.ControlType, label),
				action:      &action,
				summary:     map[string]any{"action": "select", "selector": map[string]any{"runtime_id": node.RuntimeID}, "target": label},
			})
		}
		if has("scroll") {
			for _, direction := range []string{"down", "up"} {
				action := desktop.Request{Action: "scroll", WindowHandle: windowHandle, Selector: selector, Direction: direction, Amount: "small"}
				appendCandidate(jevDesktopCandidate{
					id:          fmt.Sprintf("scroll_%s_%03d", direction, position),
					description: fmt.Sprintf("Scroll %s within control %d (%s): %s", direction, position, node.ControlType, label),
					action:      &action,
					summary:     map[string]any{"action": "scroll", "selector": map[string]any{"runtime_id": node.RuntimeID}, "target": label, "direction": direction, "amount": "small"},
				})
			}
		}
		if node.ControlType == "edit" || node.ControlType == "combobox" {
			action := desktop.Request{Action: "focus", WindowHandle: windowHandle, Selector: selector}
			appendCandidate(jevDesktopCandidate{
				id:          fmt.Sprintf("focus_%03d", position),
				description: fmt.Sprintf("Focus control %d (%s): %s", position, node.ControlType, label),
				action:      &action,
				summary:     map[string]any{"action": "focus", "selector": map[string]any{"runtime_id": node.RuntimeID}, "target": label},
			})
		}
	}
	candidates = append(candidates, jevDesktopCandidate{
		id: "done", description: "Do not execute another desktop action because the goal is already satisfied.",
		summary: map[string]any{"action": "done"},
	})
	return candidates
}

func desktopStepJevState(goal string, window map[string]any, nodes []desktop.Node, candidates []jevDesktopCandidate) map[string]any {
	elements := make([]map[string]any, 0, len(nodes))
	for index, node := range nodes {
		entry := map[string]any{
			"id":            fmt.Sprintf("element_%03d", index+1),
			"runtime_id":    node.RuntimeID,
			"name":          node.Name,
			"automation_id": node.AutomationID,
			"control_type":  node.ControlType,
			"class_name":    node.ClassName,
			"enabled":       node.Enabled,
			"offscreen":     node.Offscreen,
			"focused":       node.Focused,
			"password":      node.Password,
			"patterns":      node.Patterns,
		}
		if node.ToggleState != nil {
			entry["toggle_state"] = *node.ToggleState
		}
		if node.Selected != nil {
			entry["selected"] = *node.Selected
		}
		elements = append(elements, entry)
	}
	availableActions := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		availableActions = append(availableActions, map[string]any{
			"id": candidate.id, "description": candidate.description,
		})
	}
	return map[string]any{
		"goal":              goal,
		"window":            window,
		"elements":          elements,
		"available_actions": availableActions,
	}
}

func desktopNodeLabel(node desktop.Node) string {
	for _, value := range []string{node.Name, node.AutomationID, node.ClassName} {
		if value = strings.TrimSpace(value); value != "" {
			return truncateBrowserStepText(value, 160)
		}
	}
	return node.ControlType
}

func requiredUint64(args map[string]any, field string) (uint64, error) {
	raw, ok := args[field]
	if !ok {
		return 0, fmt.Errorf("%s is required", field)
	}
	switch value := raw.(type) {
	case float64:
		if value < 1 || value != float64(uint64(value)) {
			return 0, fmt.Errorf("%s must be a positive integer", field)
		}
		return uint64(value), nil
	case int:
		if value < 1 {
			return 0, fmt.Errorf("%s must be a positive integer", field)
		}
		return uint64(value), nil
	case int64:
		if value < 1 {
			return 0, fmt.Errorf("%s must be a positive integer", field)
		}
		return uint64(value), nil
	case uint64:
		if value < 1 {
			return 0, fmt.Errorf("%s must be a positive integer", field)
		}
		return value, nil
	default:
		return 0, fmt.Errorf("%s must be a positive integer", field)
	}
}
