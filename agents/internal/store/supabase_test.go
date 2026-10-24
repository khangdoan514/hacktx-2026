package store

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

func TestSupabaseRPCStore(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") != "secret" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing service role headers")
		}
		if r.Method != "POST" {
			t.Error("RPC must use POST")
		}
		name := strings.TrimPrefix(r.URL.Path, "/rest/v1/rpc/")
		calls = append(calls, name)
		w.Header().Set("Content-Type", "application/json")
		switch name {
		case "nmc3_create_run", "nmc3_update_run":
			w.WriteHeader(http.StatusNoContent)
		case "nmc3_get_run":
			_, _ = w.Write([]byte(`{"id":"run-1","state":"SUCCEEDED","agent_id":"root","agent_version":"1.0.0","input":{},"usage":{"input_tokens":1,"output_tokens":2,"cost_usd":0.01},"invocations":[],"tool_invocations":[],"created_at":"2026-10-24T00:00:00Z","updated_at":"2026-10-24T00:00:00Z"}`))
		case "nmc3_append_event":
			var args map[string]json.RawMessage
			_ = json.NewDecoder(r.Body).Decode(&args)
			var event contracts.RunEvent
			_ = json.Unmarshal(args["p_event"], &event)
			event.Sequence = 1
			_ = json.NewEncoder(w).Encode(event)
		case "nmc3_list_events":
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Errorf("unknown RPC %q", name)
		}
	}))
	defer srv.Close()
	s, err := NewSupabase(srv.URL, "secret", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	run := contracts.Run{ID: "run-1", State: contracts.StatePending, AgentID: "root", Version: "1.0.0", Input: []byte(`{}`), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := s.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(ctx, run); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, run.ID)
	if err != nil || got.State != contracts.StateSucceeded {
		t.Fatalf("get: %+v %v", got, err)
	}
	e, err := s.Append(ctx, contracts.RunEvent{RunID: run.ID, Type: "RUN_STARTED"})
	if err != nil || e.Sequence != 1 || e.Timestamp.IsZero() {
		t.Fatalf("append: %+v %v", e, err)
	}
	events, err := s.List(ctx, run.ID, 0)
	if err != nil || events == nil || len(events) != 0 {
		t.Fatalf("list: %+v %v", events, err)
	}
	if len(calls) != 5 {
		t.Fatalf("RPC calls %v", calls)
	}
}

func TestSupabaseSecretKeyUsesAPIKeyHeaderOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") != "sb_secret_example" || r.Header.Get("Authorization") != "" {
			t.Error("secret key sent in wrong header")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	s, err := NewSupabase(srv.URL, "sb_secret_example", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(context.Background(), contracts.Run{ID: "x"}); err != nil {
		t.Fatal(err)
	}
}

func TestSupabaseRejectsInsecureRemoteURL(t *testing.T) {
	if _, err := NewSupabase("http://example.com", "sb_secret_example", nil); err == nil {
		t.Fatal("insecure remote URL accepted")
	}
}

func TestSupabaseErrorsAreTypedAndSafe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"code":"P0002","message":"secret details"}`))
	}))
	defer srv.Close()
	s, _ := NewSupabase(srv.URL, "top-secret", srv.Client())
	_, err := s.Get(context.Background(), "missing")
	re, ok := err.(*contracts.RunError)
	if !ok || re.Code != contracts.ErrNotFound || strings.Contains(re.Message, "secret") {
		t.Fatalf("unsafe error %v", err)
	}
}
