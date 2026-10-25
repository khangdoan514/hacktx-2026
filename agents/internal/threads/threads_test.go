package threads

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/opencode"
)

type fake struct {
	release  chan struct{}
	prompted chan string
	messages []opencode.Message
	title    string
	agent    string
}

func (f *fake) CreateSession(_ context.Context, _, title, agent, _, _ string) (opencode.Session, error) {
	f.title, f.agent = title, agent
	return opencode.Session{ID: "ses_1"}, nil
}
func (f *fake) Prompt(_ context.Context, _, text string) error { f.prompted <- text; return nil }
func (f *fake) Wait(ctx context.Context, _ string) error {
	select {
	case <-f.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (f *fake) Messages(context.Context, string) ([]opencode.Message, error) { return f.messages, nil }
func (f *fake) Interrupt(context.Context, string) error                      { return nil }

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func msg(kind string, parts ...map[string]any) opencode.Message {
	m := opencode.Message{ID: kind, Type: kind}
	for _, p := range parts {
		m.Content = append(m.Content, struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			Name  string         `json:"name"`
			ID    string         `json:"id"`
			State map[string]any `json:"state"`
		}{Type: p["type"].(string), Text: str(p["text"]), Name: str(p["name"]), State: map[string]any{"status": "completed"}})
	}
	return m
}

func str(v any) string { s, _ := v.(string); return s }

func TestTranscriptIncludesTopLevelUserText(t *testing.T) {
	messages := transcript([]opencode.Message{
		msg("assistant", map[string]any{"type": "text", "text": "OK"}),
		{ID: "user-1", Type: "user", Text: "Reply with OK only."},
	})
	if len(messages) != 2 || messages[0].Role != "user" || messages[0].Text != "Reply with OK only." || messages[1].Text != "OK" {
		t.Fatalf("unexpected transcript: %+v", messages)
	}
}

func TestTranscriptShowsOriginalUserTextWithAttachmentContext(t *testing.T) {
	context := "What are the values?" + attachmentContextPreamble + "\n\n--- File alpha.txt ---\nsecret file text"
	for _, message := range []opencode.Message{
		{ID: "new", Type: "user", Text: context, Metadata: map[string]any{"nmc3_display_text": "What are the values?"}},
		{ID: "old", Type: "user", Text: context},
	} {
		got := transcript([]opencode.Message{message})
		if len(got) != 1 || got[0].Text != "What are the values?" {
			t.Fatalf("attachment context leaked into transcript: %+v", got)
		}
	}
}

func TestTranscriptIncludesOpenCodeToolContent(t *testing.T) {
	message := opencode.Message{ID: "assistant-tool", Type: "assistant"}
	message.Content = append(message.Content, struct {
		Type  string         `json:"type"`
		Text  string         `json:"text"`
		Name  string         `json:"name"`
		ID    string         `json:"id"`
		State map[string]any `json:"state"`
	}{Type: "tool", Name: "shell", State: map[string]any{
		"status":  "completed",
		"input":   map[string]any{"command": "pwd"},
		"content": []any{map[string]any{"type": "text", "text": "/work"}},
	}})
	got := transcript([]opencode.Message{message})
	if len(got) != 1 || len(got[0].Tools) != 1 || got[0].Tools[0].Output != "/work" {
		t.Fatalf("tool output missing: %+v", got)
	}
}

func TestThreadLifecycle(t *testing.T) {
	f := &fake{release: make(chan struct{}), prompted: make(chan string, 1)}
	h := New(f, "/work", "thread", "meta/model")

	rec := do(h, http.MethodPost, "/v1/threads", `{"title":"Hello"}`)
	if rec.Code != http.StatusCreated || f.agent != "thread" || f.title != "Hello" {
		t.Fatalf("create: %d agent=%q title=%q", rec.Code, f.agent, f.title)
	}

	if rec := do(h, http.MethodPost, "/v1/threads/ses_1/messages", `{"text":"  "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty text: %d", rec.Code)
	}
	if rec := do(h, http.MethodPost, "/v1/threads/ses_1/messages", `{"text":"hi"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("send: %d", rec.Code)
	}
	if got := <-f.prompted; got != "hi" {
		t.Fatalf("prompt = %q", got)
	}
	if rec := do(h, http.MethodPost, "/v1/threads/ses_1/messages", `{"text":"again"}`); rec.Code != http.StatusConflict {
		t.Fatalf("second send while busy: %d", rec.Code)
	}

	f.messages = []opencode.Message{
		msg("assistant", map[string]any{"type": "tool", "name": "catalog_search"}, map[string]any{"type": "text", "text": "done"}),
		msg("user", map[string]any{"type": "text", "text": "hi"}),
	}
	var th Thread
	_ = json.Unmarshal(do(h, http.MethodGet, "/v1/threads/ses_1", "").Body.Bytes(), &th)
	if !th.Busy || len(th.Messages) != 2 || th.Messages[0].Role != "user" || th.Messages[1].Tools[0].Name != "catalog_search" {
		t.Fatalf("busy transcript: %+v", th)
	}

	close(f.release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = json.Unmarshal(do(h, http.MethodGet, "/v1/threads/ses_1", "").Body.Bytes(), &th)
		if !th.Busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("thread stayed busy after the turn finished")
}

func TestRejectsBadIDs(t *testing.T) {
	h := New(&fake{}, "/work", "thread", "meta/model")
	if rec := do(h, http.MethodGet, "/v1/threads/..%2Fapi", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("bad id: %d", rec.Code)
	}
}
