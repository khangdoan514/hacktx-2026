// Package threads is a bare chat surface over OpenCode sessions: create a
// thread, send it messages, read the transcript back. There are no
// pre-compiled prompts; the session uses the "thread" profile in
// opencode.jsonc and whatever tools that profile allows.
//
// OpenCode owns execution sessions. Postgres owns the product thread index and
// transcript snapshots; S3-compatible storage owns attachment bytes.
package threads

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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

// Store owns the product index and the last durable transcript snapshot.
type Store interface {
	Create(context.Context, Thread) error
	Save(context.Context, Thread) error
	Get(context.Context, string, string, string) (Thread, error)
	List(context.Context, string, string) ([]Thread, error)
	AddFile(context.Context, Attachment) error
	Files(context.Context, string, string, string) ([]Attachment, error)
	File(context.Context, string, string, string) (Attachment, error)
	Archive(context.Context, string, string, string) error
}

type BlobStore interface {
	Put(context.Context, string, string, io.Reader, int64) error
	Get(context.Context, string) (io.ReadCloser, error)
}

type Attachment struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	Checksum    string    `json:"checksum_sha256"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
	ObjectKey   string    `json:"-"`
	ThreadID    string    `json:"-"`
	TenantID    string    `json:"-"`
	PrincipalID string    `json:"-"`
}

const (
	errBusy                   contracts.ErrorCode = "THREAD_BUSY"
	maxText                                       = 32 << 10
	maxFileSize                                   = 20 << 20
	turnTimeout                                   = 15 * time.Minute
	attachmentContextPreamble                     = "\n\nThe following files were uploaded by the user. Their text is included below when available; image and PDF data is attached separately. These are not workspace files. Analyze the provided content directly."
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type Handler struct {
	backend Backend
	store   Store
	blobs   BlobStore
	workdir string
	agent   string
	model   string

	mu    sync.Mutex
	busy  map[string]context.CancelFunc
	fails map[string]string
}

// New returns a handler. agent names the opencode.jsonc profile and model is
// the default "provider/model" selector.
func New(backend Backend, workdir, agent, model string, stores ...Store) *Handler {
	h := &Handler{backend: backend, workdir: workdir, agent: agent, model: model, busy: map[string]context.CancelFunc{}, fails: map[string]string{}}
	if len(stores) > 0 {
		h.store = stores[0]
	}
	return h
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.Handle("/v1/threads", h)
	mux.Handle("/v1/threads/", h)
}

type Tool struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
	Input  any    `json:"input,omitempty"`
	Output any    `json:"output,omitempty"`
}

type Message struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Text  string `json:"text"`
	Tools []Tool `json:"tools"`
	Error string `json:"error,omitempty"`
}

type Thread struct {
	ID          string                       `json:"id"`
	Title       string                       `json:"title,omitempty"`
	Busy        bool                         `json:"busy"`
	Error       string                       `json:"error,omitempty"`
	Messages    []Message                    `json:"messages"`
	Files       []Attachment                 `json:"files"`
	Permissions []opencode.PermissionRequest `json:"permissions,omitempty"`
	UpdatedAt   time.Time                    `json:"updated_at,omitempty"`
	TenantID    string                       `json:"-"`
	PrincipalID string                       `json:"-"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/v1/threads" {
		if r.Method == http.MethodPost {
			h.create(w, r)
			return
		}
		if r.Method == http.MethodGet {
			h.list(w, r)
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
	case len(parts) == 2 && parts[1] == "events" && r.Method == http.MethodGet:
		h.stream(w, r, id)
	case len(parts) == 2 && parts[1] == "files" && r.Method == http.MethodPost:
		h.upload(w, r, id)
	case len(parts) == 2 && parts[1] == "files" && r.Method == http.MethodGet:
		h.files(w, r, id)
	case len(parts) == 3 && parts[1] == "files" && r.Method == http.MethodGet:
		h.download(w, r, id, parts[2])
	case len(parts) == 2 && parts[1] == "messages" && r.Method == http.MethodPost:
		h.send(w, r, id)
	case len(parts) == 4 && parts[1] == "permissions" && parts[3] == "reply" && r.Method == http.MethodPost:
		h.replyPermission(w, r, id, parts[2])
	case len(parts) == 2 && parts[1] == "interrupt" && r.Method == http.MethodPost:
		h.interrupt(w, r, id)
	case len(parts) == 2 && parts[1] == "archive" && r.Method == http.MethodPost:
		h.archive(w, r, id)
	default:
		fail(w, http.StatusNotFound, contracts.ErrNotFound, "endpoint not found")
	}
}

