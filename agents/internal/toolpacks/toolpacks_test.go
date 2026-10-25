package toolpacks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func stub(t *testing.T) (*Runner, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghp_goodtoken" {
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"login":"octo"}`)
	})
	mux.HandleFunc("/search/code", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "foo repo:o/r" || r.URL.Query().Get("per_page") != "10" {
			http.Error(w, r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"total_count":1,"items":[{"path":"a.go","html_url":"https://x/a.go","repository":{"full_name":"o/r"}}]}`)
	})
	mux.HandleFunc("/repos/o/r/contents/dir/ab.go", func(w http.ResponseWriter, r *http.Request) {
		enc := base64.StdEncoding.EncodeToString([]byte("package a\n"))
		fmt.Fprintf(w, `{"type":"file","path":"dir/ab.go","size":10,"encoding":"base64","content":%q,"html_url":"u"}`, enc[:4]+"\n"+enc[4:])
	})
	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"number":1,"title":"bug","state":"open","html_url":"u1","user":{"login":"a"}},{"number":2,"title":"a pr","state":"open","html_url":"u2","user":{"login":"b"},"pull_request":{}}]`)
	})
	mux.HandleFunc("/drive/files", func(w http.ResponseWriter, r *http.Request) {
		if want := "fullText contains 'it\\'s' and trashed = false"; r.URL.Query().Get("q") != want {
			http.Error(w, r.URL.Query().Get("q"), http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"files":[{"id":"f1","name":"Plan","mimeType":"application/vnd.google-apps.document","webViewLink":"w"}]}`)
	})
	mux.HandleFunc("/drive/files/f1", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"f1","name":"Plan","mimeType":"application/vnd.google-apps.document","webViewLink":"w"}`)
	})
	mux.HandleFunc("/drive/files/f1/export", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mimeType") != "text/plain" {
			http.Error(w, "mime", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "the plan")
	})
	mux.HandleFunc("/drive/files/pdf", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"pdf","name":"x.pdf","mimeType":"application/pdf"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	r := NewRunner(NewStore(t.TempDir()))
	r.GitHubAPI, r.DriveAPI = srv.URL, srv.URL+"/drive"
	return r, srv
}

func call(t *testing.T, r *Runner, pack, tool, args string) (map[string]any, error) {
	t.Helper()
	out, err := r.Call(context.Background(), pack, tool, json.RawMessage(args))
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(out)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m, nil
}

func TestNotConnected(t *testing.T) {
	r, _ := stub(t)
	_, err := call(t, r, "github", "search_code", `{"query":"x"}`)
	if _, ok := err.(ErrNotConnected); !ok {
		t.Fatalf("err=%v", err)
	}
}

func TestGitHubTools(t *testing.T) {
	r, _ := stub(t)
	_ = r.Store.Put("github", Credential{Kind: "token", AccessToken: "ghp_goodtoken"})
	m, err := call(t, r, "github", "search_code", `{"query":"foo repo:o/r"}`)
	if err != nil || m["total"].(float64) != 1 {
		t.Fatalf("%v %v", m, err)
	}
	m, err = call(t, r, "github", "get_file", `{"owner":"o","repo":"r","path":"dir/ab.go"}`)
	if err != nil || m["content"] != "package a\n" {
		t.Fatalf("%v %v", m, err)
	}
	m, err = call(t, r, "github", "list_issues", `{"owner":"o","repo":"r"}`)
	if issues := m["issues"].([]any); err != nil || len(issues) != 1 {
		t.Fatalf("pull requests must be filtered: %v %v", m, err)
	}
	if _, err = call(t, r, "github", "get_file", `{"owner":"o"}`); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("err=%v", err)
	}
}

func TestBadTokenIsRejectedWithoutEchoing(t *testing.T) {
	r, _ := stub(t)
	_ = r.Store.Put("github", Credential{Kind: "token", AccessToken: "ghp_secretvalue"})
	_, err := call(t, r, "github", "search_code", `{"query":"x"}`)
	if err == nil || strings.Contains(err.Error(), "ghp_secretvalue") {
		t.Fatalf("err=%v", err)
	}
}

func TestDriveTools(t *testing.T) {
	r, _ := stub(t)
	_ = r.Store.Put("gdrive", Credential{Kind: "token", AccessToken: "ya29.tokenvalue"})
	m, err := call(t, r, "gdrive", "search_files", `{"query":"it's"}`)
	if err != nil || len(m["files"].([]any)) != 1 {
		t.Fatalf("%v %v", m, err)
	}
	m, err = call(t, r, "gdrive", "read_file", `{"file_id":"f1"}`)
	if err != nil || m["content"] != "the plan" {
		t.Fatalf("%v %v", m, err)
	}
	if _, err = call(t, r, "gdrive", "read_file", `{"file_id":"pdf"}`); err == nil {
		t.Fatal("binary files must be refused")
	}
}

func TestExpiredOAuthIsRefreshed(t *testing.T) {
	var refreshed bool
	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = req.ParseForm()
		refreshed = req.Form.Get("refresh_token") == "rt" && req.Form.Get("client_id") == "cid"
		fmt.Fprint(w, `{"access_token":"new-access-token","expires_in":3600}`)
	}))
	defer token.Close()
	t.Setenv("GOOGLE_CLIENT_ID", "x")
	r, _ := stub(t)
	_ = r.Store.Put("gdrive", Credential{Kind: "oauth", AccessToken: "old", RefreshToken: "rt", ClientID: "cid", ClientSecret: "cs", Expiry: time.Now().Add(-time.Hour)})
	prov, _ := ProviderFor("gdrive")
	prov.TokenURL = token.URL
	next, err := prov.Refresh(context.Background(), r.Client, mustGet(t, r.Store, "gdrive"))
	if err != nil || !refreshed || next.AccessToken != "new-access-token" || next.RefreshToken != "rt" {
		t.Fatalf("%+v %v refreshed=%v", next, err, refreshed)
	}
}

func mustGet(t *testing.T, s *Store, pack string) Credential {
	c, ok := s.Get(pack)
	if !ok {
		t.Fatal("missing credential")
	}
	return c
}

func TestStoreFilePermissions(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Put("github", Credential{Kind: "token", AccessToken: "abcdefgh"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode(), err)
	}
	_ = s.Delete("github")
	if _, ok := s.Get("github"); ok {
		t.Fatal("not deleted")
	}
}

func TestAPIConnectListAndOAuthState(t *testing.T) {
	r, _ := stub(t)
	t.Setenv("GITHUB_CLIENT_ID", "id")
	t.Setenv("GITHUB_CLIENT_SECRET", "secret")
	api := NewAPI(r, "http://127.0.0.1:8080", "http://localhost:3000")
	mux := http.NewServeMux()
	api.Mount(mux)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	if w := do("POST", "/v1/tool-packs/github/connect", `{"token":"ghp_badtoken1"}`); w.Code != 400 {
		t.Fatalf("bad token status=%d", w.Code)
	}
	if w := do("POST", "/v1/tool-packs/github/connect", `{"token":"ghp_goodtoken"}`); w.Code != 200 {
		t.Fatalf("status=%d %s", w.Code, w.Body)
	}
	w := do("GET", "/v1/tool-packs", "")
	if strings.Contains(w.Body.String(), "ghp_goodtoken") || !strings.Contains(w.Body.String(), `"account":"octo"`) {
		t.Fatalf("list leaks or misses: %s", w.Body)
	}
	if w := do("POST", "/v1/tool-packs/gdrive/connect", `{"token":"ya29.whatever"}`); w.Code != 400 {
		t.Fatalf("drive must be OAuth only, status=%d", w.Code)
	}
	w = do("POST", "/v1/tool-packs/github/oauth/start", "")
	var started struct{ URL string }
	_ = json.Unmarshal(w.Body.Bytes(), &started)
	if w.Code != 200 || !strings.Contains(started.URL, "client_id=id") || strings.Contains(started.URL, "secret") {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	if w := do("GET", "/oauth/github/callback?code=c&state=forged", ""); !strings.Contains(w.Header().Get("Location"), "result=invalid_state") {
		t.Fatalf("forged state accepted: %v", w.Header())
	}
	do("POST", "/v1/tool-packs/github/disconnect", "")
	if _, ok := r.Store.Get("github"); ok {
		t.Fatal("still connected")
	}
}

func TestServeSpeaksMCP(t *testing.T) {
	r, _ := stub(t)
	_ = r.Store.Put("github", Credential{Kind: "token", AccessToken: "ghp_goodtoken"})
	frame := func(s string) string { return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(s), s) }
	in := frame(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`) +
		frame(`{"jsonrpc":"2.0","method":"notifications/initialized"}`) +
		frame(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`) +
		frame(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"github_search_code","arguments":{"query":"foo repo:o/r"}}}`) +
		frame(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"gdrive_search_files","arguments":{"query":"x"}}}`)
	var out bytes.Buffer
	if err := Serve(strings.NewReader(in), &out, r); err != nil {
		t.Fatal(err)
	}
	rd := bufio.NewReader(&out)
	var replies []map[string]any
	for {
		body, err := readFrame(rd)
		if err != nil {
			break
		}
		var m map[string]any
		_ = json.Unmarshal(body, &m)
		replies = append(replies, m)
	}
	if len(replies) != 4 {
		t.Fatalf("want 4 replies (the notification gets none), got %d", len(replies))
	}
	tools := replies[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 13 {
		t.Fatalf("tools=%d", len(tools))
	}
	if replies[2]["result"].(map[string]any)["isError"] != false {
		t.Fatalf("github call: %v", replies[2])
	}
	last := replies[3]["result"].(map[string]any)
	if last["isError"] != true || !strings.Contains(fmt.Sprint(last["content"]), "not connected") {
		t.Fatalf("drive should report not connected: %v", last)
	}
}
