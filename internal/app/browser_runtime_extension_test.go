// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package app

import "testing"

func TestParseBrowserStartExtensionTransport(t *testing.T) {
	req, err := parseBrowserStart(map[string]any{
		"transport": "extension",
		"tab_id":    42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Transport != "extension" || req.TabID != 42 || req.URL != "" || !req.ShowCursor {
		t.Fatalf("extension start = %#v", req)
	}
}

func TestParseBrowserStartCanDisableVisualCursor(t *testing.T) {
	req, err := parseBrowserStart(map[string]any{"show_cursor": false})
	if err != nil {
		t.Fatal(err)
	}
	if req.ShowCursor {
		t.Fatal("show_cursor=false was not preserved")
	}
}

func TestDesktopBrowserFallbackRequiresSemanticTarget(t *testing.T) {
	if _, err := parseDesktopBrowserFallback(map[string]any{
		"action": "press", "window_handle": 123, "selector": map[string]any{},
	}); err == nil {
		t.Fatal("empty UIA fallback selector unexpectedly accepted")
	}
	request, err := parseDesktopBrowserFallback(map[string]any{
		"action": "press", "window_handle": 123,
		"selector": map[string]any{"automation_id": "submit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.WindowHandle != 123 || request.Selector.AutomationID != "submit" {
		t.Fatalf("fallback request = %#v", request)
	}
}
