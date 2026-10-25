package toolpacks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxText = 100 << 10 // bytes of file text returned to a model

// Runner executes pack tools against the real service APIs using the stored
// credential. Base URLs are fields so tests can point them at a stub.
type Runner struct {
	Store     *Store
	Client    *http.Client
	GitHubAPI string
	DriveAPI  string
}

func NewRunner(store *Store) *Runner {
	return &Runner{
		Store:     store,
		Client:    &http.Client{Timeout: 20 * time.Second},
		GitHubAPI: "https://api.github.com",
		DriveAPI:  "https://www.googleapis.com/drive/v3",
	}
}

// ErrNotConnected is returned when a pack has no credential.
type ErrNotConnected struct{ Pack string }

func (e ErrNotConnected) Error() string {
	return fmt.Sprintf("%s is not connected. Connect it from the Tools page in the console, then retry.", e.Pack)
}

// Call runs one tool. The result is JSON-serializable and never contains the
// credential.
func (r *Runner) Call(ctx context.Context, packID, tool string, args json.RawMessage) (any, error) {
	pack, ok := Find(packID)
	if !ok {
		return nil, fmt.Errorf("unknown tool pack %q", packID)
	}
	known := false
	for _, t := range pack.Tools {
		known = known || t.Name == tool
	}
	if !known {
		return nil, fmt.Errorf("unknown tool %s.%s", packID, tool)
	}
	var in map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, errors.New("arguments must be a JSON object")
		}
	}
	cred, err := r.credential(ctx, packID)
	if err != nil {
		return nil, err
	}
	a := arguments(in)
	if packID == "github" {
		return r.github(ctx, cred, tool, a)
	}
	return r.drive(ctx, cred, tool, a)
}

func (r *Runner) credential(ctx context.Context, pack string) (Credential, error) {
	cred, ok := r.Store.Get(pack)
	if !ok {
		return Credential{}, ErrNotConnected{pack}
	}
	if cred.Kind == "oauth" && !cred.Expiry.IsZero() && time.Until(cred.Expiry) < time.Minute {
		provider, _ := ProviderFor(pack)
		next, err := provider.Refresh(ctx, r.Client, cred)
		if err != nil {
			return Credential{}, err
		}
		if err := r.Store.Put(pack, next); err != nil {
			return Credential{}, err
		}
		return next, nil
	}
	return cred, nil
}

type arguments map[string]any

func (a arguments) str(key string) string {
	s, _ := a[key].(string)
	return strings.TrimSpace(s)
}

func (a arguments) limit() int {
	n, _ := a["limit"].(float64)
	switch {
	case n < 1:
		return 10
	case n > 30:
		return 30
	}
	return int(n)
}

func (a arguments) number(key string) int {
	n, _ := a[key].(float64)
	return int(n)
}

func (a arguments) require(keys ...string) error {
	for _, k := range keys {
		if a[k] == nil || a[k] == "" {
			return fmt.Errorf("%s is required", k)
		}
	}
	return nil
}

// getJSON performs an authenticated GET. Error text is derived from the
// status only, so an upstream body can never echo a credential into a result.
func (r *Runner) getJSON(ctx context.Context, rawURL, token string, headers map[string]string, out any) error {
	body, err := r.get(ctx, rawURL, token, headers)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func (r *Runner) get(ctx context.Context, rawURL, token string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, errors.New("the service could not be reached")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, errors.New("the service response could not be read")
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, errors.New("the connection was rejected (401). Reconnect it from the Tools page")
	case resp.StatusCode == http.StatusForbidden:
		return nil, errors.New("access denied (403): the connected account lacks permission or is rate limited")
	case resp.StatusCode == http.StatusNotFound:
		return nil, errors.New("not found (404)")
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("the service returned status %d", resp.StatusCode)
	}
	return body, nil
}

func truncate(s string) (string, bool) {
	if len(s) <= maxText {
		return s, false
	}
	cut := maxText
	for cut > 0 && s[cut]&0xC0 == 0x80 { // do not split a UTF-8 rune
		cut--
	}
	return s[:cut], true
}

func esc(s string) string { return url.PathEscape(s) }
