package toolpacks

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSlackOAuthPKCEUsesEmployeeTokenAndRotates(t *testing.T) {
	t.Setenv("SLACK_CLIENT_ID", "slack-client")
	t.Setenv("SLACK_CLIENT_SECRET", "private-secret")
	t.Setenv("SLACK_OAUTH_PKCE", "true")
	t.Setenv("SLACK_PUBLIC_URL", "")
	t.Setenv("SLACK_USER_SCOPES", "")
	runner := NewRunner(NewStore(t.TempDir()))
	var challenge string
	exchanges := 0
	runner.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/api/oauth.v2.access":
			exchanges++
			_ = r.ParseForm()
			if r.Form.Get("client_secret") != "" || r.Form.Get("client_id") != "slack-client" {
				t.Error("PKCE exchange leaked the secret or missed the client id")
			}
			if exchanges == 1 {
				verifier := r.Form.Get("code_verifier")
				sum := sha256.Sum256([]byte(verifier))
				if len(verifier) < 43 || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
					t.Error("incorrect PKCE verifier")
				}
				if r.Form.Get("redirect_uri") != "http://localhost:8080/oauth/slack/callback" {
					t.Error("wrong callback")
				}
				body = `{"ok":true,"access_token":"bot-must-not-be-used","token_type":"bot","authed_user":{"id":"U123","access_token":"employee-token","refresh_token":"refresh-1","expires_in":43200,"token_type":"user"}}`
			} else {
				if r.Form.Get("refresh_token") != "refresh-1" || r.Form.Get("grant_type") != "refresh_token" {
					t.Error("wrong refresh request")
				}
				body = `{"ok":true,"access_token":"employee-rotated","refresh_token":"refresh-2","expires_in":43200,"token_type":"user"}`
			}
		case "/api/auth.test":
			if r.Header.Get("Authorization") != "Bearer employee-token" {
				t.Error("bot or incorrect token used")
			}
			body = `{"ok":true,"team":"HackTX","user":"ada","user_id":"U123"}`
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	mux := http.NewServeMux()
	api := NewAPI(runner, "http://127.0.0.1:8080", "http://localhost:3000")
	api.Mount(mux)
	do := func(method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("X-NMC3-User", "alice")
		r.Header.Set("X-NMC3-Tenant", "org")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := do("POST", "/v1/tool-packs/slack/oauth/start", nil)
	var start struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &start)
	if w.Code != 200 || !strings.HasPrefix(start.URL, "http://localhost:8080/") {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	launch, _ := url.Parse(start.URL)
	w = do("GET", launch.RequestURI(), nil)
	provider, _ := url.Parse(w.Header().Get("Location"))
	if provider.Query().Get("scope") != "" || provider.Query().Get("user_scope") != "channels:read,channels:history,search:read.public" || provider.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("must request read-only user scopes with PKCE")
	}
	challenge = provider.Query().Get("code_challenge")
	callback := "/oauth/slack/callback?code=code&state=" + launch.Query().Get("state")
	w = do("GET", callback, w.Result().Cookies()[0])
	if !strings.Contains(w.Header().Get("Location"), "result=connected") {
		t.Fatalf("callback failed: %v", w.Header())
	}
	store := runner.Store.ForOwner("org:alice")
	cred, ok := store.Get("slack")
	if !ok || cred.AccessToken != "employee-token" || !cred.PublicClient || cred.Account != "HackTX · ada" {
		t.Fatal("wrong saved employee credential")
	}
	if _, ok := runner.Store.ForOwner("org:bob").Get("slack"); ok {
		t.Fatal("another employee can use connection")
	}
	cred.Expiry = time.Now().Add(-time.Second)
	_ = store.Put("slack", cred)
	personal := *runner
	personal.Store = store
	rotated, err := personal.credential(context.Background(), "slack")
	if err != nil || rotated.AccessToken != "employee-rotated" || rotated.RefreshToken != "refresh-2" || !rotated.PublicClient {
		t.Fatalf("refresh failed: %v", err)
	}
}

