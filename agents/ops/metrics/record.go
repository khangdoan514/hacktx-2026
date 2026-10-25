package metrics

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrRunIDRequired = errors.New("metrics: run_id is required")
	ErrTraceRequired = errors.New("metrics: trace_id and span_id are required")
	ErrLogIncomplete = errors.New("metrics: level, message, and timestamp are required")
)

// LogLine is one structured operator log record.
// Attrs may contain untrusted or secret material; JSON encoding redacts it.
type LogLine struct {
	Timestamp time.Time
	Level     string
	Message   string
	Fields
	Attrs map[string]any
}

// Span is one structured trace span. RunID, TraceID, and SpanID are required
// so a span can be joined to the run that produced it.
type Span struct {
	Name string
	Fields
	Attrs map[string]any
}

// Filter selects log lines for operator search. Zero-value fields are ignored.
// Since and Until are inclusive.
type Filter struct {
	TenantID     string
	AgentID      string
	AgentVersion string
	State        string
	ErrorCode    string
	Since        time.Time
	Until        time.Time
}

func (line LogLine) validate() error {
	if strings.TrimSpace(line.RunID) == "" {
		return ErrRunIDRequired
	}
	switch line.Level {
	case LevelInfo, LevelWarn, LevelError:
	default:
		return ErrLogIncomplete
	}
	if strings.TrimSpace(line.Message) == "" || line.Timestamp.IsZero() {
		return ErrLogIncomplete
	}
	return nil
}

func (span Span) validate() error {
	if strings.TrimSpace(span.RunID) == "" {
		return ErrRunIDRequired
	}
	if strings.TrimSpace(span.TraceID) == "" || strings.TrimSpace(span.SpanID) == "" || strings.TrimSpace(span.Name) == "" {
		return ErrTraceRequired
	}
	return nil
}

type encodedLog struct {
	Timestamp    string         `json:"timestamp"`
	Level        string         `json:"level"`
	Message      string         `json:"message"`
	RunID        string         `json:"run_id"`
	TraceID      string         `json:"trace_id,omitempty"`
	SpanID       string         `json:"span_id,omitempty"`
	TenantID     string         `json:"tenant_id,omitempty"`
	PrincipalID  string         `json:"principal_id,omitempty"`
	InvocationID string         `json:"invocation_id,omitempty"`
	AgentID      string         `json:"agent_id,omitempty"`
	AgentVersion string         `json:"agent_version,omitempty"`
	State        string         `json:"state,omitempty"`
	ErrorCode    string         `json:"error_code,omitempty"`
	Attempt      int            `json:"attempt,omitempty"`
	Attrs        map[string]any `json:"attrs,omitempty"`
}

type encodedSpan struct {
	Name         string         `json:"name"`
	RunID        string         `json:"run_id"`
	TraceID      string         `json:"trace_id"`
	SpanID       string         `json:"span_id"`
	ParentSpanID string         `json:"parent_span_id,omitempty"`
	TenantID     string         `json:"tenant_id,omitempty"`
	PrincipalID  string         `json:"principal_id,omitempty"`
	InvocationID string         `json:"invocation_id,omitempty"`
	AgentID      string         `json:"agent_id,omitempty"`
	AgentVersion string         `json:"agent_version,omitempty"`
	State        string         `json:"state,omitempty"`
	ErrorCode    string         `json:"error_code,omitempty"`
	Attempt      int            `json:"attempt,omitempty"`
	Attrs        map[string]any `json:"attrs,omitempty"`
}

// MarshalJSON encodes a redacted log line. Encoding fails when run_id, level,
// message, or timestamp is missing, so a record cannot be emitted unlinkable.
func (line LogLine) MarshalJSON() ([]byte, error) {
	if err := line.validate(); err != nil {
		return nil, err
	}
	payload := encodedLog{
		Timestamp:    line.Timestamp.UTC().Format(time.RFC3339Nano),
		Level:        line.Level,
		Message:      RedactText(line.Message),
		RunID:        line.RunID,
		TraceID:      line.TraceID,
		SpanID:       line.SpanID,
		TenantID:     line.TenantID,
		PrincipalID:  line.PrincipalID,
		InvocationID: line.InvocationID,
		AgentID:      line.AgentID,
		AgentVersion: line.AgentVersion,
		State:        line.State,
		ErrorCode:    line.ErrorCode,
		Attempt:      line.Attempt,
		Attrs:        RedactMap(line.Attrs),
	}
	return json.Marshal(payload)
}

// MarshalJSON encodes a redacted span linked to a run.
func (span Span) MarshalJSON() ([]byte, error) {
	if err := span.validate(); err != nil {
		return nil, err
	}
	payload := encodedSpan{
		Name:         RedactText(span.Name),
		RunID:        span.RunID,
		TraceID:      span.TraceID,
		SpanID:       span.SpanID,
		ParentSpanID: span.ParentSpanID,
		TenantID:     span.TenantID,
		PrincipalID:  span.PrincipalID,
		InvocationID: span.InvocationID,
		AgentID:      span.AgentID,
		AgentVersion: span.AgentVersion,
		State:        span.State,
		ErrorCode:    span.ErrorCode,
		Attempt:      span.Attempt,
		Attrs:        RedactMap(span.Attrs),
	}
	return json.Marshal(payload)
}

// Match reports whether line satisfies every set filter field.
func (filter Filter) Match(line LogLine) bool {
	if filter.TenantID != "" && line.TenantID != filter.TenantID {
		return false
	}
	if filter.AgentID != "" && line.AgentID != filter.AgentID {
		return false
	}
	if filter.AgentVersion != "" && line.AgentVersion != filter.AgentVersion {
		return false
	}
	if filter.State != "" && line.State != filter.State {
		return false
	}
	if filter.ErrorCode != "" && line.ErrorCode != filter.ErrorCode {
		return false
	}
	if !filter.Since.IsZero() && line.Timestamp.Before(filter.Since) {
		return false
	}
	if !filter.Until.IsZero() && line.Timestamp.After(filter.Until) {
		return false
	}
	return true
}

// SelectLogs returns lines that match filter, preserving order.
func SelectLogs(lines []LogLine, filter Filter) []LogLine {
	var matched []LogLine
	for _, line := range lines {
		if filter.Match(line) {
			matched = append(matched, line)
		}
	}
	return matched
}
