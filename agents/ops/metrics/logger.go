package metrics

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// ErrWriterRequired is returned when Install is called without a writer,
// or when a log or span is written on a nil logger.
var ErrWriterRequired = errors.New("metrics: writer is required")

// Logger writes redacted LogLine and Span records to an io.Writer.
// Each successful write is one JSON object and a trailing newline.
// cmd/nmc3-agent installs it with a single Install call. That call is not
// in main.go. The standard-library lines are documented in agents/ops/README.md.
type Logger struct {
	mu sync.Mutex
	w  io.Writer
}

// Install returns a logger that writes operator records to w.
// Call it once from cmd/nmc3-agent, passing the writer that should receive
// records, typically os.Stderr. A nil writer is rejected.
func Install(w io.Writer) (*Logger, error) {
	if w == nil {
		return nil, ErrWriterRequired
	}
	return &Logger{w: w}, nil
}

// Startup writes the listening line from cmd/nmc3-agent.
// runID is copied onto the LogLine when the caller supplies it. Encoding
// fails with ErrRunIDRequired when runID is empty, and nothing is written.
// addr and backend are scrubbed as free text. attrs are redacted.
func (l *Logger) Startup(now time.Time, runID, addr, backend string, attrs map[string]any) error {
	line := LogLine{
		Timestamp: now,
		Level:     LevelInfo,
		Message:   fmt.Sprintf("NMC3 API listening on %s (OpenCode backend %s)", addr, backend),
		Fields:    Fields{RunID: runID},
		Attrs:     attrs,
	}
	return l.WriteLog(line)
}

// Request writes one HTTP request span.
// fields.RunID is included when the caller sets it. Encoding fails when
// run_id, trace_id, or span_id is missing, and nothing is written.
// method and path are scrubbed as the span name. attrs are redacted.
func (l *Logger) Request(method, path string, fields Fields, attrs map[string]any) error {
	span := Span{
		Name:   method + " " + path,
		Fields: fields,
		Attrs:  attrs,
	}
	return l.WriteSpan(span)
}

// WriteLog encodes line and writes it. The writer receives redacted JSON only.
func (l *Logger) WriteLog(line LogLine) error {
	return l.write(line)
}

// WriteSpan encodes span and writes it. The writer receives redacted JSON only.
func (l *Logger) WriteSpan(span Span) error {
	return l.write(span)
}

func (l *Logger) write(record any) error {
	if l == nil || l.w == nil {
		return ErrWriterRequired
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.w.Write(encoded)
	return err
}