func TestSlackReadOnlyToolsAndPagination(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer employee-token" {
			t.Error("missing token")
		}
		switch r.URL.Path {
		case "/assistant.search.context":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["limit"] != float64(20) || b["cursor"] != "next-page" || b["query"] != "release" || fmt.Sprint(b["channel_types"]) != "[public_channel]" {
				t.Errorf("search: %v", b)
			}
			fmt.Fprint(w, `{"ok":true,"results":{"messages":[{"content":"Release Friday","channel_id":"C123","message_ts":"1700000000.000001","permalink":"https://demo.slack.com/archives/C123/p1700000000000001"}]},"response_metadata":{"next_cursor":"page-3"}}`)
		case "/conversations.list":
			if r.URL.Query().Get("types") != "public_channel" || r.URL.Query().Get("limit") != "30" {
				t.Error("incorrect channel list")
			}
			fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C123","name":"releases","is_private":false}],"response_metadata":{"next_cursor":"page-3"}}`)
		case "/conversations.history", "/conversations.replies":
			q := r.URL.Query()
			if q.Get("channel") != "C123" || q.Get("limit") != "15" {
				t.Errorf("history pagination: %v", q)
			}
			if r.URL.Path == "/conversations.replies" && q.Get("ts") != "1700000000.000001" {
				t.Error("missing parent timestamp")
			}
			fmt.Fprint(w, `{"ok":true,"messages":[{"user":"U123","text":"Release Friday","ts":"1700000000.000001"}],"has_more":true,"response_metadata":{"next_cursor":"page-3"}}`)
		case "/chat.getPermalink":
			fmt.Fprint(w, `{"ok":true,"permalink":"https://demo.slack.com/archives/C123/p1700000000000001"}`)
		default:
			t.Errorf("write or unknown endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	r := NewRunner(NewStore(t.TempDir()))
	r.SlackAPI = srv.URL
	_ = r.Store.Put("slack", Credential{AccessToken: "employee-token"})
	for _, item := range []struct{ tool, args, want string }{
		{"search_messages", `{"query":"release","limit":99,"cursor":"next-page"}`, "page-3"},
		{"list_channels", `{"limit":99}`, "releases"},
		{"get_channel_history", `{"channel_id":"C123","limit":99}`, "Release Friday"},
		{"get_thread", `{"channel_id":"C123","ts":"1700000000.000001","limit":99}`, "Release Friday"},
		{"get_permalink", `{"channel_id":"C123","ts":"1700000000.000001"}`, "https://demo.slack.com"},
	} {
		out, err := r.Call(context.Background(), "slack", item.tool, json.RawMessage(item.args))
		raw, _ := json.Marshal(out)
		if err != nil || !strings.Contains(string(raw), item.want) {
			t.Fatalf("%s: %s %v", item.tool, raw, err)
		}
	}
	before := calls
	for _, item := range []struct{ tool, args string }{{"send_message", `{}`}, {"get_thread", `{"channel_id":"../../users","ts":"1"}`}, {"get_permalink", `{"channel_id":"C123","ts":"invalid"}`}} {
		if _, err := r.Call(context.Background(), "slack", item.tool, json.RawMessage(item.args)); err == nil {
			t.Fatal("invalid action accepted")
		}
	}
	if calls != before {
		t.Fatal("invalid call reached Slack")
	}
}

func TestSlackErrorsAndBotOnlyOAuth(t *testing.T) {
	for _, code := range []string{"missing_scope", "invalid_auth", "not_in_channel", "feature_not_enabled", "private-token-value"} {
		t.Run(code, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"ok":false,"error":%q,"detail":"private-token-value"}`, code)
			}))
			defer srv.Close()
			r := NewRunner(NewStore(t.TempDir()))
			r.SlackAPI = srv.URL
			_, err := r.SlackAccount(context.Background(), "token")
			if err == nil || strings.Contains(err.Error(), "private-token-value") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"access_token":"bot-token","token_type":"bot"}`)
	}))
	defer srv.Close()
	p := Provider{SlackUser: true, TokenURL: srv.URL}
	if _, err := p.Exchange(context.Background(), srv.Client(), "code", "https://example.test/oauth/slack/callback"); err == nil {
		t.Fatal("bot-only token accepted as a personal connection")
	}
}
