package toolpacks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"
)

// API is the console's view of the catalog: list packs, connect with a token
// or OAuth, disconnect. Credentials are write-only through this API.
type API struct {
	Runner *Runner
	// PublicURL is where the browser reaches this process; the OAuth redirect
	// URI is PublicURL + /oauth/<pack>/callback. AppURL is where the console
	// lives, for the post-consent redirect.
	PublicURL string
	AppURL    string
}

func NewAPI(runner *Runner, publicURL, appURL string) *API {
	return &API{Runner: runner, PublicURL: strings.TrimRight(publicURL, "/"), AppURL: strings.TrimRight(appURL, "/")}
}

// Mount registers /v1/tool-packs and the OAuth callback. The callback must be
// reachable without the console's bearer token (it is a browser redirect from
// the provider); the state value ties it to a start request.
func (a *API) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/tool-packs", a.signedIn(a.list))
	mux.HandleFunc("POST /v1/tool-packs/{id}/connect", a.signedIn(a.connect))
	mux.HandleFunc("POST /v1/tool-packs/{id}/oauth/start", a.signedIn(a.oauthStart))
	mux.HandleFunc("POST /v1/tool-packs/{id}/disconnect", a.signedIn(a.disconnect))
	mux.HandleFunc("POST /v1/tool-packs/{id}/test", a.signedIn(a.testConnection))
	mux.HandleFunc("GET /oauth/{id}/authorize", a.oauthAuthorize)
	mux.HandleFunc("GET /oauth/{id}/callback", a.oauthCallback)
}

// IsCallback reports whether a request path is the unauthenticated redirect.
func IsCallback(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "oauth" {
		return false
	}
	_, known := Find(parts[1])
	return known && (parts[2] == "callback" || parts[2] == "authorize")
}

type toolView struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	SideEffect  string    `json:"side_effect"`
	Arguments   []argView `json:"arguments"`
}

// argView describes one tool argument so the builder can ask for a fixed value.
type argView struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

