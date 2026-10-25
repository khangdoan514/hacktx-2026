package toolpacks

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPersonalOAuthSurvivesRestartAndRejectsReplay(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	runner := NewRunner(NewStore(t.TempDir()))
	providerCalls := 0
	runner.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		providerCalls++
		body := `{"access_token":"private-token","refresh_token":"refresh","expires_in":3600}`
		if r.URL.Path == "/drive/v3/about" {
			if r.Header.Get("Authorization") != "Bearer private-token" {
				t.Fatal("missing bearer")
			}
			body = `{"user":{"emailAddress":"connected@example.com"}}`
		} else if r.URL.Path != "/token" {
			t.Fatalf("unexpected provider path: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}
	makeAPI := func() *http.ServeMux {
		mux := http.NewServeMux()
		NewAPI(runner, "http://127.0.0.1:8080", "http://localhost:3000").Mount(mux)
		return mux
	}
	mux := makeAPI()
	do := func(user, method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if user != "" {
			r.Header.Set("X-NMC3-User", user)
			r.Header.Set("X-NMC3-Tenant", "org")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := do("", "GET", "/v1/tool-packs", nil); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	started := do("alice", "POST", "/v1/tool-packs/gdrive/oauth/start", nil)
	var body struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(started.Body.Bytes(), &body) != nil || started.Code != 200 {
		t.Fatal(started.Body)
	}
	u, _ := url.Parse(body.URL)
	authorized := do("", "GET", u.RequestURI(), nil)
	providerURL, _ := url.Parse(authorized.Header().Get("Location"))
	if providerURL.Query().Get("redirect_uri") != "http://127.0.0.1:8080/oauth/gdrive/callback" || providerURL.Query().Get("client_id") != "client" {
		t.Fatalf("provider URL: %v", providerURL)
	}
	cookies := authorized.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("missing secure browser binding")
	}
	callback := "/oauth/gdrive/callback?state=" + u.Query().Get("state") + "&code=code"
	mux = makeAPI() // Pending login must survive recreating the API/server.
	if w := do("", "GET", callback, nil); !strings.Contains(w.Header().Get("Location"), "invalid_state") {
		t.Fatal("callback accepted without browser cookie")
	}
	if w := do("", "GET", callback, cookies[0]); !strings.Contains(w.Header().Get("Location"), "result=connected") {
		t.Fatalf("callback: %v", w.Header())
	}
	if w := do("", "GET", callback, cookies[0]); !strings.Contains(w.Header().Get("Location"), "invalid_state") {
		t.Fatal("replay accepted")
	}
	for _, user := range []string{"alice", "bob"} {
		w := do(user, "GET", "/v1/tool-packs", nil)
		if strings.Contains(w.Body.String(), "private-token") || strings.Contains(w.Body.String(), "refresh") {
			t.Fatal("credentials leaked")
		}
		connected := strings.Contains(w.Body.String(), "connected@example.com")
		if connected != (user == "alice") {
			t.Fatalf("connection shared with %s: %s", user, w.Body)
		}
	}
	if w := do("bob", "POST", "/v1/tool-packs/gdrive/test", nil); w.Code != 409 {
		t.Fatalf("bob tested alice's connection: %d", w.Code)
	}
	if w := do("alice", "POST", "/v1/tool-packs/gdrive/test", nil); w.Code != 200 {
		t.Fatalf("test: %d %s", w.Code, w.Body)
	}
	do("bob", "POST", "/v1/tool-packs/gdrive/disconnect", nil)
	if _, ok := runner.Store.ForOwner("org:alice").Get("gdrive"); !ok {
		t.Fatal("bob disconnected alice")
	}
	do("alice", "POST", "/v1/tool-packs/gdrive/disconnect", nil)
	if _, ok := runner.Store.ForOwner("org:alice").Get("gdrive"); ok {
		t.Fatal("alice still connected")
	}
	if providerCalls != 3 {
		t.Fatalf("unexpected provider calls: %d", providerCalls)
	}
}

func TestExpiredOAuthState(t *testing.T) {
	a := NewAPI(NewRunner(NewStore(t.TempDir())), "http://127.0.0.1:8080", "http://localhost:3000")
	state := randomState()
	if err := a.saveState(state, pending{Pack: "gdrive", Owner: "alice", Expires: time.Now().Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.readState(state, "gdrive"); err == nil {
		t.Fatal("expired state accepted")
	}
	if _, err := a.readState("../../credentials", "gdrive"); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestMCPNewlineTransport(t *testing.T) {
	runner := NewRunner(NewStore(t.TempDir()))
	input := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{}}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n"
	var out bytes.Buffer
	if err := Serve(strings.NewReader(input), &out, runner); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d replies", len(lines))
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatalf("not a newline-delimited MCP message: %s", line)
		}
	}
}
