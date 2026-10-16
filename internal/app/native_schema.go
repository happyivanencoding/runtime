// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package app

func nativeInputSchema(name string) (map[string]any, bool) {
	str := codingString
	integer := func(description string, min, max int) map[string]any {
		return map[string]any{"type": "integer", "description": description, "minimum": min, "maximum": max}
	}
	enumeration := func(values ...string) map[string]any { return map[string]any{"type": "string", "enum": values} }
	selector := codingObjectSchema(map[string]any{
		"runtime_id":    str("Actual UIA runtime id returned by desktop_inspect; rediscover when the control is recreated."),
		"automation_id": str("Exact UIA AutomationId."), "name": str("Exact accessible name."), "name_contains": str("Case-insensitive name substring."), "control_type": str("Control type, e.g. button, edit, checkbox, list, listitem, window. Selector fields are combined with AND."),
	})
	props := map[string]any{}
	required := []string{}
	switch name {
	case "lsp_manage":
		props["task_id"] = str("Existing Coding Task id from work_on_project.")
		props["action"] = enumeration("discover", "start", "status", "stop")
		props["language"] = str("Language, required for start/stop; go, typescript/javascript, python or a configured server.")
		required = []string{"task_id", "action"}
	case "lsp_query":
		props["task_id"] = str("Existing Coding Task id; uses its actual checkout/worktree.")
		props["action"] = enumeration("definition", "references", "hover", "diagnostics", "symbols", "workspace_symbols", "call_hierarchy")
		props["language"] = str("Optional language override; inferred from path except workspace_symbols.")
		props["path"] = str("Source file inside this task's workspace, absolute or workspace-relative.")
		props["line"] = integer("1-based source line for position queries.", 1, 1000000)
		props["character"] = integer("0-based UTF-16 code-unit offset, not a byte offset.", 0, 1000000)
		props["query"] = str("Search text for workspace_symbols.")
		props["direction"] = enumeration("incoming", "outgoing")
		props["limit"] = integer("Maximum returned top-level records, default 100.", 1, 500)
		required = []string{"task_id", "action"}
	case "desktop_inspect":
		props["action"] = enumeration("windows", "applications", "tree", "find")
		props["window_handle"] = map[string]any{"type": "integer", "minimum": 1, "description": "Exact handle from windows listing; required for tree/find. No whole-desktop descendant traversal."}
		props["process_id"] = integer("Optional process filter for windows/applications listing.", 1, 2147483647)
		props["selector"] = selector
		props["max_depth"] = integer("Maximum accessibility tree depth, default 8.", 1, 20)
		props["max_nodes"] = integer("Maximum visited nodes, default 150.", 1, 1000)
		required = []string{"action"}
	case "desktop_act":
		props["action"] = enumeration("focus", "press", "type", "scroll", "toggle", "select")
		props["window_handle"] = map[string]any{"type": "integer", "minimum": 1}
		props["selector"] = selector
		props["text"] = str("Replacement value for type, not keyboard events or clipboard paste.")
		props["direction"] = enumeration("up", "down", "left", "right")
		props["amount"] = enumeration("small", "large")
		required = []string{"action", "window_handle", "selector"}
	case "desktop_step", "desktop_run":
		props["window_handle"] = map[string]any{"type": "integer", "minimum": 1, "description": "Exact visible window handle from desktop_inspect."}
		props["goal"] = str("Goal StartLux should advance inside this Windows application window.")
		props["text"] = str("Optional caller-supplied replacement text that StartLux may choose to enter through UI Automation ValuePattern. The text itself is not sent to StartLux.")
		props["max_depth"] = integer("Maximum UI Automation tree depth considered by StartLux, default 8.", 1, 20)
		props["max_nodes"] = integer("Maximum UI Automation nodes considered by StartLux, default 180.", 1, 1000)
		if name == "desktop_run" {
			props["max_steps"] = integer("Maximum bounded StartLux UIA decisions. Defaults to 8; maximum 16.", 1, 16)
			props["max_duration_ms"] = integer("Overall desktop-run deadline in milliseconds. Defaults to 60000; maximum 85000.", 1000, 85000)
		}
		required = []string{"window_handle", "goal"}
	case "desktop_clipboard":
		props["action"] = enumeration("read", "write")
		props["text"] = str("Unicode text for an explicitly requested clipboard write.")
		required = []string{"action"}
	case "desktop_screen":
		props["window_handle"] = map[string]any{"type": "integer", "minimum": 1}
		props["task_id"] = str("Optional existing Coding Task to attach screenshot Artifact.")
		required = []string{"window_handle"}
	default:
		return nil, false
	}
	return codingObjectSchema(props, required...), true
}

