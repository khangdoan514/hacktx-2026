package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
)

// Supabase calls service-role-only RPCs. The secret must never reach a browser.
type Supabase struct {
	endpoint string
	key      string
	client   *http.Client
}

func NewSupabase(projectURL, serviceRoleKey string, client *http.Client) (*Supabase, error) {
	u, err := url.Parse(projectURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) {
		return nil, fmt.Errorf("invalid Supabase project URL")
	}
	if serviceRoleKey == "" {
		return nil, fmt.Errorf("Supabase service role key is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Supabase{endpoint: strings.TrimRight(projectURL, "/") + "/rest/v1/rpc/", key: serviceRoleKey, client: client}, nil
}

type rpcError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *Supabase) call(ctx context.Context, name string, args any, out any) error {
	body, err := json.Marshal(args)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+name, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("apikey", s.key)
	// Current sb_secret keys are not JWTs and belong only in apikey. Legacy
	// service_role JWTs may also be supplied as Bearer for older projects.
	if !strings.HasPrefix(s.key, "sb_secret_") {
		req.Header.Set("Authorization", "Bearer "+s.key)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		var p rpcError
		_ = json.Unmarshal(data, &p)
		switch p.Code {
		case "P0002":
			return &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
		case "23505":
			return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "run ID already exists"}
		case "22023":
			return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "invalid run state transition"}
		default:
			return &contracts.RunError{Code: contracts.ErrInternal, Message: "run storage request failed"}
		}
	}
	if out != nil && len(data) > 0 && string(data) != "null" {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("invalid storage response: %w", err)
		}
	}
	if out != nil && (len(data) == 0 || string(data) == "null") {
		return &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	return nil
}

func (s *Supabase) Create(ctx context.Context, run contracts.Run) error {
	return s.call(ctx, "nmc3_create_run", map[string]any{"p_run": run}, nil)
}
func (s *Supabase) Get(ctx context.Context, id string) (contracts.Run, error) {
	var run contracts.Run
	err := s.call(ctx, "nmc3_get_run", map[string]any{"p_id": id}, &run)
	if run.Invocations == nil {
		run.Invocations = []contracts.AgentInvocation{}
	}
	if run.ToolInvocations == nil {
		run.ToolInvocations = []contracts.ToolInvocation{}
	}
	return run, err
}
func (s *Supabase) Update(ctx context.Context, run contracts.Run) error {
	return s.call(ctx, "nmc3_update_run", map[string]any{"p_run": run}, nil)
}
func (s *Supabase) Append(ctx context.Context, event contracts.RunEvent) (contracts.RunEvent, error) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	var saved contracts.RunEvent
	err := s.call(ctx, "nmc3_append_event", map[string]any{"p_event": event}, &saved)
	return saved, err
}
func (s *Supabase) List(ctx context.Context, id string, after uint64) ([]contracts.RunEvent, error) {
	var events []contracts.RunEvent
	err := s.call(ctx, "nmc3_list_events", map[string]any{"p_id": id, "p_after": after}, &events)
	if events == nil && err == nil {
		events = []contracts.RunEvent{}
	}
	return events, err
}