func (h *Handler) archive(w http.ResponseWriter, r *http.Request, id string) {
	if h.store == nil {
		fail(w, http.StatusServiceUnavailable, contracts.ErrInternal, "thread storage is not configured")
		return
	}
	if err := h.store.Archive(r.Context(), id, r.Header.Get("X-NMC3-Tenant"), r.Header.Get("X-NMC3-User")); err != nil {
		fail(w, http.StatusNotFound, contracts.ErrNotFound, "thread not found")
		return
	}
	write(w, http.StatusOK, map[string]any{"id": id, "archived": true})
}

func (h *Handler) SetBlobStore(blobs BlobStore) { h.blobs = blobs }

func (h *Handler) upload(w http.ResponseWriter, r *http.Request, threadID string) {
	if h.store == nil || h.blobs == nil {
		fail(w, http.StatusServiceUnavailable, contracts.ErrInternal, "file storage is not configured")
		return
	}
	tenant, principal := r.Header.Get("X-NMC3-Tenant"), r.Header.Get("X-NMC3-User")
	if _, err := h.store.Get(r.Context(), threadID, tenant, principal); err != nil {
		fail(w, http.StatusNotFound, contracts.ErrNotFound, "thread not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFileSize+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		fail(w, http.StatusRequestEntityTooLarge, contracts.ErrInvalidInput, "file must be 20 MB or smaller")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, "file is required")
		return
	}
	defer file.Close()
	if header.Size > maxFileSize {
		fail(w, http.StatusRequestEntityTooLarge, contracts.ErrInvalidInput, "file must be 20 MB or smaller")
		return
	}
	id := newFileID()
	name := filepath.Base(header.Filename)
	if name == "." || name == "" {
		name = "attachment"
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(name))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	key := threadID + "/" + id
	hash := sha256.New()
	if err := h.blobs.Put(r.Context(), key, contentType, io.TeeReader(file, hash), header.Size); err != nil {
		fail(w, http.StatusBadGateway, contracts.ErrInternal, "could not store file")
		return
	}
	item := Attachment{ID: id, Name: name, ContentType: contentType, Size: header.Size, Checksum: hex.EncodeToString(hash.Sum(nil)), ObjectKey: key, ThreadID: threadID, TenantID: tenant, PrincipalID: principal}
	if err := h.store.AddFile(r.Context(), item); err != nil {
		fail(w, http.StatusInternalServerError, contracts.ErrInternal, "could not save file metadata")
		return
	}
	write(w, http.StatusCreated, item)
}

func newFileID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "file-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b)
}

func (h *Handler) files(w http.ResponseWriter, r *http.Request, threadID string) {
	if h.store == nil {
		write(w, http.StatusOK, map[string]any{"files": []Attachment{}})
		return
	}
	if _, err := h.store.Get(r.Context(), threadID, r.Header.Get("X-NMC3-Tenant"), r.Header.Get("X-NMC3-User")); err != nil {
		fail(w, http.StatusNotFound, contracts.ErrNotFound, "thread not found")
		return
	}
	items, err := h.store.Files(r.Context(), threadID, r.Header.Get("X-NMC3-Tenant"), r.Header.Get("X-NMC3-User"))
	if err != nil {
		fail(w, http.StatusNotFound, contracts.ErrNotFound, "thread not found")
		return
	}
	write(w, http.StatusOK, map[string]any{"files": items})
}

