//go:build windows && amd64

// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"bytes"
	"context"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/uvwt/agentdock/internal/process"
)

func ownedFixture(t *testing.T) (uint64, uint32, string) {
	t.Helper()
	if os.Getenv("RUNTIME_DESKTOP_TEST") != "1" {
		t.Skip("set RUNTIME_DESKTOP_TEST=1 to open a Runtime-owned accessibility fixture")
	}
	state := t.TempDir()
	script, err := filepath.Abs("testdata/fixture.ps1")
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(state, "fixture.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-File", script, "-StateDir", state)
	cmd.Stdout, cmd.Stderr = log, log
	process.Configure(cmd)
	if err = cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	control, err := process.Attach(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		log.Close()
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = control.Terminate(); <-done; _ = control.Close(); _ = log.Close() })
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(filepath.Join(state, "window.txt")); err == nil {
			handle, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
			if err == nil && handle != 0 {
				return handle, uint32(cmd.Process.Pid), state
			}
		}
		select {
		case <-done:
			data, _ := os.ReadFile(filepath.Join(state, "fixture.log"))
			t.Fatalf("fixture exited: %s", data)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	data, _ := os.ReadFile(filepath.Join(state, "fixture.log"))
	t.Fatalf("fixture did not become ready: %s", data)
	return 0, 0, ""
}

func TestDesktopNativeSemanticLifecycle(t *testing.T) {
	handle, pid, state := ownedFixture(t)
	service := New()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// Native handle is written during SourceInitialized, before WPF has laid out
	// its children. Poll the semantic tree, rather than relying on a timing sleep.
	var input Node
	for deadline := time.Now().Add(12 * time.Second); time.Now().Before(deadline); {
		result, err := service.Inspect(ctx, Request{Action: "find", WindowHandle: handle, Selector: Selector{AutomationID: "runtime-input"}})
		if err == nil {
			nodes := result["elements"].([]Node)
			if len(nodes) == 1 {
				input = nodes[0]
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if input.RuntimeID == "" {
		t.Fatal("input not found through native accessibility")
	}
	windows, err := service.Inspect(ctx, Request{Action: "windows", ProcessID: pid})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, window := range windows["windows"].([]Window) {
		if window.Handle == handle {
			found = true
		}
	}
	if !found {
		t.Fatal("native window enumeration did not find owned fixture")
	}
	if _, err = service.Inspect(ctx, Request{Action: "applications", ProcessID: pid}); err != nil {
		t.Fatal(err)
	}
	typed, err := service.Act(ctx, Request{Action: "type", WindowHandle: handle, Selector: Selector{RuntimeID: input.RuntimeID}, Text: "中文 runtime value"})
	if err != nil {
		t.Fatal(err)
	}
	after := typed["after"].(Node)
	if after.Value == nil || *after.Value != "中文 runtime value" {
		t.Fatalf("ValuePattern did not set Unicode text: %+v", after)
	}
	if _, err = service.Act(ctx, Request{Action: "focus", WindowHandle: handle, Selector: Selector{AutomationID: "runtime-input"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Act(ctx, Request{Action: "press", WindowHandle: handle, Selector: Selector{AutomationID: "runtime-apply"}}); err != nil {
		t.Fatal(err)
	}
	applied := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		if data, err := os.ReadFile(filepath.Join(state, "applied.txt")); err == nil && string(data) == "中文 runtime value" {
			applied = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !applied {
		t.Fatal("InvokePattern returned without the expected application-side effect")
	}
	toggled, err := service.Act(ctx, Request{Action: "toggle", WindowHandle: handle, Selector: Selector{AutomationID: "runtime-toggle"}})
	if err != nil {
		t.Fatal(err)
	}
	node := toggled["after"].(Node)
	if node.ToggleState == nil || *node.ToggleState != 1 {
		t.Fatal("TogglePattern did not change state")
	}
	selected, err := service.Act(ctx, Request{Action: "select", WindowHandle: handle, Selector: Selector{Name: "Fixture item 2", ControlType: "listitem"}})
	if err != nil {
		t.Fatal(err)
	}
	node = selected["after"].(Node)
	if node.Selected == nil || !*node.Selected {
		t.Fatal("SelectionItemPattern did not select the item")
	}
	if _, err = service.Act(ctx, Request{Action: "scroll", WindowHandle: handle, Selector: Selector{AutomationID: "runtime-list"}, Direction: "down", Amount: "large"}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Act(ctx, Request{Action: "press", WindowHandle: handle, Selector: Selector{Name: "Duplicate"}}); err == nil {
		t.Fatal("ambiguous controls caused an action")
	}
	if _, err = service.Act(ctx, Request{Action: "press", WindowHandle: handle, Selector: Selector{AutomationID: "nonexistent"}}); err == nil {
		t.Fatal("missing control caused an action")
	}
	tree, err := service.Inspect(ctx, Request{Action: "tree", WindowHandle: handle, MaxNodes: 2})
	if err != nil || tree["truncated"] != true {
		t.Fatalf("bounded tree did not report truncation: %+v %v", tree, err)
	}
	pngData, bounds, err := service.Screen(ctx, handle)
	if err != nil {
		t.Fatal(err)
	}
	picture, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		t.Fatal(err)
	}
	if picture.Bounds().Dx() != int(bounds.Right-bounds.Left) || picture.Bounds().Dy() != int(bounds.Bottom-bounds.Top) {
		t.Fatal("screenshot dimensions do not match window bounds")
	}
	t.Log("native HWND discovery, UIA tree/find, Unicode Value, Focus, Invoke with app-side evidence, Toggle, Select, Scroll, ambiguity rejection and PNG capture passed")
}

func TestDesktopRejectsUnscopedActions(t *testing.T) {
	service := New()
	if _, err := service.Act(context.Background(), Request{Action: "press"}); err == nil {
		t.Fatal("accepted unscoped action")
	}
	if _, err := service.Inspect(context.Background(), Request{Action: "find", WindowHandle: 1}); err == nil {
		t.Fatal("accepted empty find selector")
	}
	if _, _, err := service.Screen(context.Background(), 0); err == nil {
		t.Fatal("accepted unscoped screenshot")
	}
}
