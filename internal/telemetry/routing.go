package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const routingSchemaVersion = 1

type RoutingEvent struct {
	SchemaVersion int       `json:"schema_version"`
	Time          time.Time `json:"time"`
	Event         string    `json:"event"`
	Backend       string    `json:"backend"`
	Source        string    `json:"source"`
	Via           string    `json:"via,omitempty"`
	Transport     string    `json:"transport"`
	Tool          string    `json:"tool"`
	RequestID     string    `json:"request_id,omitempty"`
	DurationMS    int64     `json:"duration_ms,omitempty"`
	OK            *bool     `json:"ok,omitempty"`
	ErrorCode     string    `json:"error_code,omitempty"`
	ErrorCategory string    `json:"error_category,omitempty"`
	Retryable     *bool     `json:"retryable,omitempty"`
	TargetServer  string    `json:"target_server,omitempty"`
	TargetTool    string    `json:"target_tool,omitempty"`
	Probe         bool      `json:"probe,omitempty"`
	PID           int       `json:"pid"`
}

type RoutingWriter struct {
	path      string
	backend   string
	source    string
	via       string
	transport string
	mu        sync.Mutex
}

func NewRoutingWriter(home string, stdio bool) *RoutingWriter {
	transport := "http"
	defaultSource := "runtime_http"
	if stdio {
		transport = "stdio"
		defaultSource = "runtime_stdio"
	}
	source := boundedLabel(os.Getenv("RUNTIME_INVOCATION_SOURCE"), defaultSource)
	via := boundedLabel(os.Getenv("RUNTIME_INVOCATION_VIA"), "")
	return &RoutingWriter{
		path:      filepath.Join(home, "telemetry", "runtime-routing.jsonl"),
		backend:   "runtime",
		source:    source,
		via:       via,
		transport: transport,
	}
}

func (w *RoutingWriter) Write(event RoutingEvent) error {
	if w == nil || strings.TrimSpace(w.path) == "" {
		return nil
	}
	if event.SchemaVersion == 0 {
		event.SchemaVersion = routingSchemaVersion
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	if event.Backend == "" {
		event.Backend = w.backend
	}
	if event.Source == "" {
		event.Source = w.source
	}
	if event.Via == "" {
		event.Via = w.via
	}
	if event.Transport == "" {
		event.Transport = w.transport
	}
	if event.PID == 0 {
		event.PID = os.Getpid()
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(w.path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	encoded = append(encoded, '\n')
	_, err = file.Write(encoded)
	return err
}

func (w *RoutingWriter) Path() string {
	if w == nil {
		return ""
	}
	return w.path
}

func boundedLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if len(value) > 160 {
		value = value[:160]
	}
	return value
}