func (h *Handler) download(w http.ResponseWriter, r *http.Request, threadID, fileID string) {
	if h.store == nil || h.blobs == nil {
		fail(w, http.StatusServiceUnavailable, contracts.ErrInternal, "file storage is not configured")
		return
	}
	item, err := h.store.File(r.Context(), fileID, r.Header.Get("X-NMC3-Tenant"), r.Header.Get("X-NMC3-User"))
	if err != nil || item.ThreadID != threadID {
		fail(w, http.StatusNotFound, contracts.ErrNotFound, "file not found")
		return
	}
	body, err := h.blobs.Get(r.Context(), item.ObjectKey)
	if err != nil {
		fail(w, http.StatusBadGateway, contracts.ErrInternal, "could not retrieve file")
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", item.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.Name}))
	_, _ = io.Copy(w, body)
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
	if runes := []rune(title); len(runes) > 120 {
		title = string(runes[:120])
	}
	if title == "" {
		title = "Thread"
	}
	session, err := h.backend.CreateSession(r.Context(), h.workdir, title, h.agent, provider, modelID)
	if err != nil {
		fail(w, http.StatusBadGateway, contracts.ErrInternal, "could not start a thread")
		return
	}
	thread := Thread{ID: session.ID, Title: title, Messages: []Message{}, UpdatedAt: time.Now().UTC(), TenantID: r.Header.Get("X-NMC3-Tenant"), PrincipalID: r.Header.Get("X-NMC3-User")}
	if h.store != nil {
		if err := h.store.Create(r.Context(), thread); err != nil {
			fail(w, http.StatusInternalServerError, contracts.ErrInternal, "could not save the thread")
			return
		}
	}
	thread.Files = []Attachment{}
	write(w, http.StatusCreated, thread)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, id string) {
	messages, err := h.backend.Messages(r.Context(), id)
	if err != nil {
		if h.store != nil {
			saved, loadErr := h.store.Get(r.Context(), id, r.Header.Get("X-NMC3-Tenant"), r.Header.Get("X-NMC3-User"))
			if loadErr == nil {
				saved.Busy = false
				saved.Files, _ = h.store.Files(r.Context(), id, saved.TenantID, saved.PrincipalID)
				write(w, http.StatusOK, saved)
				return
			}
		}
		fail(w, http.StatusNotFound, contracts.ErrNotFound, "thread not found")
		return
	}
	h.mu.Lock()
	_, busy := h.busy[id]
	failure := h.fails[id]
	h.mu.Unlock()
	thread := Thread{ID: id, Busy: busy, Error: failure, Messages: transcript(messages), UpdatedAt: time.Now().UTC(), TenantID: r.Header.Get("X-NMC3-Tenant"), PrincipalID: r.Header.Get("X-NMC3-User")}
	thread.Permissions = h.pendingPermissions(r.Context(), id)
	if h.store != nil {
		if saved, err := h.store.Get(r.Context(), id, thread.TenantID, thread.PrincipalID); err == nil {
			thread.Title = saved.Title
			thread.UpdatedAt = saved.UpdatedAt
		} else {
			fail(w, http.StatusNotFound, contracts.ErrNotFound, "thread not found")
			return
		}
		thread.Files, _ = h.store.Files(r.Context(), id, thread.TenantID, thread.PrincipalID)
		_ = h.store.Save(r.Context(), thread)
	}
	write(w, http.StatusOK, thread)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		write(w, http.StatusOK, map[string]any{"threads": []Thread{}})
		return
	}
	items, err := h.store.List(r.Context(), r.Header.Get("X-NMC3-Tenant"), r.Header.Get("X-NMC3-User"))
	if err != nil {
		fail(w, http.StatusInternalServerError, contracts.ErrInternal, "could not list threads")
		return
	}
	write(w, http.StatusOK, map[string]any{"threads": items})
}

