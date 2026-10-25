package metrics

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLogAndSpanRequireRunLinkage(t *testing.T) {
	now := time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)
	_, err := json.Marshal(LogLine{Level: LevelError, Message: "failed", Timestamp: now})
	if !errors.Is(err, ErrRunIDRequired) {
		t.Fatalf("log err = %v", err)
	}
	_, err = json.Marshal(LogLine{Level: "debug", Message: "failed", Timestamp: now, Fields: Fields{RunID: "run_123"}})
	if !errors.Is(err, ErrLogIncomplete) {
		t.Fatalf("level err = %v", err)
	}
	_, err = json.Marshal(Span{Name: "agent.run", Fields: Fields{TraceID: "trace_123", SpanID: "span_123"}})
	if !errors.Is(err, ErrRunIDRequired) {
		t.Fatalf("span run err = %v", err)
	}
	_, err = json.Marshal(Span{Name: "agent.run", Fields: Fields{RunID: "run_123", SpanID: "span_123"}})
	if !errors.Is(err, ErrTraceRequired) {
		t.Fatalf("span trace err = %v", err)
	}
}

func TestLogAndSpanOmitSecrets(t *testing.T) {
	now := time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)
	line := LogLine{
		Timestamp: now,
		Level:     LevelError,
		Message:   "run failed: Bearer " + sampleSecret,
		Fields: Fields{
			RunID:        "run_123",
			TraceID:      "trace_123",
			SpanID:       "span_123",
			TenantID:     "org_123",
			PrincipalID:  "user_123",
			InvocationID: "inv_123",
			AgentID:      "orchestrator",
			AgentVersion: "1.0.0",
			State:        "FAILED",
			ErrorCode:    "TIMEOUT",
			Attempt:      1,
		},
		Attrs: map[string]any{
			"api_key": sampleSecret,
			"prompt":  "reveal " + sampleSecret,
			"detail":  "postgres://localuser:" + sampleSecret + "@127.0.0.1/nmc3",
		},
	}
	encodedLine := mustJSON(t, line)
	assertNoSecret(t, encodedLine, "run_123")
	var decodedLine map[string]any
	if err := json.Unmarshal(encodedLine, &decodedLine); err != nil {
		t.Fatal(err)
	}
	if decodedLine["message"] != "run failed: Bearer "+Redacted {
		t.Fatalf("message = %#v", decodedLine["message"])
	}
	if decodedLine["timestamp"] != "2026-10-24T18:00:00Z" {
		t.Fatalf("timestamp = %#v", decodedLine["timestamp"])
	}
	attrs, _ := decodedLine["attrs"].(map[string]any)
	if attrs["api_key"] != Redacted {
		t.Fatalf("api_key = %#v", attrs["api_key"])
	}
	if attrs["detail"] != "postgres://"+Redacted+"@127.0.0.1/nmc3" {
		t.Fatalf("detail = %#v", attrs["detail"])
	}
	if _, ok := attrs["prompt"]; ok {
		t.Fatal("prompt was kept on the log line")
	}

	span := Span{
		Name: "tool Bearer " + sampleSecret,
		Fields: Fields{
			RunID:   "run_123",
			TraceID: "trace_123",
			SpanID:  "span_123",
		},
		Attrs: map[string]any{"service_password": sampleSecret, "instructions": sampleSecret},
	}
	encoded := mustJSON(t, span)
	assertNoSecret(t, encoded, "run_123")
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["trace_id"] != "trace_123" || decoded["span_id"] != "span_123" {
		t.Fatalf("span linkage = %#v", decoded)
	}
	if _, ok := decoded["attrs"].(map[string]any)["instructions"]; ok {
		t.Fatal("instructions were kept")
	}
}

func TestSelectLogsFindsFailedRun(t *testing.T) {
	now := time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)
	lines := []LogLine{
		{Timestamp: now.Add(-time.Hour), Level: LevelInfo, Message: "started", Fields: Fields{RunID: "run_ok", TenantID: "org_123", State: "SUCCEEDED", AgentID: "orchestrator", AgentVersion: "1.0.0"}},
		{Timestamp: now, Level: LevelError, Message: "budget stop", Fields: Fields{RunID: "run_fail", TenantID: "org_123", State: "FAILED", ErrorCode: "BUDGET_EXCEEDED", AgentID: "orchestrator", AgentVersion: "1.0.0"}, Attrs: map[string]any{"token": sampleSecret}},
		{Timestamp: now, Level: LevelError, Message: "other tenant", Fields: Fields{RunID: "run_other", TenantID: "org_999", State: "FAILED", ErrorCode: "BUDGET_EXCEEDED"}},
	}
	matched := SelectLogs(lines, Filter{
		TenantID:  "org_123",
		AgentID:   "orchestrator",
		State:     "FAILED",
		ErrorCode: "BUDGET_EXCEEDED",
		Since:     now.Add(-time.Minute),
		Until:     now,
	})
	if len(matched) != 1 || matched[0].RunID != "run_fail" {
		t.Fatalf("matched = %#v", matched)
	}
	assertNoSecret(t, mustJSON(t, matched[0]), "run_fail")
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func assertNoSecret(t *testing.T, encoded []byte, runID string) {
	t.Helper()
	text := string(encoded)
	if strings.Contains(text, sampleSecret) {
		t.Fatalf("secret leaked: %s", text)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["run_id"] != runID {
		t.Fatalf("run_id = %#v in %s", decoded["run_id"], text)
	}
}
