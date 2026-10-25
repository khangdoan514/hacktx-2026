package opencode

// REST client for the local OpenCode service (see
// https://opencode.ai/v2/docs/api). Authentication is HTTP Basic with the
// fixed user "opencode" and the service password from the service
// registration file or OPENCODE_SERVICE_PASSWORD. The password is only ever
// sent in the Authorization header and never logged.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const serviceUser = "opencode"

// callTimeout bounds ordinary REST calls. Wait blocks server-side until the
// session idles, so it uses the caller's context with no client timeout.
var callTimeout = 30 * time.Second

type Client struct {
	baseURL  string
	password string
	client   *http.Client
}

func defaultServiceFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "opencode", "service.json")
}

// ResolvePassword prefers the environment, then the service registration
// file. It returns an error when neither is configured so the failure is
// explicit at startup instead of a 401 at runtime.
func ResolvePassword() (string, error) {
	if v := os.Getenv("OPENCODE_SERVICE_PASSWORD"); v != "" {
		return v, nil
	}
	path := os.Getenv("OPENCODE_SERVICE_FILE")
	if path == "" {
		path = defaultServiceFile()
	}
	if path == "" {
		return "", fmt.Errorf("OpenCode service password is not configured")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read OpenCode service registration: %w", err)
	}
	var registration struct {
		Password string `json:"password"`
	}
	if err := json.Unmarshal(data, &registration); err != nil || registration.Password == "" {
		return "", fmt.Errorf("invalid OpenCode service registration")
	}
	return registration.Password, nil
}

func NewClient(baseURL, password string, client *http.Client) (*Client, error) {
	if baseURL == "" || password == "" {
		return nil, fmt.Errorf("OpenCode URL and service password are required")
	}
	if client == nil {
		client = &http.Client{}
	}
	return &Client{baseURL: baseURL, password: password, client: client}, nil
}

func (c *Client) call(ctx context.Context, method, path string, body any, out any) error {
	return c.callWithTimeout(ctx, method, path, body, out, callTimeout)
}

func (c *Client) callWithTimeout(ctx context.Context, method, path string, body any, out any, timeout time.Duration) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode OpenCode request: %w", err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("create OpenCode request: %w", err)
	}
	request.SetBasicAuth(serviceUser, c.password)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return fmt.Errorf("OpenCode request failed: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read OpenCode response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail := strings.TrimSpace(string(data))
		if len(detail) > 180 {
			detail = detail[:180]
		}
		return fmt.Errorf("OpenCode returned HTTP %d: %s", response.StatusCode, detail)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode OpenCode response: %w", err)
		}
	}
	return nil
}

// Session mirrors the subset of Session.Info the coordinator needs: identity,
// outcome, cost, and token usage.
type Session struct {
	ID    string `json:"id"`
	Model struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
	} `json:"model"`
	Outcome string  `json:"outcome"`
	Cost    float64 `json:"cost"`
	Tokens  struct {
		Input     int `json:"input"`
		Output    int `json:"output"`
		Reasoning int `json:"reasoning"`
	} `json:"tokens"`
}

type sessionEnvelope struct {
	Data Session `json:"data"`
}

type messageEnvelope struct {
	Data []Message `json:"data"`
}

// Message mirrors the assistant/user message shape used for event mapping.
// Only text and tool parts are interpreted; other parts are ignored.
type Message struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Agent   string `json:"agent"`
	Content []struct {
		Type  string         `json:"type"`
		Text  string         `json:"text"`
		Name  string         `json:"name"`
		ID    string         `json:"id"`
		State map[string]any `json:"state"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *Client) CreateSession(ctx context.Context, directory, title, agent, providerID, modelID string, permissions []Permission) (Session, error) {
	var out sessionEnvelope
	body := map[string]any{
		"location": map[string]string{"directory": directory},
		"title":    title,
		"agent":    agent,
	}
	if len(permissions) > 0 {
		body["permissions"] = permissions
	}
	if providerID != "" && modelID != "" {
		body["model"] = map[string]string{"providerID": providerID, "id": modelID}
	}
	if err := c.call(ctx, http.MethodPost, "/api/session", body, &out); err != nil {
		return Session{}, err
	}
	if out.Data.ID == "" {
		return Session{}, fmt.Errorf("OpenCode created an empty session")
	}
	return out.Data, nil
}

func (c *Client) Prompt(ctx context.Context, sessionID, text string) error {
	return c.call(ctx, http.MethodPost, "/api/session/"+sessionID+"/prompt", map[string]string{"text": text}, nil)
}

// Wait blocks until the session idles. A 204 means idle; callers enforce the
// run deadline through ctx. No client timeout applies: the wait is long-lived
// by design and the context governs it.
func (c *Client) Wait(ctx context.Context, sessionID string) error {
	return c.callWithTimeout(ctx, http.MethodPost, "/api/experimental/session/"+sessionID+"/wait", nil, nil, 0)
}

func (c *Client) GetSession(ctx context.Context, sessionID string) (Session, error) {
	var out sessionEnvelope
	if err := c.call(ctx, http.MethodGet, "/api/session/"+sessionID, nil, &out); err != nil {
		return Session{}, err
	}
	return out.Data, nil
}

func (c *Client) Messages(ctx context.Context, sessionID string) ([]Message, error) {
	var out messageEnvelope
	if err := c.call(ctx, http.MethodGet, "/api/session/"+sessionID+"/message", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
