// Package threads is a bare chat surface over OpenCode sessions: create a
// thread, send it messages, read the transcript back. There are no
// pre-compiled prompts; the session uses the "thread" profile in
// opencode.jsonc and whatever tools that profile allows.
//
// Threads are not stored here. OpenCode keeps the transcript, so a thread
// survives an agent restart as long as a client remembers its ID. This
// process only tracks which threads are mid-turn.
package threads

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	"github.com/khangdoan514/hacktx-2026/agents/internal/opencode"
)

// Backend is the part of the OpenCode client threads need.
type Backend interface {
	CreateSession(ctx context.Context, directory, title, agent, providerID, modelID string) (opencode.Session, error)
	Prompt(ctx context.Context, sessionID, text string) error
	Wait(ctx context.Context, sessionID string) error
	Messages(ctx context.Context, sessionID string) ([]opencode.Message, error)
	Interrupt(ctx context.Context, sessionID string) error
}

const (
	errBusy     contracts.ErrorCode = "THREAD_BUSY"
	maxText                         = 32 << 10
	turnTimeout                     = 15 * time.Minute
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type Handler struct {
	backend Backend
	workdir string
	agent   string
	model   string

	mu    sync.Mutex
	busy  map[string]context.CancelFunc
	fails map[string]string
}

// New returns a handler. agent names the opencode.jsonc profile and model is
// the default "provider/model" selector.
func New(backend Backend, workdir, agent, model string) *Handler {
	return &Handler{backend: backend, workdir: workdir, agent: agent, model: model, busy: map[string]context.CancelFunc{}, fails: map[string]string{}}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.Handle("/v1/threads", h)
	mux.Handle("/v1/threads/", h)
}

type Tool struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

type Message struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Text  string `json:"text"`
	Tools []Tool `json:"tools"`
	Error string `json:"error,omitempty"`
}

type Thread struct {
	ID       string    `json:"id"`
	Title    string    `json:"title,omitempty"`
	Busy     bool      `json:"busy"`
	Error    string    `json:"error,omitempty"`
	Messages []Message `json:"messages"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/v1/threads" {
		if r.Method == http.MethodPost {
			h.create(w, r)
			return
		}
		fail(w, http.StatusMethodNotAllowed, contracts.ErrInvalidInput, "method not allowed")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/threads/"), "/")
	if len(parts) == 0 || !idPattern.MatchString(parts[0]) {
		fail(w, http.StatusNotFound, contracts.ErrNotFound, "thread not found")
		return
	}
	id := parts[0]
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		h.get(w, r, id)
	case len(parts) == 2 && parts[1] == "messages" && r.Method == http.MethodPost:
		h.send(w, r, id)
	case len(parts) == 2 && parts[1] == "interrupt" && r.Method == http.MethodPost:
		h.interrupt(w, r, id)
	default:
		fail(w, http.StatusNotFound, contracts.ErrNotFound, "endpoint not found")
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, "invalid request JSON")
		return false
	}
	return true
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Model string `json:"model"`
	}
	if !decode(w, r, &req) {
		return
	}
	model := req.Model
	if model == "" {
		model = h.model
	}
	if !contracts.ValidModelID(model) {
		fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, "invalid model ID")
		return
	}
	provider, modelID, ok := strings.Cut(model, "/")
	if !ok || provider == "" || modelID == "" {
		fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, "model must be provider/model")
		return
	}
	title := strings.TrimSpace(req.Title)
	if len(title) > 120 {
		title = title[:120]
	}
	if title == "" {
		title = "Thread"
	}
	session, err := h.backend.CreateSession(r.Context(), h.workdir, title, h.agent, provider, modelID)
	if err != nil {
		fail(w, http.StatusBadGateway, contracts.ErrInternal, "could not start a thread")
		return
	}
	write(w, http.StatusCreated, Thread{ID: session.ID, Title: title, Messages: []Message{}})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, id string) {
	messages, err := h.backend.Messages(r.Context(), id)
	if err != nil {
		fail(w, http.StatusNotFound, contracts.ErrNotFound, "thread not found")
		return
	}
	h.mu.Lock()
	_, busy := h.busy[id]
	failure := h.fails[id]
	h.mu.Unlock()
	write(w, http.StatusOK, Thread{ID: id, Busy: busy, Error: failure, Messages: transcript(messages)})
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &req) {
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" || len(text) > maxText {
		fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, "message text is required and must be under 32 KB")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), turnTimeout)
	h.mu.Lock()
	if _, running := h.busy[id]; running {
		h.mu.Unlock()
		cancel()
		fail(w, http.StatusConflict, errBusy, "the thread is still answering")
		return
	}
	h.busy[id] = cancel
	delete(h.fails, id)
	h.mu.Unlock()

	go func() {
		defer cancel()
		err := h.backend.Prompt(ctx, id, text)
		if err == nil {
			err = h.backend.Wait(ctx, id)
		}
		h.mu.Lock()
		delete(h.busy, id)
		if err != nil && !errors.Is(err, context.Canceled) {
			h.fails[id] = "The agent could not finish this turn."
		}
		h.mu.Unlock()
	}()
	write(w, http.StatusAccepted, map[string]any{"id": id, "busy": true})
}

func (h *Handler) interrupt(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.backend.Interrupt(r.Context(), id); err != nil {
		fail(w, http.StatusBadGateway, contracts.ErrInternal, "could not stop the thread")
		return
	}
	write(w, http.StatusOK, map[string]any{"id": id})
}

// transcript turns OpenCode's newest-first list into chronological chat
// messages. Only text and tool parts are shown.
func transcript(in []opencode.Message) []Message {
	out := make([]Message, 0, len(in))
	for i := len(in) - 1; i >= 0; i-- {
		m := in[i]
		msg := Message{ID: m.ID, Role: "user", Tools: []Tool{}}
		if m.Type == "assistant" {
			msg.Role = "assistant"
		}
		var texts []string
		for _, part := range m.Content {
			switch part.Type {
			case "text":
				if strings.TrimSpace(part.Text) != "" {
					texts = append(texts, part.Text)
				}
			case "tool":
				status, _ := part.State["status"].(string)
				msg.Tools = append(msg.Tools, Tool{Name: part.Name, Status: status})
			}
		}
		msg.Text = strings.Join(texts, "\n\n")
		if m.Error != nil {
			msg.Error = m.Error.Message
		}
		if msg.Text == "" && len(msg.Tools) == 0 && msg.Error == "" {
			continue
		}
		out = append(out, msg)
	}
	return out
}

func write(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code contracts.ErrorCode, message string) {
	write(w, status, contracts.ErrorResponse{Error: contracts.RunError{Code: code, Message: message}})
}