// stream emits transcript snapshots as server sent events while an OpenCode
// turn is active, then sends one final snapshot and closes the response.
func (h *Handler) stream(w http.ResponseWriter, r *http.Request, id string) {
	if h.store != nil {
		if _, err := h.store.Get(r.Context(), id, r.Header.Get("X-NMC3-Tenant"), r.Header.Get("X-NMC3-User")); err != nil {
			fail(w, http.StatusNotFound, contracts.ErrNotFound, "thread not found")
			return
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, contracts.ErrInternal, "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		messages, err := h.backend.Messages(r.Context(), id)
		if err != nil {
			return
		}
		h.mu.Lock()
		_, busy := h.busy[id]
		failure := h.fails[id]
		h.mu.Unlock()
		thread := Thread{ID: id, Busy: busy, Error: failure, Messages: transcript(messages), UpdatedAt: time.Now().UTC()}
		thread.Permissions = h.pendingPermissions(r.Context(), id)
		if h.store != nil {
			tenant, principal := r.Header.Get("X-NMC3-Tenant"), r.Header.Get("X-NMC3-User")
			if saved, e := h.store.Get(r.Context(), id, tenant, principal); e != nil {
				return
			} else {
				thread.Title = saved.Title
			}
			thread.TenantID, thread.PrincipalID = tenant, principal
			thread.Files, _ = h.store.Files(r.Context(), id, tenant, principal)
			_ = h.store.Save(r.Context(), thread)
		}
		body, _ := json.Marshal(thread)
		_, _ = w.Write(append(append([]byte("data: "), body...), []byte("\n\n")...))
		flusher.Flush()
		if !busy {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Text    string   `json:"text"`
		FileIDs []string `json:"file_ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" && len(req.FileIDs) > 0 {
		text = "Analyze the attached file(s)."
	}
	if text == "" || len(text) > maxText {
		fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, "message text or an attachment is required, and text must be under 32 KB")
		return
	}
	userText := text
	tenant, principal := r.Header.Get("X-NMC3-Tenant"), r.Header.Get("X-NMC3-User")
	var saved Thread
	if h.store != nil {
		var err error
		saved, err = h.store.Get(r.Context(), id, tenant, principal)
		if err != nil {
			fail(w, http.StatusNotFound, contracts.ErrNotFound, "thread not found")
			return
		}
	}
	var attachments []opencode.FileAttachment
	var fileContext strings.Builder
	if len(req.FileIDs) > 0 && (h.store == nil || h.blobs == nil) {
		fail(w, http.StatusServiceUnavailable, contracts.ErrInternal, "file storage is not configured")
		return
	}
	if h.store != nil && h.blobs != nil {
		files, err := h.store.Files(r.Context(), id, tenant, principal)
		if err != nil {
			fail(w, http.StatusBadGateway, contracts.ErrInternal, "could not list attachments")
			return
		}
		selected := map[string]bool{}
		if len(req.FileIDs) > 10 {
			fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, "attach at most 10 files per message")
			return
		}
		for _, fileID := range req.FileIDs {
			selected[fileID] = true
		}
		for _, file := range files {
			if !selected[file.ID] {
				continue
			}
			delete(selected, file.ID)
			body, err := h.blobs.Get(r.Context(), file.ObjectKey)
			if err != nil {
				fail(w, http.StatusBadGateway, contracts.ErrInternal, "could not retrieve an attachment")
				return
			}
			data, err := io.ReadAll(io.LimitReader(body, maxFileSize+1))
			body.Close()
			if err != nil || len(data) > maxFileSize {
				fail(w, http.StatusBadGateway, contracts.ErrInternal, "could not read an attachment")
				return
			}
			attachments = append(attachments, promptAttachment(file.Name, file.ContentType, data))
			fileContext.WriteString(fmt.Sprintf("\n\n--- File %q ---\n", file.Name))
			if inline := inlineAttachmentText(file.Name, data); inline != "" && fileContext.Len() < 512<<10 {
				fileContext.WriteString(inline)
			} else {
				fileContext.WriteString("This file is included as a native image, PDF, or binary attachment.")
			}
		}
		if len(selected) > 0 {
			fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, "attachment does not belong to this thread")
			return
		}
	}
	if fileContext.Len() > 0 {
		text += attachmentContextPreamble
		text += fileContext.String()
	}
	if h.store != nil {
		if saved.Title == "Thread" || saved.Title == "New thread" {
			titleRunes := []rune(userText)
			if len(titleRunes) > 48 {
				titleRunes = append(titleRunes[:48], '…')
			}
			saved.Title = string(titleRunes)
		}
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
	if h.store != nil {
		saved.Busy = true
		_ = h.store.Save(context.Background(), saved)
	}

	go func() {
		defer cancel()
		var err error
		if backend, ok := h.backend.(interface {
			PromptWithDisplayText(context.Context, string, string, string, []opencode.FileAttachment) error
		}); ok {
			err = backend.PromptWithDisplayText(ctx, id, text, userText, attachments)
		} else if backend, ok := h.backend.(interface {
			PromptWithFiles(context.Context, string, string, []opencode.FileAttachment) error
		}); ok {
			err = backend.PromptWithFiles(ctx, id, text, attachments)
		} else {
			err = h.backend.Prompt(ctx, id, text)
		}
		if err == nil {
			err = h.backend.Wait(ctx, id)
		}
		h.mu.Lock()
		delete(h.busy, id)
		if err != nil && !errors.Is(err, context.Canceled) {
			h.fails[id] = "The agent could not finish this turn."
		}
		h.mu.Unlock()
		if h.store != nil {
			saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer saveCancel()
			if messages, readErr := h.backend.Messages(saveCtx, id); readErr == nil {
				final := saved
				final.Busy = false
				final.Messages = transcript(messages)
				_ = h.store.Save(saveCtx, final)
			}
		}
	}()
	write(w, http.StatusAccepted, map[string]any{"id": id, "busy": true})
}

func (h *Handler) interrupt(w http.ResponseWriter, r *http.Request, id string) {
	if h.store != nil {
		if _, err := h.store.Get(r.Context(), id, r.Header.Get("X-NMC3-Tenant"), r.Header.Get("X-NMC3-User")); err != nil {
			fail(w, http.StatusNotFound, contracts.ErrNotFound, "thread not found")
			return
		}
	}
	if err := h.backend.Interrupt(r.Context(), id); err != nil {
		fail(w, http.StatusBadGateway, contracts.ErrInternal, "could not stop the thread")
		return
	}
	write(w, http.StatusOK, map[string]any{"id": id})
}

func (h *Handler) pendingPermissions(ctx context.Context, id string) []opencode.PermissionRequest {
	backend, ok := h.backend.(interface {
		PermissionRequests(context.Context, string) ([]opencode.PermissionRequest, error)
	})
	if !ok {
		return nil
	}
	requests, err := backend.PermissionRequests(ctx, id)
	if err != nil {
		return nil
	}
	return requests
}

func (h *Handler) replyPermission(w http.ResponseWriter, r *http.Request, id, requestID string) {
	if !idPattern.MatchString(requestID) {
		fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, "invalid permission request")
		return
	}
	if h.store != nil {
		if _, err := h.store.Get(r.Context(), id, r.Header.Get("X-NMC3-Tenant"), r.Header.Get("X-NMC3-User")); err != nil {
			fail(w, http.StatusNotFound, contracts.ErrNotFound, "thread not found")
			return
		}
	}
	var req struct {
		Decision string `json:"decision"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Decision != "once" && req.Decision != "reject" {
		fail(w, http.StatusBadRequest, contracts.ErrInvalidInput, "decision must be once or reject")
		return
	}
	backend, ok := h.backend.(interface {
		ReplyPermission(context.Context, string, string, string) error
	})
	if !ok {
		fail(w, http.StatusServiceUnavailable, contracts.ErrInternal, "permission approvals are unavailable")
		return
	}
	if err := backend.ReplyPermission(r.Context(), id, requestID, req.Decision); err != nil {
		fail(w, http.StatusBadGateway, contracts.ErrInternal, "could not reply to permission request")
		return
	}
	write(w, http.StatusOK, map[string]any{"id": requestID, "decision": req.Decision})
}

// OpenCode accepts native text, image and PDF attachments. Office documents
// are ZIP/XML containers, so extract readable XML text before attaching them.
// Other binaries remain stored for download and carry an honest content note.
func promptAttachment(name, claimedType string, data []byte) opencode.FileAttachment {
	detected := http.DetectContentType(data)
	ext := strings.ToLower(filepath.Ext(name))
	mediaType := "text/plain"
	content := data
	switch {
	case detected == "application/pdf" || bytes.HasPrefix(data, []byte("%PDF-")):
		mediaType = "application/pdf"
	case strings.HasPrefix(detected, "image/png"), strings.HasPrefix(detected, "image/jpeg"), strings.HasPrefix(detected, "image/gif"), strings.HasPrefix(detected, "image/webp"):
		mediaType = strings.Split(detected, ";")[0]
	case ext == ".docx" || ext == ".xlsx" || ext == ".pptx" || ext == ".odt" || ext == ".ods" || ext == ".odp" || ext == ".zip":
		content = []byte(extractZipText(data))
	case utf8.Valid(data) && !bytes.Contains(data, []byte{0}):
		content = data
	default:
		content = []byte(fmt.Sprintf("%s is stored as a %d-byte %s file. This format cannot be decoded for model analysis here; ask for a PDF, image, or text export to inspect its contents.", name, len(data), claimedType))
	}
	if len(content) > maxFileSize {
		content = content[:maxFileSize]
	}
	return opencode.FileAttachment{URI: "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(content), Name: name}
}

// Short text and Office files are included directly in the message as well as
// attached through OpenCode. This makes their contents reliable with models
// that otherwise try to find attachment names in the workspace.
func inlineAttachmentText(name string, data []byte) string {
	detected := http.DetectContentType(data)
	if detected == "application/pdf" || bytes.HasPrefix(data, []byte("%PDF-")) || strings.HasPrefix(detected, "image/") {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(name))
	var content string
	if ext == ".docx" || ext == ".xlsx" || ext == ".pptx" || ext == ".odt" || ext == ".ods" || ext == ".odp" || ext == ".zip" {
		content = extractZipText(data)
	} else if utf8.Valid(data) && !bytes.Contains(data, []byte{0}) {
		content = string(data)
	}
	const inlineLimit = 64 << 10
	if len(content) > inlineLimit {
		return content[:inlineLimit] + "\n[Inline preview truncated; full text is also attached.]"
	}
	return content
}

func extractZipText(data []byte) string {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "This archive could not be read."
	}
	var names []string
	for _, f := range z.File {
		if !f.FileInfo().IsDir() {
			names = append(names, f.Name)
		}
	}
	sort.Strings(names)
	var out strings.Builder
	out.WriteString("Archive contents:\n")
	for _, name := range names {
		out.WriteString(name + "\n")
	}
	for _, f := range z.File {
		if out.Len() >= 4<<20 {
			break
		}
		if f.UncompressedSize64 > 4<<20 {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		if ext != ".xml" && ext != ".txt" && ext != ".md" && ext != ".csv" && ext != ".json" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(r, 4<<20))
		r.Close()
		if err != nil {
			continue
		}
		out.WriteString("\n--- " + f.Name + " ---\n")
		if ext != ".xml" {
			out.Write(body)
			continue
		}
		dec := xml.NewDecoder(bytes.NewReader(body))
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			if ch, ok := tok.(xml.CharData); ok {
				value := strings.TrimSpace(html.UnescapeString(string(ch)))
				if value != "" {
					out.WriteString(value + " ")
				}
			}
		}
		out.WriteByte('\n')
	}
	return out.String()
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
		messageText := m.Text
		if m.Type == "user" {
			if displayText, ok := m.Metadata["nmc3_display_text"].(string); ok {
				messageText = displayText
			} else if before, _, found := strings.Cut(messageText, attachmentContextPreamble); found {
				// Hide context from prompts sent before display metadata existed.
				messageText = before
			}
		}
		if strings.TrimSpace(messageText) != "" {
			texts = append(texts, messageText)
		}
		for _, part := range m.Content {
			switch part.Type {
			case "text":
				if strings.TrimSpace(part.Text) != "" {
					texts = append(texts, part.Text)
				}
			case "tool":
				status, _ := part.State["status"].(string)
				msg.Tools = append(msg.Tools, Tool{Name: part.Name, Status: status, Input: part.State["input"], Output: toolOutput(part.State)})
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

func toolOutput(state map[string]any) any {
	if output := state["output"]; output != nil {
		return output
	}
	parts, ok := state["content"].([]any)
	if !ok {
		return nil
	}
	var out strings.Builder
	for _, part := range parts {
		block, ok := part.(map[string]any)
		if !ok {
			continue
		}
		if block["type"] == "text" {
			if value, ok := block["text"].(string); ok {
				if out.Len() > 0 {
					out.WriteByte('\n')
				}
				out.WriteString(value)
			}
		}
	}
	if out.Len() == 0 {
		return nil
	}
	return out.String()
}

func write(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code contracts.ErrorCode, message string) {
	write(w, status, contracts.ErrorResponse{Error: contracts.RunError{Code: code, Message: message}})
}
