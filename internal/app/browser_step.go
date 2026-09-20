package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	toolbrowser "github.com/uvwt/agentdock/internal/tool/browser"
	tooljev "github.com/uvwt/agentdock/internal/tool/jev"
)

type jevBrowserCandidate struct {
	id          string
	description string
	action      *toolbrowser.Action
	summary     map[string]any
}

func jevPolicyExecutionGate(policy tooljev.PolicyResult, hasAction bool) string {
	if !hasAction || policy.TaskDone >= 0.85 ||
		(policy.ContextState.Choice == "completed" && policy.ContextState.Confidence >= 0.60) {
		return "done"
	}
	if policy.ContextState.Confidence >= 0.70 &&
		(policy.ContextState.Choice == "blocked" || policy.ContextState.Choice == "error") {
		return "blocked"
	}
	if policy.NextAction.Confidence < 0.35 {
		return "uncertain"
	}
	if policy.NeedConfirmation >= 0.65 || policy.ActionRisk.Score >= 1.50 {
		return "confirm_required"
	}
	return "execute"
}

func (r *Runtime) browserStep(ctx context.Context, args map[string]any) (Result, error) {
	if err := validateBrowserKeys(args, "session_id", "page_id", "goal", "text", "url", "key", "files", "full_page", "max_text_chars", "max_dom_chars", "max_interactive_elements", "retention_seconds", "close_after", "timeout_ms"); err != nil {
		return browserFailure(err), nil
	}
	if r.jev == nil || !r.jev.Configured() {
		return nil, fmt.Errorf("TypeSafe Jev is not configured for Runtime browser control")
	}
	sessionID, err := requiredString(args, "session_id")
	if err != nil {
		return browserFailure(err), nil
	}
	goal, err := requiredString(args, "goal")
	if err != nil {
		return browserFailure(err), nil
	}
	pageID, err := optionalString(args, "page_id", "")
	if err != nil {
		return browserFailure(err), nil
	}
	inputText, err := optionalString(args, "text", "")
	if err != nil {
		return browserFailure(err), nil
	}
	targetURL, err := optionalString(args, "url", "")
	if err != nil {
		return browserFailure(err), nil
	}
	key, err := optionalString(args, "key", "")
	if err != nil {
		return browserFailure(err), nil
	}
	var files []string
	if raw, ok := args["files"]; ok {
		files, err = browserStringArray(raw, "files", 1, 32)
		if err != nil {
			return browserFailure(err), nil
		}
	}
	fullPage, err := boolArgStrict(args, "full_page", false)
	if err != nil {
		return browserFailure(err), nil
	}
	closeAfter, err := boolArgStrict(args, "close_after", false)
	if err != nil {
		return browserFailure(err), nil
	}
	maxText, err := intArgRange(args, "max_text_chars", 6000, 1, 50000)
	if err != nil {
		return browserFailure(err), nil
	}
	maxDOM, err := intArgRange(args, "max_dom_chars", 12000, 1, 200000)
	if err != nil {
		return browserFailure(err), nil
	}
	maxInteractive, err := intArgRange(args, "max_interactive_elements", 80, 1, 200)
	if err != nil {
		return browserFailure(err), nil
	}
	retention, err := intArgRange(args, "retention_seconds", 0, 0, 604800)
	if err != nil {
		return browserFailure(err), nil
	}
	timeout, err := durationArg(args, "timeout_ms", 30*time.Second, time.Millisecond, 5*time.Minute)
	if err != nil {
		return browserFailure(err), nil
	}

	snapshot, err := r.browser.Snapshot(ctx, toolbrowser.SnapshotRequest{
		SessionID: sessionID, PageID: pageID, FullPage: fullPage,
		MaxTextChars: maxText, MaxDOMChars: maxDOM, MaxInteractiveElements: maxInteractive, Timeout: timeout,
	})
	if err != nil {
		return browserFailure(err), nil
	}
	candidates := buildJevBrowserCandidates(snapshot, inputText, targetURL, key, files)
	criteria := make(map[string]string, len(candidates))
	candidateByID := make(map[string]jevBrowserCandidate, len(candidates))
	for _, candidate := range candidates {
		criteria[candidate.id] = candidate.description
		candidateByID[candidate.id] = candidate
	}

	policy, err := r.jev.Policy(ctx, tooljev.PolicyRequest{
		State:                    browserStepJevState(goal, snapshot, candidates),
		ActionCriteria:           criteria,
		ActionInstructions:       "Choose exactly one next browser action that best advances the goal from the current page state. Prefer direct task progress over exploration. Choose done only when the goal is already satisfied. The listed actions are the only actions Runtime can execute in this step.",
		DoneInstructions:         "Is the user's browser goal already satisfied in the current page state, so no further browser action is needed?",
		RiskInstructions:         "Rate the operational risk of executing the best next browser action for the goal from the available action candidates.",
		ConfirmationInstructions: "Should Runtime require explicit human confirmation before executing the best next browser action because it is sensitive, externally visible, destructive, financial, authorization-related, privacy-sensitive, or otherwise consequential?",
		ContextStateInstructions: "Classify the current browser page state for deciding what Runtime should do next.",
		ContextStateCriteria: map[string]string{
			"ready":     "The page is usable and has enough information for a normal next action",
			"loading":   "The page is still loading, transitioning, or waiting for content that is likely to appear shortly",
			"blocked":   "Progress is blocked by login, captcha, permission, modal confirmation, missing user input, or another condition requiring intervention",
			"error":     "The page is in an error or failed state",
			"completed": "The user's stated browser goal is visibly satisfied",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("Jev browser policy: %w", err)
	}
	selected, ok := candidateByID[policy.NextAction.Choice]
	if !ok {
		return nil, fmt.Errorf("Jev browser policy returned unknown choice %q", policy.NextAction.Choice)
	}

	gate := jevPolicyExecutionGate(policy, selected.action != nil)
	execute := gate == "execute"

	var result Result
	if !execute {
		result, err = r.publishBrowserSnapshot(ctx, snapshot, retention)
	} else {
		actionSnapshot, actionErr := r.browser.Act(ctx, toolbrowser.ActRequest{
			SessionID: sessionID, PageID: pageID, Actions: []toolbrowser.Action{*selected.action}, FullPage: fullPage,
			MaxTextChars: maxText, MaxDOMChars: maxDOM, MaxInteractiveElements: maxInteractive, Timeout: timeout,
		})
		if actionErr != nil {
			return browserFailure(actionErr), nil
		}
		result, err = r.publishBrowserSnapshot(ctx, actionSnapshot, retention)
	}
	if err != nil {
		return nil, err
	}
	if closeAfter && (execute || gate == "done") {
		if _, err := r.browser.CloseSession(toolbrowser.CloseRequest{SessionID: sessionID}); err != nil {
			return browserFailure(err), nil
		}
		result["closed"] = true
	}
	result["jev_model"] = policy.Model
	result["jev_choice"] = policy.NextAction.Choice
	result["jev_confidence"] = policy.NextAction.Confidence
	result["jev_policy"] = map[string]any{
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
		"page_state": map[string]any{
			"choice": policy.ContextState.Choice, "confidence": policy.ContextState.Confidence,
			"probabilities": policy.ContextState.Probabilities,
		},
	}
	result["selected_action"] = selected.summary
	result["execution_gate"] = gate
	result["confirm_required"] = gate == "confirm_required"
	result["executed"] = execute
	return result, nil
}

func buildJevBrowserCandidates(snapshot toolbrowser.Snapshot, inputText, targetURL, key string, files []string) []jevBrowserCandidate {
	const maxCandidates = 240
	candidates := make([]jevBrowserCandidate, 0, 64)
	appendCandidate := func(candidate jevBrowserCandidate) {
		if len(candidates) < maxCandidates-1 {
			candidates = append(candidates, candidate)
		}
	}
	for index, element := range snapshot.InteractiveElements {
		selector := strings.TrimSpace(element.Selector)
		if selector == "" || element.Disabled {
			continue
		}
		position := index + 1
		label := browserStepElementLabel(element)
		if strings.ToLower(element.Type) != "file" {
			action := toolbrowser.Action{Kind: "click", Click: &toolbrowser.ClickAction{Selector: selector}}
			appendCandidate(jevBrowserCandidate{
				id:          fmt.Sprintf("click_%03d", position),
				description: fmt.Sprintf("Click visible element %d (%s): %s", position, browserStepElementKind(element), label),
				action:      &action,
				summary:     map[string]any{"action": "click", "selector": selector, "target": label},
			})
		}
		if inputText != "" && element.IsEditable {
			tag := strings.ToLower(element.Tag)
			typeName := strings.ToLower(element.Type)
			switch {
			case tag == "textarea" || (tag == "input" && browserStepTextInputType(typeName)):
				action := toolbrowser.Action{Kind: "fill", Fill: &toolbrowser.FillAction{Selector: selector, Value: inputText}}
				appendCandidate(jevBrowserCandidate{
					id:          fmt.Sprintf("fill_%03d", position),
					description: fmt.Sprintf("Replace the contents of editable element %d with the caller-supplied text: %s", position, label),
					action:      &action,
					summary:     map[string]any{"action": "fill", "selector": selector, "target": label, "input_supplied": true},
				})
			case tag != "select" && typeName != "file":
				action := toolbrowser.Action{Kind: "type", Type: &toolbrowser.TypeAction{Selector: selector, Text: inputText}}
				appendCandidate(jevBrowserCandidate{
					id:          fmt.Sprintf("type_%03d", position),
					description: fmt.Sprintf("Type the caller-supplied text into editable element %d: %s", position, label),
					action:      &action,
					summary:     map[string]any{"action": "type", "selector": selector, "target": label, "input_supplied": true},
				})
			}
		}
		if strings.EqualFold(element.Tag, "select") {
			optionLimit := len(element.Options)
			if optionLimit > 20 {
				optionLimit = 20
			}
			for optionIndex := 0; optionIndex < optionLimit; optionIndex++ {
				option := element.Options[optionIndex]
				if option.Disabled {
					continue
				}
				optionLabel := strings.TrimSpace(option.Text)
				if optionLabel == "" {
					optionLabel = option.Value
				}
				action := toolbrowser.Action{Kind: "select", Select: &toolbrowser.SelectAction{Selector: selector, Value: option.Value}}
				appendCandidate(jevBrowserCandidate{
					id:          fmt.Sprintf("select_%03d_%02d", position, optionIndex+1),
					description: fmt.Sprintf("Select option %q in element %d: %s", truncateBrowserStepText(optionLabel, 100), position, label),
					action:      &action,
					summary:     map[string]any{"action": "select", "selector": selector, "target": label, "option": truncateBrowserStepText(optionLabel, 100)},
				})
			}
		}
		if len(files) > 0 && strings.EqualFold(element.Tag, "input") && strings.EqualFold(element.Type, "file") {
			action := toolbrowser.Action{Kind: "upload", Upload: &toolbrowser.UploadAction{Selector: selector, Paths: append([]string(nil), files...)}}
			appendCandidate(jevBrowserCandidate{
				id:          fmt.Sprintf("upload_%03d", position),
				description: fmt.Sprintf("Upload the caller-supplied file(s) through element %d: %s", position, label),
				action:      &action,
				summary:     map[string]any{"action": "upload", "selector": selector, "target": label, "file_count": len(files)},
			})
		}
	}
	if strings.TrimSpace(targetURL) != "" {
		action := toolbrowser.Action{Kind: "goto", Goto: &toolbrowser.GotoAction{URL: targetURL, WaitUntil: toolbrowser.WaitLoad}}
		appendCandidate(jevBrowserCandidate{id: "goto_supplied_url", description: "Navigate to the caller-supplied URL.", action: &action, summary: map[string]any{"action": "goto", "url": targetURL}})
	}
	if strings.TrimSpace(key) != "" {
		selector := ""
		if snapshot.FocusedElement != nil {
			selector = strings.TrimSpace(snapshot.FocusedElement.Selector)
		}
		action := toolbrowser.Action{Kind: "press", Press: &toolbrowser.PressAction{Selector: selector, Key: key}}
		appendCandidate(jevBrowserCandidate{id: "press_supplied_key", description: "Press the caller-supplied key on the currently focused element.", action: &action, summary: map[string]any{"action": "press", "key": key}})
	}
	scrollDown := toolbrowser.Action{Kind: "scroll", Scroll: &toolbrowser.ScrollAction{DeltaY: 720}}
	appendCandidate(jevBrowserCandidate{id: "scroll_down", description: "Scroll the current page down to reveal later content.", action: &scrollDown, summary: map[string]any{"action": "scroll", "direction": "down"}})
	scrollUp := toolbrowser.Action{Kind: "scroll", Scroll: &toolbrowser.ScrollAction{DeltaY: -720}}
	appendCandidate(jevBrowserCandidate{id: "scroll_up", description: "Scroll the current page up to reveal earlier content.", action: &scrollUp, summary: map[string]any{"action": "scroll", "direction": "up"}})
	back := toolbrowser.Action{Kind: "back", Navigation: &toolbrowser.NavigationAction{WaitUntil: toolbrowser.WaitLoad}}
	appendCandidate(jevBrowserCandidate{id: "go_back", description: "Navigate back one entry in browser history.", action: &back, summary: map[string]any{"action": "back"}})
	forward := toolbrowser.Action{Kind: "forward", Navigation: &toolbrowser.NavigationAction{WaitUntil: toolbrowser.WaitLoad}}
	appendCandidate(jevBrowserCandidate{id: "go_forward", description: "Navigate forward one entry in browser history.", action: &forward, summary: map[string]any{"action": "forward"}})
	reload := toolbrowser.Action{Kind: "reload", Navigation: &toolbrowser.NavigationAction{WaitUntil: toolbrowser.WaitLoad}}
	appendCandidate(jevBrowserCandidate{id: "reload_page", description: "Reload the current page.", action: &reload, summary: map[string]any{"action": "reload"}})
	wait := toolbrowser.Action{Kind: "wait", Wait: &toolbrowser.WaitAction{Duration: 500 * time.Millisecond}}
	appendCandidate(jevBrowserCandidate{id: "wait_500ms", description: "Wait briefly for the page to update or finish loading.", action: &wait, summary: map[string]any{"action": "wait", "milliseconds": 500}})
	candidates = append(candidates, jevBrowserCandidate{id: "done", description: "Do not execute another browser action because the goal is already satisfied.", summary: map[string]any{"action": "done"}})
	return candidates
}

func browserStepJevState(goal string, snapshot toolbrowser.Snapshot, candidates []jevBrowserCandidate) map[string]any {
	elements := make([]map[string]any, 0, len(snapshot.InteractiveElements))
	for index, element := range snapshot.InteractiveElements {
		entry := map[string]any{
			"id": fmt.Sprintf("element_%03d", index+1), "tag": element.Tag, "type": element.Type,
			"name": element.Name, "text": element.Text, "aria_name": element.ARIAName, "placeholder": element.Placeholder,
			"role": element.Role, "href": sanitizeBrowserStepURL(element.Href), "disabled": element.Disabled, "is_editable": element.IsEditable,
		}
		if len(element.Options) > 0 {
			options := make([]map[string]any, 0, len(element.Options))
			for _, option := range element.Options {
				options = append(options, map[string]any{"text": option.Text, "value": option.Value, "disabled": option.Disabled})
			}
			entry["options"] = options
		}
		elements = append(elements, entry)
	}
	var focused map[string]any
	if snapshot.FocusedElement != nil {
		focused = map[string]any{
			"tag": snapshot.FocusedElement.Tag, "id": snapshot.FocusedElement.ID, "name": snapshot.FocusedElement.Name,
			"type": snapshot.FocusedElement.Type, "text": snapshot.FocusedElement.Text, "aria_name": snapshot.FocusedElement.ARIAName,
			"is_editable": snapshot.FocusedElement.IsEditable,
		}
	}
	availableActions := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		availableActions = append(availableActions, map[string]any{
			"id": candidate.id, "description": candidate.description,
		})
	}
	return map[string]any{
		"goal":              goal,
		"available_actions": availableActions,
		"page": map[string]any{
			"url": sanitizeBrowserStepURL(snapshot.URL), "title": snapshot.Title, "visible_text": snapshot.Text,
			"focused_element": focused, "interactive_elements": elements,
		},
	}
}

func browserStepElementKind(element toolbrowser.InteractiveElement) string {
	parts := []string{strings.ToLower(strings.TrimSpace(element.Tag))}
	if value := strings.ToLower(strings.TrimSpace(element.Type)); value != "" {
		parts = append(parts, "type="+value)
	}
	if value := strings.ToLower(strings.TrimSpace(element.Role)); value != "" {
		parts = append(parts, "role="+value)
	}
	return strings.Join(parts, ", ")
}

func browserStepElementLabel(element toolbrowser.InteractiveElement) string {
	for _, value := range []string{element.ARIAName, element.Text, element.Placeholder, element.Name} {
		if value = strings.TrimSpace(value); value != "" {
			return truncateBrowserStepText(value, 160)
		}
	}
	if href := sanitizeBrowserStepURL(element.Href); href != "" {
		return href
	}
	return browserStepElementKind(element)
}

func sanitizeBrowserStepURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return truncateBrowserStepText(value, 240)
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return truncateBrowserStepText(parsed.String(), 240)
}

func browserStepTextInputType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "text", "email", "search", "tel", "url", "password", "number", "date", "datetime-local", "month", "time", "week":
		return true
	default:
		return false
	}
}

func truncateBrowserStepText(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}
