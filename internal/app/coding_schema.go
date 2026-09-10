// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package app

func codingString(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
func codingStrings(description string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": description}
}
func codingObjectSchema(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func withCodingContext(name string, schema map[string]any) map[string]any {
	if codingContextTool(name) {
		props, _ := schema["properties"].(map[string]any)
		props["task_id"] = codingString("Optional existing coding Task from work_on_project. Relative paths/workdir inherit its saved workspace; no global cwd is changed. Omit for ordinary General-tool behavior.")
	}
	return schema
}

func codingInputSchema(name string) (map[string]any, bool) {
	str := codingString
	props := map[string]any{"task_id": str("Existing Task id returned by work_on_project.")}
	required := []string{"task_id"}
	switch name {
	case "project_registry":
		props = map[string]any{
			"action": map[string]any{"type": "string", "enum": []string{"register", "get", "list"}}, "id": str("Project id for get."),
			"project": codingObjectSchema(map[string]any{
				"id": str("Stable lower-case project id."), "name": str("Display name."), "repo_path": str("Absolute local repository path."),
				"remote": str("Remote URL, otherwise discovered from origin. Do not include credentials."), "machine_id": str("Local machine id; omitted means this machine."),
				"stack": codingStrings("Declared stack, otherwise inferred from manifest files."), "instructions": str("Project-specific instructions."),
				"instruction_files": codingStrings("Instruction entrypoints, default AGENTS.md and CLAUDE.md. Inspect nested rules before edits."),
				"capabilities":      codingStrings("Declared project capabilities, not permission grants."), "default_branch": str("Default branch, otherwise inferred from origin/HEAD or current branch."),
				"devices":    map[string]any{"type": "array", "description": "Declared project devices, not live connectivity evidence.", "items": codingObjectSchema(map[string]any{"id": str("Device id."), "name": str("Name."), "kind": str("Kind, e.g. android.")}, "id")},
				"deployment": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Non-secret deployment metadata. Never store keys or tokens."},
			}, "id", "repo_path"),
		}
		required = []string{"action"}
	case "work_on_project":
		props["project"] = str("Registered project id. Required for new work, optional for task_id continuation.")
		props["task"] = str("Concrete instruction, required for new coding work.")
		props["execution_mode"] = map[string]any{"type": "string", "enum": []string{"interactive-owner", "delegated-task"}, "description": "Default interactive-owner uses current checkout; delegated-task creates a managed worktree."}
		props["assignee"] = str("Assigned person or agent label, not an execution backend.")
		props["base_ref"] = str("Optional base for a new delegated worktree, default HEAD. Resolved to a commit before creation.")
		props["completion_conditions"] = codingStrings("Optional completion conditions for the existing Task system.")
		props["steps"] = map[string]any{"type": "array", "maxItems": 12, "items": codingObjectSchema(map[string]any{"id": str("Step id."), "title": str("Step title.")}, "id", "title")}
		required = nil
	case "coding_task", "git_status", "git_changed_files", "git_branches":
	case "git_diff":
		props["scope"] = map[string]any{"type": "string", "enum": []string{"task", "staged", "unstaged"}}
		props["paths"] = codingStrings("Optional literal repository-relative path filters.")
	case "git_commits":
		props["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 100}
	case "git_worktree":
		props["action"] = map[string]any{"type": "string", "enum": []string{"list", "remove"}}
		required = append(required, "action")
	case "git_commit":
		props["message"] = str("Commit message.")
		props["paths"] = codingStrings("Explicit repository-relative files to commit; no implicit add-all.")
		required = append(required, "message", "paths")
	case "validation_run":
		schema := InputSchema("exec_command")
		props = schema["properties"].(map[string]any)
		props["purpose"] = str("Before running: what specific failure can this check discover?")
		props["on_failure"] = str("Before running: what will you do differently if it fails?")
		required = []string{"task_id", "cmd", "purpose", "on_failure"}
	case "finish_coding_task":
		props["summary"] = str("Concrete implementation/result summary.")
		props["remaining_issues"] = codingStrings("Known remaining issues, including relevant pre-existing problems.")
		props["ready_for_review"] = map[string]any{"type": "boolean", "description": "Explicit readiness declaration, never human acceptance; default false."}
		props["validation_note"] = str("Explain validation limitations. Required for readiness when no executable check ran.")
		required = append(required, "summary")
	default:
		return nil, false
	}
	return codingObjectSchema(props, required...), true
}

func codingOutputSchema(name string) (map[string]any, bool) {
	object := func() map[string]any { return map[string]any{"type": "object", "additionalProperties": true} }
	array := func() map[string]any { return map[string]any{"type": "array", "items": object()} }
	props := map[string]any{}
	required := []string{}
	switch name {
	case "project_registry":
		props["project"] = object()
		props["projects"] = array()
		props["machine"] = object()
	case "work_on_project":
		for _, field := range []string{"task_id", "repository", "workspace", "branch", "assignee", "execution_mode", "device_discovery", "next_action"} {
			props[field] = codingString(field)
		}
		for _, field := range []string{"project", "machine", "task", "git"} {
			props[field] = object()
		}
		for _, field := range []string{"instruction_files", "connected_devices"} {
			props[field] = array()
		}
		props["available_capabilities"] = codingStrings("Implemented native capabilities.")
		required = []string{"task_id", "project", "machine", "task", "workspace", "git"}
	case "coding_task", "finish_coding_task":
		props["task_id"] = codingString("Existing Task id.")
		props["task"] = object()
		required = []string{"task_id", "task"}
	case "git_status":
		for _, field := range []string{"branch", "head", "upstream"} {
			props[field] = codingString(field)
		}
		for _, field := range []string{"ahead", "behind"} {
			props[field] = map[string]any{"type": "integer"}
		}
		props["clean"] = map[string]any{"type": "boolean"}
		props["files"] = array()
		required = []string{"branch", "head", "ahead", "behind", "clean", "files"}
	case "git_diff":
		props["diff"] = codingString("Unified Git diff.")
		props["scope"] = codingString("Comparison scope.")
		props["truncated"] = map[string]any{"type": "boolean"}
		required = []string{"diff", "scope", "truncated"}
	case "git_changed_files":
		props["files"] = array()
		required = []string{"files"}
	case "git_commits":
		props["commits"] = array()
		required = []string{"commits"}
	case "git_branches":
		props["branches"] = array()
		required = []string{"branches"}
	case "git_worktree":
		props["worktrees"] = array()
		props["task_id"] = codingString("Task id after removal.")
		props["task"] = object()
	case "git_commit":
		props["commit"] = codingString("Resulting commit id.")
		required = []string{"commit"}
	case "validation_run":
		return OutputSchema("exec_command"), true
	default:
		return nil, false
	}
	return codingObjectSchema(props, required...), true
}
