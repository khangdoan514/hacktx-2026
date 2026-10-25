package toolpacks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
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

	mu     sync.Mutex
	states map[string]pending
}

type pending struct {
	pack    string
	expires time.Time
}

func NewAPI(runner *Runner, publicURL, appURL string) *API {
	return &API{Runner: runner, PublicURL: strings.TrimRight(publicURL, "/"), AppURL: strings.TrimRight(appURL, "/"), states: map[string]pending{}}
}

// Mount registers /v1/tool-packs and the OAuth callback. The callback must be
// reachable without the console's bearer token (it is a browser redirect from
// the provider); the state value ties it to a start request.
func (a *API) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/tool-packs", a.list)
	mux.HandleFunc("POST /v1/tool-packs/{id}/connect", a.connect)
	mux.HandleFunc("POST /v1/tool-packs/{id}/oauth/start", a.oauthStart)
	mux.HandleFunc("POST /v1/tool-packs/{id}/disconnect", a.disconnect)
	mux.HandleFunc("GET /oauth/{id}/callback", a.oauthCallback)
}

// IsCallback reports whether a request path is the unauthenticated redirect.
func IsCallback(path string) bool {
	return strings.HasPrefix(path, "/oauth/") && strings.HasSuffix(path, "/callback")
}

type toolView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SideEffect  string `json:"side_effect"`
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

func (a *API) list(w http.ResponseWriter, _ *http.Request) {
	views := make([]packView, 0)
	for _, p := range Packs() {
		prov, _ := ProviderFor(p.ID)
		v := packView{ID: p.ID, Name: p.Name, Description: p.Description, TokenAccepted: p.TokenHint != "", TokenHint: p.TokenHint, OAuthAvailable: prov.Configured(), Tools: []toolView{}}
		if cred, ok := a.Runner.Store.Get(p.ID); ok {
			t := cred.ConnectedAt
			v.Connected, v.Account, v.ConnectedAt, v.Method = true, cred.Account, &t, cred.Kind
		}
		for _, t := range p.Tools {
			v.Tools = append(v.Tools, toolView{ID: p.ToolID(t), Name: t.Name, Description: t.Description, SideEffect: "READ_ONLY"})
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
	if err := a.Runner.Store.Put(pack.ID, Credential{Kind: "token", AccessToken: token, Account: account}); err != nil {
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
	a.mu.Lock()
	for k, v := range a.states { // drop expired starts
		if time.Now().After(v.expires) {
			delete(a.states, k)
		}
	}
	a.states[state] = pending{pack: id, expires: time.Now().Add(10 * time.Minute)}
	a.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"url": prov.AuthorizeURL(a.redirectURI(id), state)})
}

func (a *API) oauthCallback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	back := func(result string) {
		target := a.AppURL + "/tools?" + url.Values{"pack": {id}, "result": {result}}.Encode()
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
	a.mu.Lock()
	p, ok := a.states[r.URL.Query().Get("state")]
	delete(a.states, r.URL.Query().Get("state")) // single use
	a.mu.Unlock()
	if !ok || p.pack != id || time.Now().After(p.expires) {
		back("invalid_state")
		return
	}
	code := r.URL.Query().Get("code")
	prov, known := ProviderFor(id)
	if r.URL.Query().Get("error") != "" || code == "" || !known {
		back("denied")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	cred, err := prov.Exchange(ctx, a.Runner.Client, code, a.redirectURI(id))
	if err != nil {
		back("exchange_failed")
		return
	}
	if id == "github" {
		cred.Account, err = a.Runner.GitHubLogin(ctx, cred.AccessToken)
	} else {
		cred.Account, err = a.Runner.DriveAccount(ctx, cred.AccessToken)
	}
	if err != nil {
		back("verify_failed")
		return
	}
	if err := a.Runner.Store.Put(id, cred); err != nil {
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
	if err := a.Runner.Store.Delete(r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, "the connection could not be removed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"connected": false})
}

func (a *API) redirectURI(id string) string { return a.PublicURL + "/oauth/" + id + "/callback" }

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
