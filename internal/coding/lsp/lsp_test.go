// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLSPFramingAndUTF16(t *testing.T) {
	var wire bytes.Buffer
	value := map[string]any{"jsonrpc": "2.0", "id": 1, "result": "中文😀"}
	if err := writeMessage(&wire, value); err != nil {
		t.Fatal(err)
	}
	msg, err := readMessage(bufio.NewReader(&wire))
	if err != nil {
		t.Fatal(err)
	}
	var result string
	if err = json.Unmarshal(msg.Result, &result); err != nil || result != "中文😀" {
		t.Fatalf("framing lost Unicode: %q %v", result, err)
	}
	if err := validatePosition("x😀z\r\nnext", 1, 3); err != nil {
		t.Fatal(err)
	}
	if err := validatePosition("x😀z", 1, 2); err == nil {
		t.Fatal("accepted a split UTF-16 surrogate")
	}
	if err := validatePosition("line", 0, 0); err == nil {
		t.Fatal("accepted zero-based input line")
	}
	if _, err := readMessage(bufio.NewReader(strings.NewReader("Content-Length: -1\r\n\r\n"))); err == nil {
		t.Fatal("accepted negative frame length")
	}
}

func TestLSPWorkspaceAndURI(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "中文 space#%.go")
	if err := os.WriteFile(filename, []byte("package sample\n"), 0600); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveFile(root, filepath.Base(filename))
	if err != nil {
		t.Fatal(err)
	}
	uri := fileURI(resolved)
	if !strings.Contains(uri, "%23") || !strings.Contains(uri, "%25") {
		t.Fatalf("URI was not escaped: %s", uri)
	}
	other := filepath.Join(t.TempDir(), "other.go")
	_ = os.WriteFile(other, []byte("package other"), 0600)
	if _, err = resolveFile(root, other); err == nil {
		t.Fatal("task LSP accepted another workspace file")
	}
}

