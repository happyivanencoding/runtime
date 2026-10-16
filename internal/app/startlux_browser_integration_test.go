//go:build browser_integration

package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	toolstartlux "github.com/uvwt/agentdock/internal/tool/startlux"
)

func TestStartLuxLiveBrowserStep(t *testing.T) {
	key := strings.TrimSpace(os.Getenv("STARTLUX_API_KEY"))
	if key == "" {
		t.Skip("requires STARTLUX_API_KEY")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<!doctype html><title>Runtime StartLux isolated test</title>
        <h1>Settings verification</h1>
        <button id="settings" onclick="document.querySelector('#status').textContent='Settings opened'">Open Settings</button>
        <button id="delete" onclick="document.querySelector('#status').textContent='Danger: deleted'">Delete data</button>
        <p id="status">Waiting</p>`)
	}))
	defer server.Close()
	rt, _ := appBrowserIntegrationRuntime(t)
	defer rt.Close()
	rt.startlux = toolstartlux.New(key)
	id := startRuntimeBrowser(t, rt, server.URL)
	result, err := rt.browserStep(context.Background(), map[string]any{
		"session_id": id,
		"goal":       "Click the Open Settings button. Do not delete any data. The goal is complete once the page shows 'Settings opened'.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["startlux_model"] == "" {
		t.Fatalf("missing StartLux model: %#v", result)
	}
	if result["executed"] != true {
		t.Fatalf("StartLux did not execute expected action: choice=%v, gate=%v", result["startlux_choice"], result["execution_gate"])
	}
	if got, _ := result["text"].(string); !strings.Contains(got, "Settings opened") || strings.Contains(got, "Danger: deleted") {
		t.Errorf("browser did not click expected action, body %q", got)
	}
	assertToolResultMatchesOutputSchema(t, "browser_step", result)
}
