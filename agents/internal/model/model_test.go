package model

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

func TestDeterministicReplayAndCancellation(t *testing.T) {
	action := contracts.ModelAction{Type: contracts.ActionFinish, Output: json.RawMessage(`{"ok":true}`)}
	adapter := NewDeterministic(map[string][]contracts.ModelResult{"agent": {{Action: action, Usage: contracts.Usage{InputTokens: 4, OutputTokens: 2, CostUSD: .001}}}})
	input := contracts.ModelInput{Definition: contracts.AgentDefinition{ID: "agent"}}
	result, err := adapter.Next(context.Background(), input)
	if err != nil || result.Action.Type != contracts.ActionFinish || result.Usage.InputTokens != 4 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	input.History = []contracts.ActionResult{{Action: action}}
	if _, err := adapter.Next(context.Background(), input); !hasCode(err, contracts.ErrInternal) {
		t.Fatalf("expected exhausted script, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.Next(ctx, input); !hasCode(err, contracts.ErrTimeout) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestOpenAIActionUsageAndSecretRedaction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Error("missing authorization")
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["model"] != "test-model" {
			t.Errorf("model=%v", request["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"TOOL\",\"tool_call\":{\"tool_id\":\"catalog\",\"input\":{\"query\":\"x\"}}}"}}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`))
	}))
	defer server.Close()
	adapter, err := NewOpenAI(OpenAIConfig{APIKey: "secret-token", Model: "test-model", Endpoint: server.URL, InputUSDPerMillion: 2, OutputUSDPerMillion: 10})
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Next(context.Background(), contracts.ModelInput{Definition: contracts.AgentDefinition{ID: "agent"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action.Type != contracts.ActionTool || result.Action.ToolCall.ToolID != "catalog" {
		t.Fatalf("action=%+v", result.Action)
	}
	if result.Usage.InputTokens != 100 || result.Usage.OutputTokens != 20 || result.Usage.CostUSD != .0004 {
		t.Fatalf("usage=%+v", result.Usage)
	}
	server.Close()
	_, err = adapter.Next(context.Background(), contracts.ModelInput{})
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("unsafe error=%v", err)
	}
}

func TestOpenAIRejectsInvalidResponseAndProviderFailure(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		code       contracts.ErrorCode
	}{
		{"bad action", `{"choices":[{"message":{"content":"{\"type\":\"FINISH\",\"unknown\":1}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`, 200, contracts.ErrInvalidInput},
		{"missing usage", `{"choices":[{"message":{"content":"{\"type\":\"FINISH\",\"output\":{}}"}}]}`, 200, contracts.ErrInvalidInput},
		{"negative usage", `{"choices":[{"message":{"content":"{\"type\":\"FINISH\",\"output\":{}}"}}],"usage":{"prompt_tokens":-1,"completion_tokens":1}}`, 200, contracts.ErrInvalidInput},
		{"provider failure", `{"error":"secret-token"}`, 503, contracts.ErrInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			adapter, err := NewOpenAI(OpenAIConfig{APIKey: "secret-token", Model: "test", Endpoint: server.URL, InputUSDPerMillion: 1, OutputUSDPerMillion: 1})
			if err != nil {
				t.Fatal(err)
			}
			_, err = adapter.Next(context.Background(), contracts.ModelInput{})
			if !hasCode(err, tc.code) || strings.Contains(err.Error(), "secret-token") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestOpenAIHonorsDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()
	adapter, err := NewOpenAI(OpenAIConfig{APIKey: "key", Model: "test", Endpoint: server.URL, InputUSDPerMillion: 1, OutputUSDPerMillion: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = adapter.Next(ctx, contracts.ModelInput{})
	if !hasCode(err, contracts.ErrTimeout) {
		t.Fatalf("error=%v", err)
	}
}

func hasCode(err error, code contracts.ErrorCode) bool {
	runErr, ok := err.(*contracts.RunError)
	return ok && runErr.Code == code
}