func nativeHome(t *testing.T) string {
	t.Helper()
	home := os.Getenv("RUNTIME_LSP_TEST_HOME")
	if home == "" {
		t.Skip("set RUNTIME_LSP_TEST_HOME to explicitly run installed native language servers")
	}
	return home
}
func fixtureFile(t *testing.T, root, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func resultContains(t *testing.T, result map[string]any, token string) {
	t.Helper()
	data, err := json.Marshal(result["data"])
	if err != nil || !strings.Contains(strings.ToLower(string(data)), strings.ToLower(token)) {
		t.Fatalf("result missing %q: %s (%v)", token, data, err)
	}
}
func diagnosticsReceived(t *testing.T, m *Manager, ctx context.Context, root, file, language string) map[string]any {
	t.Helper()
	for i := 0; i < 3; i++ {
		result, err := m.Query(ctx, root, Query{Action: "diagnostics", Language: language, Path: file})
		if err != nil {
			t.Fatal(err)
		}
		if result["status"] == "received" {
			return result
		}
	}
	t.Fatal("no diagnostics publication received; not treating pending as clean")
	return nil
}

func TestLSPNativeLanguageServers(t *testing.T) {
	home := nativeHome(t)
	fixtures := []struct {
		language, file, source, fixed, symbol, run string
		line, runLine                              int
	}{
		{"go", "sample.go", "package sample\nfunc Double(x int) int {\n return x * 2\n}\nfunc Run() int {\n return Double(21)\n}\nvar wrong int = \"bad\"\n", "package sample\nfunc Double(x int) int {\n return x * 2\n}\nfunc Run() int {\n return Double(21)\n}\nvar wrong int = 1\n", "Double", "Run", 6, 5},
		{"typescript", "sample.ts", "export function double(x: number): number {\n return x * 2;\n}\nexport function run(): number {\n return double(21);\n}\nconst wrong: number = 'bad';\n", "export function double(x: number): number {\n return x * 2;\n}\nexport function run(): number {\n return double(21);\n}\nconst wrong: number = 1;\n", "double", "run", 5, 4},
		{"python", "sample.py", "def double(x: int) -> int:\n    return x * 2\n\ndef run() -> int:\n    return double(21)\n\nwrong: int = 'bad'\n", "def double(x: int) -> int:\n    return x * 2\n\ndef run() -> int:\n    return double(21)\n\nwrong: int = 1\n", "double", "run", 5, 4},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.language, func(t *testing.T) {
			root := t.TempDir()
			fixtureFile(t, root, fixture.file, fixture.source)
			switch fixture.language {
			case "go":
				fixtureFile(t, root, "go.mod", "module runtimefixture\n\ngo 1.26.0\n")
			case "typescript":
				fixtureFile(t, root, "tsconfig.json", `{"compilerOptions":{"strict":true,"noEmit":true,"skipLibCheck":true},"include":["*.ts"]}`)
			case "python":
				fixtureFile(t, root, "pyrightconfig.json", `{"typeCheckingMode":"basic","include":["*.py"]}`)
			}
			m := New(home)
			defer m.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			started, err := m.Manage(ctx, root, fixture.language, "start")
			if err != nil {
				t.Fatal(err)
			}
			pid := started["pid"]
			lines := strings.Split(fixture.source, "\n")
			character := strings.Index(lines[fixture.line-1], fixture.symbol)
			for _, action := range []string{"definition", "references", "hover", "symbols", "workspace_symbols", "call_hierarchy"} {
				query := Query{Action: action, Language: fixture.language, Path: fixture.file, Line: fixture.line, Character: character, Query: fixture.symbol}
				if action == "call_hierarchy" {
					query.Line = fixture.runLine
					query.Character = strings.Index(lines[fixture.runLine-1], fixture.run)
					query.Direction = "outgoing"
				}
				result, err := m.Query(ctx, root, query)
				if err != nil {
					t.Fatalf("%s: %v", action, err)
				}
				if action == "hover" || action == "symbols" || action == "workspace_symbols" || action == "call_hierarchy" {
					resultContains(t, result, fixture.symbol)
				} else {
					resultContains(t, result, fixture.file)
				}
				t.Logf("%s: native response received", action)
			}
			diagnostic := diagnosticsReceived(t, m, ctx, root, fixture.file, fixture.language)
			var items []map[string]any
			data, _ := json.Marshal(diagnostic["data"])
			if err = json.Unmarshal(data, &items); err != nil || len(items) == 0 {
				t.Fatalf("known type error not reported: %s %v", data, err)
			}
			fixtureFile(t, root, fixture.file, fixture.fixed)
			clean := diagnosticsReceived(t, m, ctx, root, fixture.file, fixture.language)
			data, _ = json.Marshal(clean["data"])
			if err = json.Unmarshal(data, &items); err != nil {
				t.Fatal(err)
			}
			for _, item := range items {
				if item["severity"] == float64(1) {
					t.Fatalf("stale diagnostic after file repair: %s", data)
				}
			}
			same, err := m.Manage(ctx, root, fixture.language, "start")
			if err != nil || same["pid"] != pid {
				t.Fatal("query did not reuse the same language server")
			}
			stopped, err := m.Manage(ctx, root, fixture.language, "stop")
			if err != nil || stopped["stopped"] != true {
				t.Fatalf("stop failed: %+v %v", stopped, err)
			}
			t.Log("diagnostics changed after disk edit; process reused and explicitly stopped")
		})
	}
}

func TestLSPNativeCrashRestartsOnlyServer(t *testing.T) {
	home := nativeHome(t)
	root := t.TempDir()
	fixtureFile(t, root, "sample.py", "def sample() -> int:\n    return 1\n")
	m := New(home)
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	old, err := m.get(ctx, root, "python")
	if err != nil {
		t.Fatal(err)
	}
	if err = old.control.Terminate(); err != nil {
		t.Fatal(err)
	}
	<-old.done
	result, err := m.Query(ctx, root, Query{Action: "symbols", Path: "sample.py"})
	if err != nil {
		t.Fatal(err)
	}
	resultContains(t, result, "sample")
	current, err := m.get(ctx, root, "python")
	if err != nil {
		t.Fatal(err)
	}
	if old.cmd.Process.Pid == current.cmd.Process.Pid {
		t.Fatal("dead language server was reused")
	}
}
