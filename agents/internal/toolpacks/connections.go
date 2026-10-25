package toolpacks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type ownerKey struct{}

// Owner identifies the employee and organization asserted by the guarded proxy.
func Owner(r *http.Request) string {
	user, tenant := strings.TrimSpace(r.Header.Get(platform.UserHeader)), strings.TrimSpace(r.Header.Get(platform.TenantHeader))
	if user == "" || tenant == "" {
		return ""
	}
	return tenant + ":" + user
}

func WithOwner(ctx context.Context, owner string) context.Context {
	return context.WithValue(ctx, ownerKey{}, owner)
}
func ContextOwner(ctx context.Context) string {
	owner, _ := ctx.Value(ownerKey{}).(string)
	return owner
}

func (a *API) personal(r *http.Request) *Runner {
	runner := *a.Runner
	runner.Store = a.Runner.Store.ForOwner(Owner(r))
	return &runner
}

func (a *API) signedIn(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if Owner(r) == "" {
			writeError(w, http.StatusUnauthorized, "Sign in to connect your accounts.")
			return
		}
		next(w, r)
	}
}

type pending struct {
	Pack    string    `json:"pack"`
	Owner   string    `json:"owner"`
	Browser string    `json:"browser"`
	Expires time.Time `json:"expires"`
}

var statePattern = regexp.MustCompile(`^[a-f0-9]{48}$`)

func (a *API) statePath(state string) string {
	return filepath.Join(filepath.Dir(a.Runner.Store.path), "oauth-states", state+".json")
}

func (a *API) saveState(state string, p pending) error {
	dir := filepath.Dir(a.statePath(state))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > 15*time.Minute {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(a.statePath(state), b, 0o600)
}

func (a *API) readState(state, pack string) (pending, error) {
	if !statePattern.MatchString(state) {
		return pending{}, errors.New("invalid state")
	}
	b, err := os.ReadFile(a.statePath(state))
	if err != nil {
		return pending{}, err
	}
	var p pending
	if json.Unmarshal(b, &p) != nil || p.Pack != pack || p.Owner == "" || time.Now().After(p.Expires) {
		return pending{}, errors.New("expired state")
	}
	return p, nil
}

// The browser first visits the backend so its callback cookie has the same
// origin even when the frontend and callback use different loopback hosts.
func (a *API) oauthAuthorize(w http.ResponseWriter, r *http.Request) {
	state, id := r.URL.Query().Get("state"), r.PathValue("id")
	p, err := a.readState(state, id)
	if err != nil {
		http.Error(w, "This sign-in link expired. Start again from Tools.", http.StatusBadRequest)
		return
	}
	prov, ok := ProviderFor(id)
	if !ok || !prov.Configured() {
		http.Error(w, "Sign-in is unavailable.", http.StatusConflict)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "nmc3_oauth_" + state, Value: p.Browser, Path: "/oauth/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: strings.HasPrefix(a.origin(id), "https://"), MaxAge: 600})
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, prov.AuthorizeURL(a.redirectURI(id), state), http.StatusSeeOther)
}

func (a *API) testConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := Find(id); !ok {
		writeError(w, http.StatusNotFound, "Unknown connection.")
		return
	}
	runner := a.personal(r)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	cred, err := runner.credential(ctx, id)
	if err != nil {
		writeError(w, http.StatusConflict, "Reconnect this account to test access.")
		return
	}
	account, err := runner.account(ctx, id, cred.AccessToken)
	if err != nil {
		writeError(w, http.StatusBadGateway, "The provider could not verify this connection. Try reconnecting your account.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "account": account, "message": "Account access verified. No documents were read."})
}
