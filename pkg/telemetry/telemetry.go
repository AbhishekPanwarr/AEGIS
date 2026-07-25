package telemetry

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// Logger is a structured JSON logger that writes one line per event to stdout.
// Section 4.6 of the implementation plan.
// Minimum fields on every line: ts, level, service, event.
// Decision-path log lines additionally carry decision_id once one exists.
type Logger struct {
	service string
	mu      sync.Mutex
	enc     *json.Encoder
}

// New creates a new Logger for the given service name.
func New(service string) *Logger {
	enc := json.NewEncoder(os.Stdout)
	return &Logger{service: service, enc: enc}
}

// logEntry is the on-wire JSON structure.
type logEntry struct {
	TS         string                 `json:"ts"`
	Level      string                 `json:"level"`
	Service    string                 `json:"service"`
	Event      string                 `json:"event"`
	DecisionID string                 `json:"decision_id,omitempty"`
	Fields     map[string]interface{} `json:"-"`
}

// Log emits a structured log line at the given level.
// fields is a flat map of additional key-value pairs merged into the JSON output.
func (l *Logger) Log(level, event string, fields ...map[string]interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	m := map[string]interface{}{
		"ts":      time.Now().UTC().Format(time.RFC3339Nano),
		"level":   level,
		"service": l.service,
		"event":   event,
	}
	for _, f := range fields {
		for k, v := range f {
			m[k] = v
		}
	}
	_ = l.enc.Encode(m)
}

// Info is a convenience wrapper for Log at the "info" level.
func (l *Logger) Info(event string, fields ...map[string]interface{}) {
	l.Log("info", event, fields...)
}

// Error is a convenience wrapper for Log at the "error" level.
func (l *Logger) Error(event string, fields ...map[string]interface{}) {
	l.Log("error", event, fields...)
}

// Warn is a convenience wrapper for Log at the "warn" level.
func (l *Logger) Warn(event string, fields ...map[string]interface{}) {
	l.Log("warn", event, fields...)
}
