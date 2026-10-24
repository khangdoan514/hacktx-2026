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

func TestOpenRouterUsesReportedCost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer router-secret" {
			t.Error("missing bearer key")
		}
		var request struct {
			Model string `json:"model"`
			Usage struct {
				Include bool `json:"include"`
			} `json:"usage"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "provider/model" || !request.Usage.Include {
			t.Fatalf("request=%+v", request)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"FINISH\",\"output\":{\"ok\":true}}"}}],"usage":{"prompt_tokens":50,"completion_tokens":10,"cost":0.000123}}`))
	}))
	defer server.Close()
	adapter, err := NewOpenRouter(OpenRouterConfig{APIKey: "router-secret", Model: "provider/model", Endpoint: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Next(context.Background(), contracts.ModelInput{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Action.Type != contracts.ActionFinish || result.Usage.InputTokens != 50 || result.Usage.OutputTokens != 10 || result.Usage.CostUSD != .000123 {
		t.Fatalf("result=%+v", result)
	}
}

func TestOpenRouterPerRunModelSelection(t *testing.T) {
 var received []string
 server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  var request struct { Model string `json:"model"` }
  if err := json.NewDecoder(r.Body).Decode(&request); err != nil { t.Fatal(err) }
  received = append(received, request.Model)
  _, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"type\":\"FINISH\",\"output\":{}}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"cost":0}}`))
 }))
 defer server.Close()
 adapter, err := NewOpenRouter(OpenRouterConfig{APIKey:"key", Model:"meta/muse-spark-1.3-contributor", Endpoint:server.URL})
 if err != nil { t.Fatal(err) }
 if _, err = adapter.Next(context.Background(), contracts.ModelInput{}); err != nil { t.Fatal(err) }
 if _, err = adapter.Next(context.Background(), contracts.ModelInput{Context: contracts.RunContext{ModelID:"anthropic/claude-sonnet-4.5"}}); err != nil { t.Fatal(err) }
 if len(received)!=2 || received[0]!="meta/muse-spark-1.3-contributor" || received[1]!="anthropic/claude-sonnet-4.5" { t.Fatalf("models=%v", received) }
 for _, bad := range []string{"bad model", "../bad\nkey", strings.Repeat("a", 129)} {
  _, err := adapter.Next(context.Background(), contracts.ModelInput{Context: contracts.RunContext{ModelID:bad}})
  if !hasCode(err, contracts.ErrInvalidInput) || len(received)!=2 { t.Fatalf("bad model %q err=%v requests=%v", bad, err, received) }
 }
 if _, err := NewOpenRouter(OpenRouterConfig{APIKey:"key", Model:"bad model"}); !hasCode(err, contracts.ErrInvalidInput) { t.Fatalf("bad default model err=%v", err) }
}

func TestOpenRouterRejectsMissingCostAndRedactsError(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		code       contracts.ErrorCode
	}{
		{"missing cost", `{"choices":[{"message":{"content":"{\"type\":\"FINISH\",\"output\":{}}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`, 200, contracts.ErrInvalidInput},
		{"negative cost", `{"choices":[{"message":{"content":"{\"type\":\"FINISH\",\"output\":{}}"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"cost":-1}}`, 200, contracts.ErrInvalidInput},
		{"failure", `{"error":{"message":"router-secret"}}`, 429, contracts.ErrInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			adapter, err := NewOpenRouter(OpenRouterConfig{APIKey: "router-secret", Model: "provider/model", Endpoint: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			_, err = adapter.Next(context.Background(), contracts.ModelInput{})
			if !hasCode(err, tc.code) || strings.Contains(err.Error(), "router-secret") {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func hasCode(err error, code contracts.ErrorCode) bool {
	runErr, ok := err.(*contracts.RunError)
	return ok && runErr.Code == code
}

func TestParseActionToleratesChatter(t *testing.T) {
	for _, content := range []string{
		"{\"type\":\"FINISH\",\"output\":{\"ok\":true}} here is my reasoning",
		"```json\n{\"type\":\"FINISH\",\"output\":{\"ok\":true}}\n```",
		"Sure! {\"type\":\"FINISH\",\"output\":{\"ok\":true}} done.",
	} {
		action, err := parseAction(content)
		if err != nil || action.Type != contracts.ActionFinish {
			t.Fatalf("content=%q err=%v", content, err)
		}
	}
	if _, err := parseAction("no json here"); err == nil {
		t.Fatal("expected failure without JSON object")
	}
	if _, err := parseAction("{\"type\":\"FINISH\",\"output\":{},\"unknown\":1} trailing"); err == nil {
		t.Fatal("expected strict field rejection")
	}
}
