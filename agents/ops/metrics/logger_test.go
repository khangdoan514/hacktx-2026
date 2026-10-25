package metrics

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLoggerStartupAndRequestIncludeRunIDAndRedact(t *testing.T) {
	const startupRun = "run_startup"
	const requestRun = "run_request"
	password := sampleSecret
	token := sampleSecret
	databaseURL := "postgres://localuser:" + sampleSecret + "@127.0.0.1/nmc3"
	supabaseKey := sampleSecret

	var buf bytes.Buffer
	logger, err := Install(&buf)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)
	backend := "http://localuser:" + password + "@127.0.0.1:49374"
	err = logger.Startup(now, startupRun, "127.0.0.1:8080", backend, map[string]any{
		"password":            password,
		"token":               token,
		"database_url":        databaseURL,
		"supabase_secret_key": supabaseKey,
		"note":                "password=" + password + " token=" + token + " " + databaseURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = logger.Request("GET", "/v1/agent-runs/"+requestRun+"?token="+token, Fields{
		RunID:   requestRun,
		TraceID: "trace_request",
		SpanID:  "span_request",
	}, map[string]any{
		"password":            password,
		"token":               token,
		"database_url":        databaseURL,
		"supabase_secret_key": supabaseKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("records = %d\n%s", len(lines), buf.String())
	}
	if strings.Contains(buf.String(), sampleSecret) {
		t.Fatalf("secret leaked: %s", buf.String())
	}

	var startup map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &startup); err != nil {
		t.Fatal(err)
	}
	if startup["run_id"] != startupRun {
		t.Fatalf("startup run_id = %#v", startup["run_id"])
	}
	message, _ := startup["message"].(string)
	if !strings.Contains(message, "NMC3 API listening on 127.0.0.1:8080") || !strings.Contains(message, "http://"+Redacted+"@127.0.0.1:49374") {
		t.Fatalf("startup message = %q", message)
	}
	assertRedactedSecrets(t, startup["attrs"])

	var request map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &request); err != nil {
		t.Fatal(err)
	}
	if request["run_id"] != requestRun {
		t.Fatalf("request run_id = %#v", request["run_id"])
	}
	if request["trace_id"] != "trace_request" || request["span_id"] != "span_request" {
		t.Fatalf("request linkage = %#v", request)
	}
	name, _ := request["name"].(string)
	if !strings.Contains(name, requestRun) || !strings.Contains(name, "token="+Redacted) {
		t.Fatalf("request name = %q", name)
	}
	assertRedactedSecrets(t, request["attrs"])
}

func TestLoggerWritesNothingWithoutRunID(t *testing.T) {
	var buf bytes.Buffer
	logger, err := Install(&buf)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)
	err = logger.Startup(now, "", "127.0.0.1:8080", "http://127.0.0.1:49374", map[string]any{
		"password":            sampleSecret,
		"token":               sampleSecret,
		"database_url":        "postgres://localuser:" + sampleSecret + "@127.0.0.1/nmc3",
		"supabase_secret_key": sampleSecret,
	})
	if !errors.Is(err, ErrRunIDRequired) {
		t.Fatalf("startup err = %v", err)
	}
	err = logger.Request("GET", "/v1/agents", Fields{TraceID: "trace_request", SpanID: "span_request"}, map[string]any{
		"supabase_secret_key": sampleSecret,
	})
	if !errors.Is(err, ErrRunIDRequired) {
		t.Fatalf("request err = %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("wrote without run_id: %s", buf.String())
	}
	if _, err := Install(nil); !errors.Is(err, ErrWriterRequired) {
		t.Fatalf("nil writer = %v", err)
	}
}

func assertRedactedSecrets(t *testing.T, attrs any) {
	t.Helper()
	bag, ok := attrs.(map[string]any)
	if !ok {
		t.Fatalf("attrs = %#v", attrs)
	}
	for _, key := range []string{"password", "token", "database_url", "supabase_secret_key"} {
		if bag[key] != Redacted {
			t.Fatalf("%s = %#v", key, bag[key])
		}
	}
}
