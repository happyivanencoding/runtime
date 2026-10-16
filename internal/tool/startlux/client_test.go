package startlux

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func samplePolicyRequest() PolicyRequest {
	return PolicyRequest{
		State:                    map[string]any{"goal": "Open Settings", "actions": []string{"click_settings", "done"}},
		ActionCriteria:           map[string]string{"click_settings": "Click Settings", "done": "Already completed"},
		ActionInstructions:       "Choose the next action",
		DoneInstructions:         "Has the task finished?",
		RiskInstructions:         "Rate risk",
		ConfirmationInstructions: "Is confirmation needed?",
		ContextStateInstructions: "Describe state",
		ContextStateCriteria:     map[string]string{"ready": "Ready", "completed": "Completed"},
	}
}

func TestPolicyStartLuxRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/decisions" {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization header: %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != defaultModel || body["think_mode"] != "auto" {
			t.Errorf("model/think_mode: %#v", body)
		}
		if _, ok := body["questions"].(map[string]any)["next_action"]; !ok {
			t.Error("missing next_action question")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"startlux-decision-27b-autothink","answers":{"next_action":{"type":"choice","choice":"click_settings","confidence":0.91,"none_probability":0.013,"probabilities":{"click_settings":0.91}},"task_done":{"type":"noul","noul":0.01},"action_risk":{"type":"score","score":0.24,"confidence":0.88},"need_confirmation":{"type":"noul","noul":0.02},"context_state":{"type":"choice","choice":"ready","confidence":0.89}}}`))
	}))
	defer server.Close()
	c := New("test-key")
	c.endpoint = server.URL + "/v1/decisions"
	result, err := c.Policy(context.Background(), samplePolicyRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != defaultModel || result.NextAction.Choice != "click_settings" || result.NextAction.NoneProbability == nil || *result.NextAction.NoneProbability != 0.013 {
		t.Errorf("unexpected response: %#v", result)
	}
}

func TestPolicyAcceptsNullChoice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"startlux-decision-27b-autothink","answers":{"next_action":{"type":"choice","choice":null,"confidence":0.8,"none_probability":0.82},"task_done":{"type":"noul","noul":0.01},"action_risk":{"type":"score","score":0.1},"need_confirmation":{"type":"noul","noul":0.01},"context_state":{"type":"choice","choice":"ready","confidence":0.9}}}`))
	}))
	defer server.Close()
	c := New("test-key")
	c.endpoint = server.URL
	result, err := c.Policy(context.Background(), samplePolicyRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.NextAction.Choice != "" || result.NextAction.NoneProbability == nil || *result.NextAction.NoneProbability != 0.82 {
		t.Errorf("null response: %#v", result.NextAction)
	}
}