func argViews(t Tool) []argView {
	out := make([]argView, 0, len(t.Properties))
	for name, p := range t.Properties {
		prop, _ := p.(map[string]any)
		typ, _ := prop["type"].(string)
		desc, _ := prop["description"].(string)
		out = append(out, argView{Name: name, Type: typ, Description: desc, Required: slices.Contains(t.Required, name)})
	}
	// Required first, then by name, so the form reads the same every time.
	slices.SortFunc(out, func(a, b argView) int {
		if a.Required != b.Required {
			if a.Required {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

type packView struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	Connected      bool       `json:"connected"`
	Account        string     `json:"account,omitempty"`
	ConnectedAt    *time.Time `json:"connected_at,omitempty"`
	Method         string     `json:"method,omitempty"`
	TokenAccepted  bool       `json:"token_accepted"`
	TokenHint      string     `json:"token_hint,omitempty"`
	OAuthAvailable bool       `json:"oauth_available"`
	Tools          []toolView `json:"tools"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	runner := a.personal(r)
	views := make([]packView, 0)
	for _, p := range Packs() {
		prov, _ := ProviderFor(p.ID)
		v := packView{ID: p.ID, Name: p.Name, Description: p.Description, TokenAccepted: p.TokenHint != "", TokenHint: p.TokenHint, OAuthAvailable: prov.Configured(), Tools: []toolView{}}
		if cred, ok := runner.Store.Get(p.ID); ok {
			t := cred.ConnectedAt
			v.Connected, v.Account, v.ConnectedAt, v.Method = true, cred.Account, &t, cred.Kind
		}
		for _, t := range p.Tools {
			v.Tools = append(v.Tools, toolView{ID: p.ToolID(t), Name: t.Name, Description: t.Description, SideEffect: "READ_ONLY", Arguments: argViews(t)})
		}
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"packs": views})
}

func (a *API) connect(w http.ResponseWriter, r *http.Request) {
	pack, ok := Find(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown tool pack")
		return
	}
	if pack.TokenHint == "" {
		writeError(w, http.StatusBadRequest, pack.Name+" connects with sign-in, not a pasted token")
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	token := strings.TrimSpace(body.Token)
	if len(token) < 8 || strings.ContainsAny(token, "\r\n \t") {
		writeError(w, http.StatusBadRequest, "token is not valid")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	account, err := a.Runner.GitHubLogin(ctx, token)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the token was rejected: "+err.Error())
		return
	}
	if err := a.personal(r).Store.Put(pack.ID, Credential{Kind: "token", AccessToken: token, Account: account}); err != nil {
		writeError(w, http.StatusInternalServerError, "the connection could not be saved")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": true, "account": account})
}

func (a *API) oauthStart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	prov, ok := ProviderFor(id)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown tool pack")
		return
	}
	if !prov.Configured() {
		writeError(w, http.StatusConflict, "sign-in is not configured on this server: set the OAuth client id and secret for this provider")
		return
	}
	state := randomState()
	attempt := pending{Pack: id, Owner: Owner(r), Browser: randomState(), Expires: time.Now().Add(10 * time.Minute)}
	if prov.PKCE {
		attempt.Verifier = randomState()
	}
	if id == "slack" {
		u, err := url.Parse(a.origin(id))
		if err != nil || (u.Scheme != "https" && !(prov.PKCE && u.Scheme == "http" && u.Hostname() == "localhost")) {
			writeError(w, http.StatusConflict, "Slack requires HTTPS, or localhost with PKCE enabled in the Slack app.")
			return
		}
	}
	if err := a.saveState(state, attempt); err != nil {
		writeError(w, http.StatusInternalServerError, "Sign-in could not be started.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": a.origin(id) + "/oauth/" + id + "/authorize?state=" + state})
}

func (a *API) oauthCallback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	back := func(result string) {
		target := a.AppURL + "/tools?" + url.Values{"pack": {id}, "result": {result}}.Encode()
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	state := r.URL.Query().Get("state")
	p, stateErr := a.readState(state, id)
	cookie, cookieErr := r.Cookie("nmc3_oauth_" + state)
	if stateErr != nil || cookieErr != nil || cookie.Value != p.Browser {
		back("invalid_state")
		return
	}
	// Claim atomically so callback replay cannot exchange another code.
	claimed := a.statePath(state) + ".claimed"
	if os.Rename(a.statePath(state), claimed) != nil {
		back("invalid_state")
		return
	}
	defer os.Remove(claimed)
	http.SetCookie(w, &http.Cookie{Name: "nmc3_oauth_" + state, Value: "", Path: "/oauth/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1, Secure: strings.HasPrefix(a.origin(id), "https://")})
	code := r.URL.Query().Get("code")
	prov, known := ProviderFor(id)
	if r.URL.Query().Get("error") != "" || code == "" || !known {
		back("denied")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	cred, err := prov.ExchangeWithVerifier(ctx, a.Runner.Client, code, a.redirectURI(id), p.Verifier)
	if err != nil {
		back("exchange_failed")
		return
	}
	cred.Account, err = a.Runner.account(ctx, id, cred.AccessToken)
	if err != nil {
		back("verify_failed")
		return
	}
	if err := a.Runner.Store.ForOwner(p.Owner).Put(id, cred); err != nil {
		back("save_failed")
		return
	}
	back("connected")
}

func (a *API) disconnect(w http.ResponseWriter, r *http.Request) {
	if _, ok := Find(r.PathValue("id")); !ok {
		writeError(w, http.StatusNotFound, "unknown tool pack")
		return
	}
	if err := a.personal(r).Store.Delete(r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, "the connection could not be removed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"connected": false})
}

func (a *API) origin(id string) string {
	if id == "notion" || id == "slack" {
		if configured := strings.TrimRight(os.Getenv(strings.ToUpper(id)+"_PUBLIC_URL"), "/"); configured != "" {
			return configured
		}
		u, err := url.Parse(a.PublicURL)
		if err == nil && u.Hostname() == "127.0.0.1" {
			port := u.Port()
			u.Host = "localhost"
			if port != "" {
				u.Host += ":" + port
			}
			return strings.TrimRight(u.String(), "/")
		}
	}
	return a.PublicURL
}

func (a *API) redirectURI(id string) string { return a.origin(id) + "/oauth/" + id + "/callback" }

func randomState() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"message": message}})
}
