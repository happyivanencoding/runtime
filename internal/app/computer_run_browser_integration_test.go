//go:build browser_integration

package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	toolbrowser "github.com/uvwt/agentdock/internal/tool/browser"
)

func TestBrowserIntegrationComputerRunUsesNativeCDP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Computer Run</title>
        <h1>Native CDP multi-step test</h1>
        <p id="status">Ready</p>
        <button id="open" onclick="document.getElementById('status').textContent='Opened'">Open details</button>
        <button id="save" onclick="document.getElementById('status').textContent='Saved'">Save changes</button>
        <button id="delete" onclick="document.getElementById('status').textContent='Deleted'">Delete all</button>`)
	}))
	defer server.Close()
	rt, _ := appBrowserIntegrationRuntime(t)
	defer rt.Close()
	sessionID := startRuntimeBrowser(t, rt, server.URL)
	calls := 0
	result, err := runComputerSteps(context.Background(), "browser", map[string]any{
		"session_id": sessionID, "max_steps": 6, "max_duration_ms": 20000,
	}, func(ctx context.Context, args map[string]any) (Result, error) {
		calls++
		snapshot, err := rt.browser.Snapshot(ctx, toolbrowser.SnapshotRequest{
			SessionID: sessionID, Timeout: 5 * time.Second,
		})
		if err != nil {
			return nil, err
		}
		if strings.Contains(snapshot.Text, "Saved") {
			r, err := rt.publishBrowserSnapshot(ctx, snapshot, 0)
			if err != nil {
				return nil, err
			}
			r["execution_gate"] = "done"
			r["executed"] = false
			r["startlux_choice"] = "done"
			r["startlux_model"] = "test-native-policy"
			return r, nil
		}
		selector, id := "#open", "click_001"
		if strings.Contains(snapshot.Text, "Opened") {
			selector, id = "#save", "click_002"
		}
		acted, err := rt.browser.Act(ctx, toolbrowser.ActRequest{
			SessionID: sessionID,
			Actions:   []toolbrowser.Action{{Kind: "click", Click: &toolbrowser.ClickAction{Selector: selector}}},
			Timeout:   5 * time.Second,
		})
		if err != nil {
			return nil, err
		}
		r, err := rt.publishBrowserSnapshot(ctx, acted, 0)
		if err != nil {
			return nil, err
		}
		r["execution_gate"] = "execute"
		r["executed"] = true
		r["startlux_choice"] = id
		r["startlux_model"] = "test-native-policy"
		r["selected_action"] = map[string]any{"action": "click", "selector": selector}
		return r, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "completed" || result["steps_executed"] != 2 || calls != 3 {
		t.Fatalf("native multi-step run stopped unexpectedly: %#v", result)
	}
	last, _ := result["last_result"].(Result)
	if !strings.Contains(fmt.Sprint(last["text"]), "Saved") ||
		strings.Contains(fmt.Sprint(last["text"]), "Deleted") {
		t.Fatalf("final native page state wrong: %#v", last["text"])
	}
	assertToolResultMatchesOutputSchema(t, "browser_run", result)
}

func TestBrowserIntegrationSelectOptionReportsCurrentSelection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<title>Select status</title><h1>Select status</h1>
        <select id="team"><option value="Risk" selected>Risk</option><option value="Equity">Equity</option></select>`)
	}))
	defer server.Close()
	rt, _ := appBrowserIntegrationRuntime(t)
	defer rt.Close()
	sessionID := startRuntimeBrowser(t, rt, server.URL)
	read := func() []toolbrowser.SelectOption {
		t.Helper()
		snapshot, err := rt.browser.Snapshot(context.Background(), toolbrowser.SnapshotRequest{
			SessionID: sessionID, Timeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, el := range snapshot.InteractiveElements {
			if el.Selector == "#team" {
				return el.Options
			}
		}
		t.Fatal("select element not found")
		return nil
	}
	opts := read()
	if len(opts) != 2 || !opts[0].Selected || opts[1].Selected {
		t.Fatalf("initial select state missing: %+v", opts)
	}
	_, err := rt.browser.Act(context.Background(), toolbrowser.ActRequest{
		SessionID: sessionID,
		Actions:   []toolbrowser.Action{{Kind: "select", Select: &toolbrowser.SelectAction{Selector: "#team", Value: "Equity"}}},
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	opts = read()
	if opts[0].Selected || !opts[1].Selected {
		t.Fatalf("updated select state missing: %+v", opts)
	}
}
