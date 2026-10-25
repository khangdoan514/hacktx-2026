package toolpacks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const testNotionID = "11111111-2222-3333-4444-555555555555"

func TestNotionReadToolsAndPermissions(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer private-token" || r.Header.Get("Notion-Version") != notionVersion {
			t.Error("missing API authentication or pinned version")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users/me":
			fmt.Fprint(w, `{"name":"App","bot":{"workspace_name":"Demo workspace"}}`)
		case "/search", "/data_sources/" + testNotionID + "/query":
			if r.Method != "POST" {
				t.Error("wrong method")
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["page_size"] != float64(30) || body["start_cursor"] != "cursor-2" {
				t.Errorf("pagination not bounded: %v", body)
			}
			if r.URL.Path == "/search" && body["query"] != "Project" {
				t.Errorf("query: %v", body)
			}
			fmt.Fprintf(w, `{"results":[{"id":%q,"object":"page","url":"https://www.notion.so/example","properties":{"Name":{"title":[{"plain_text":"Project"}]}}}],"has_more":true,"next_cursor":"cursor-3"}`, testNotionID)
		case "/pages/" + testNotionID:
			fmt.Fprintf(w, `{"id":%q,"object":"page","url":"https://www.notion.so/example","properties":{"Title":{"title":[{"plain_text":"Project"}]}}}`, testNotionID)
		case "/databases/" + testNotionID:
			fmt.Fprintf(w, `{"id":%q,"object":"database","data_sources":[{"id":%q,"name":"Tasks"}]}`, testNotionID, testNotionID)
		case "/blocks/" + testNotionID + "/children":
			if r.URL.Query().Get("page_size") != "30" {
				t.Error("unbounded blocks")
			}
			fmt.Fprintf(w, `{"results":[{"id":%q,"type":"paragraph","has_children":false,"paragraph":{"rich_text":[{"plain_text":"Project notes"}]}}],"has_more":false,"next_cursor":null}`, testNotionID)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	r := NewRunner(NewStore(t.TempDir()))
	r.NotionAPI = srv.URL
	if err := r.Store.Put("notion", Credential{Kind: "oauth", AccessToken: "private-token"}); err != nil {
		t.Fatal(err)
	}
	account, err := r.NotionAccount(context.Background(), "private-token")
	if err != nil || account != "Demo workspace" {
		t.Fatalf("account=%s err=%v", account, err)
	}
	for _, c := range []struct{ tool, args, want string }{
		{"search", `{"query":"Project","limit":999,"cursor":"cursor-2"}`, "cursor-3"},
		{"get_page", `{"page_id":"` + testNotionID + `"}`, "https://www.notion.so/example"},
		{"list_blocks", `{"block_id":"` + testNotionID + `","limit":999}`, "Project notes"},
		{"get_database", `{"database_id":"` + testNotionID + `"}`, "Tasks"},
		{"query_data_source", `{"data_source_id":"` + testNotionID + `","limit":999,"cursor":"cursor-2"}`, "cursor-3"},
	} {
		out, err := r.Call(context.Background(), "notion", c.tool, json.RawMessage(c.args))
		raw, _ := json.Marshal(out)
		if err != nil || !strings.Contains(string(raw), c.want) {
			t.Fatalf("%s: %s %v", c.tool, raw, err)
		}
	}
	before := calls
	if _, err := r.Call(context.Background(), "notion", "get_page", json.RawMessage(`{"page_id":"../../users/me"}`)); err == nil {
		t.Fatal("path injection accepted")
	}
	if _, err := r.Call(context.Background(), "notion", "delete_page", json.RawMessage(`{}`)); err == nil {
		t.Fatal("write accepted")
	}
	if calls != before {
		t.Fatal("invalid request reached provider")
	}
}

func TestNotionOAuthJSONBasicAndRefresh(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		id, secret, ok := r.BasicAuth()
		if !ok || id != "notion-id" || secret != "private-secret" || r.Header.Get("Content-Type") != "application/json" {
			t.Fatal("Notion must use Basic auth and JSON")
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["client_secret"] != "" || body["client_id"] != "" {
			t.Fatal("credentials duplicated in JSON")
		}
		if requests == 1 {
			if body["redirect_uri"] != "http://localhost:8080/oauth/notion/callback" || body["grant_type"] != "authorization_code" {
				t.Fatalf("exchange body: %v", body)
			}
		} else if body["refresh_token"] != "refresh-1" || body["grant_type"] != "refresh_token" {
			t.Fatalf("refresh body: %v", body)
		}
		fmt.Fprintf(w, `{"access_token":"access-%d","refresh_token":"refresh-%d","expires_in":3600}`, requests, requests)
	}))
	defer srv.Close()
	t.Setenv("NOTION_CLIENT_ID", "notion-id")
	t.Setenv("NOTION_CLIENT_SECRET", "private-secret")
	t.Setenv("NOTION_PUBLIC_URL", "")
	p, ok := ProviderFor("notion")
	if !ok {
		t.Fatal("provider missing")
	}
	p.TokenURL = srv.URL
	a := NewAPI(NewRunner(NewStore(t.TempDir())), "http://127.0.0.1:8080", "http://localhost:3000")
	redirect := a.redirectURI("notion")
	if redirect != "http://localhost:8080/oauth/notion/callback" {
		t.Fatal(redirect)
	}
	u, _ := url.Parse(p.AuthorizeURL(redirect, "state"))
	if u.Query().Get("owner") != "user" || u.Query().Get("response_type") != "code" || u.Query().Has("scope") {
		t.Fatal("wrong Notion authorization parameters")
	}
	old, err := p.Exchange(context.Background(), srv.Client(), "code", redirect)
	if err != nil || old.AccessToken != "access-1" {
		t.Fatalf("exchange: %v", err)
	}
	old.Account = "Demo"
	next, err := p.Refresh(context.Background(), srv.Client(), old)
	if err != nil || next.RefreshToken != "refresh-2" || next.Account != "Demo" {
		t.Fatalf("refresh: %v", err)
	}
}

func TestNotionErrorsDoNotExposeProviderBody(t *testing.T) {
	for _, status := range []int{401, 403, 404, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"message":"private-token-must-not-leak"}`)
			}))
			defer srv.Close()
			r := NewRunner(NewStore(t.TempDir()))
			r.NotionAPI = srv.URL
			_, err := r.NotionAccount(context.Background(), "token")
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("unsafe error: %v", err)
			}
		})
	}
}