func nativeOutputSchema(name string) (map[string]any, bool) {
	object := func() map[string]any { return map[string]any{"type": "object", "additionalProperties": true} }
	array := func() map[string]any { return map[string]any{"type": "array", "items": object()} }
	props := map[string]any{}
	required := []string{}
	switch name {
	case "lsp_manage":
		for _, k := range []string{"workspace", "language", "configuration", "command", "status", "stderr_tail"} {
			props[k] = codingString(k)
		}
		props["pid"] = map[string]any{"type": "integer"}
		props["stopped"] = map[string]any{"type": "boolean"}
		props["args"] = codingStrings("Native server arguments.")
		props["servers"] = array()
		props["capabilities"] = object()
		required = []string{"workspace"}
	case "lsp_query":
		for _, k := range []string{"workspace", "language", "action", "method", "direction", "status", "source", "uri", "position_encoding", "note", "freshness_note"} {
			props[k] = codingString(k)
		}
		props["data"] = map[string]any{"description": "Native LSP result; shape depends on method/server."}
		props["truncated"] = map[string]any{"type": "boolean"}
		props["document_version"] = map[string]any{"type": "integer"}
		props["server_version"] = map[string]any{"type": []string{"integer", "null"}}
		required = []string{"workspace", "language", "action", "data", "truncated"}
	case "desktop_inspect":
		props["backend"] = codingString("Native backend.")
		for _, k := range []string{"windows", "applications", "elements"} {
			props[k] = array()
		}
		for _, k := range []string{"window_handle", "visited", "skipped_elements"} {
			props[k] = map[string]any{"type": "integer"}
		}
		props["truncated"] = map[string]any{"type": "boolean"}
		required = []string{"backend"}
	case "desktop_act":
		props["backend"] = codingString("Native backend.")
		props["action"] = codingString("Performed operation.")
		props["window_handle"] = map[string]any{"type": "integer"}
		props["performed"] = map[string]any{"type": "boolean"}
		props["before"] = object()
		props["after"] = object()
		props["observation_note"] = codingString("Post-action observation limitation.")
		required = []string{"backend", "action", "window_handle", "performed", "before"}
	case "desktop_step":
		props["backend"] = codingString("windows-uia+startlux.")
		props["window_handle"] = map[string]any{"type": "integer"}
		props["window"] = object()
		props["elements"] = array()
		props["visited"] = map[string]any{"type": "integer"}
		props["skipped_elements"] = map[string]any{"type": "integer"}
		props["truncated"] = map[string]any{"type": "boolean"}
		props["startlux_model"] = codingString("Concrete StartLux model version.")
		props["startlux_choice"] = codingString("Closed-set action selected by StartLux; empty if no candidate fits.")
		props["startlux_confidence"] = map[string]any{"type": "number", "minimum": 0, "maximum": 1}
		props["startlux_none_probability"] = map[string]any{"type": "number", "minimum": 0, "maximum": 1}
		props["startlux_policy"] = object()
		props["selected_action"] = object()
		props["execution_gate"] = codingString("execute, done, blocked, uncertain, no_matching_action, or confirm_required.")
		props["confirm_required"] = map[string]any{"type": "boolean"}
		props["executed"] = map[string]any{"type": "boolean"}
		props["action_result"] = object()
		required = []string{"backend", "window_handle", "elements", "startlux_model", "startlux_choice", "execution_gate", "confirm_required", "executed"}
	case "desktop_run":
		props["domain"] = codingString("Computer execution domain: desktop.")
		props["window_handle"] = map[string]any{"type": "integer"}
		props["status"] = codingString("completed, stopped or failed.")
		props["stop_reason"] = codingString("Reason the bounded UIA run stopped.")
		props["completion_source"] = codingString("startlux_policy when the model declares the goal complete.")
		for _, key := range []string{"max_steps", "max_duration_ms", "steps_observed", "steps_executed", "elapsed_ms"} {
			props[key] = map[string]any{"type": "integer"}
		}
		props["steps"] = array()
		props["last_result"] = object()
		props["startlux_model"] = codingString("Concrete StartLux model.")
		props["error"] = codingString("Error message if the run failed.")
		required = []string{"domain", "window_handle", "status", "stop_reason", "max_steps", "max_duration_ms", "steps_observed", "steps_executed", "elapsed_ms", "steps"}
	case "desktop_clipboard":
		props["has_text"] = map[string]any{"type": "boolean"}
		props["text"] = codingString("Clipboard text.")
		props["written"] = map[string]any{"type": "boolean"}
		props["characters"] = map[string]any{"type": "integer"}
	case "desktop_screen":
		props["artifact"] = object()
		props["artifact_id"] = codingString("Existing Artifact identity.")
		props["bounds"] = object()
		props["window_handle"] = map[string]any{"type": "integer"}
		props["capture_mode"] = codingString("Capture semantics.")
		required = []string{"artifact_id", "bounds", "window_handle", "capture_mode"}
	default:
		return nil, false
	}
	return codingObjectSchema(props, required...), true
}
