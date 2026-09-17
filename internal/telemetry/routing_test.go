package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRoutingWriterUsesInvocationLabelsWithoutPayloadData(t *testing.T) {
	t.Setenv("RUNTIME_INVOCATION_SOURCE", "agentdock_fallback")
	t.Setenv("RUNTIME_INVOCATION_VIA", "runtime-core-preview")
	home := t.TempDir()
	writer := NewRoutingWriter(home, true)
	ok := true
	if err := writer.Write(RoutingEvent{Event: "tool_finished", Tool: "exec_command", RequestID: "req-test", DurationMS: 12, OK: &ok}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "telemetry", "runtime-routing.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var event map[string]any
	if err := json.Unmarshal(data[:len(data)-1], &event); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if event["backend"] != "runtime" || event["source"] != "agentdock_fallback" || event["via"] != "runtime-core-preview" || event["transport"] != "stdio" {
		t.Fatalf("unexpected routing labels: %#v", event)
	}
	for _, forbidden := range []string{"arguments", "stdout", "stderr", "token", "secret"} {
		if _, exists := event[forbidden]; exists {
			t.Fatalf("routing event contains forbidden field %q: %#v", forbidden, event)
		}
	}
}
