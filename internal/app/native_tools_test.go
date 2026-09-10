// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNativeExecutionBindingsAndExplicitMutations(t *testing.T) {
	rt := newRuntimeValidationTestRuntime(t)
	if rt.acp != nil {
		t.Fatal("native execution initialized ACP")
	}
	repo := codingTestRepo(t, "native context")
	id := codingTestStart(t, rt, "native", repo)
	codingTestCall(t, rt, "lsp_manage", map[string]any{"task_id": id, "action": "discover"})
	codingTestCall(t, rt, "lsp_manage", map[string]any{"task_id": id, "action": "status"})
	if _, err := rt.Call(context.Background(), "lsp_query", map[string]any{"task_id": "missing", "action": "symbols", "path": "sample.py"}); err == nil {
		t.Fatal("missing task accepted")
	}
	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{"desktop_act", map[string]any{"action": "type", "window_handle": 1, "selector": map[string]any{"name": "test"}}},
		{"desktop_clipboard", map[string]any{"action": "write"}},
	} {
		if _, err := rt.Call(context.Background(), test.name, test.args); err == nil || !strings.Contains(err.Error(), "explicit text") {
			t.Fatalf("%s did not reject implicit clear before any OS mutation: %v", test.name, err)
		}
	}
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		codingTestCall(t, rt, "desktop_inspect", map[string]any{"action": "windows", "process_id": os.Getpid()})
		codingTestCall(t, rt, "desktop_inspect", map[string]any{"action": "applications", "process_id": os.Getpid()})
	}
}

func TestNativeLSPThroughTaskTools(t *testing.T) {
	sourceHome := os.Getenv("RUNTIME_LSP_TEST_HOME")
	if sourceHome == "" {
		t.Skip("set RUNTIME_LSP_TEST_HOME for actual native server integration")
	}
	config, err := os.ReadFile(filepath.Join(sourceHome, "lsp-servers.json"))
	if err != nil {
		t.Fatal(err)
	}
	rt := newRuntimeValidationTestRuntime(t)
	if err = os.WriteFile(filepath.Join(rt.cfg.AgentDockHome, "lsp-servers.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	repo := codingTestRepo(t, "native LSP task")
	if err = os.WriteFile(filepath.Join(repo, "sample.py"), []byte("def native_symbol() -> int:\n    return 42\n"), 0600); err != nil {
		t.Fatal(err)
	}
	id := codingTestStart(t, rt, "native", repo)
	result := codingTestCall(t, rt, "lsp_query", map[string]any{"task_id": id, "action": "symbols", "path": "sample.py"})
	data, _ := json.Marshal(result["data"])
	if !strings.Contains(string(data), "native_symbol") {
		t.Fatalf("real task-bound native symbols missing: %s", data)
	}
	codingTestCall(t, rt, "lsp_query", map[string]any{"task_id": id, "action": "hover", "path": "sample.py", "line": 1, "character": 5})
	codingTestCall(t, rt, "lsp_manage", map[string]any{"task_id": id, "action": "status"})
	codingTestCall(t, rt, "lsp_manage", map[string]any{"task_id": id, "action": "stop", "language": "python"})
}
