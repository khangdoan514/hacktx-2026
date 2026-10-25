package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/khangdoan514/hacktx-2026/agents/internal/toolpacks"
)

func TestEmployeeSessionsUseSeparateCredentialStores(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	source := filepath.Join(filepath.Dir(file), "..", "..")
	base := t.TempDir()
	t.Setenv("NMC3_TOOLS_DIR", base)
	locations := map[string]string{}
	for _, owner := range []string{"org:alice", "org:bob"} {
		dir, err := connectionProject(source, owner)
		if err != nil {
			t.Fatal(err)
		}
		locations[owner] = dir
		raw, err := os.ReadFile(filepath.Join(dir, "opencode.jsonc"))
		if err != nil {
			t.Fatal(err)
		}
		var config struct {
			MCP struct {
				Servers map[string]struct {
					CWD         string            `json:"cwd"`
					Environment map[string]string `json:"environment"`
					Command     []string          `json:"command"`
				} `json:"servers"`
			} `json:"mcp"`
		}
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatal(err)
		}
		tools := config.MCP.Servers["tools"]
		if tools.Environment["NMC3_TOOLS_DIR"] != toolpacks.NewStore(base).ForOwner(owner).Directory() {
			t.Fatal("wrong credential store")
		}
		if !filepath.IsAbs(tools.CWD) || !filepath.IsAbs(tools.Command[0]) {
			t.Fatal("MCP paths must be absolute")
		}
	}
	if locations["org:alice"] == locations["org:bob"] {
		t.Fatal("employees share an MCP location")
	}
}

func TestThreadOwnership(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"metadata":{"nmc3_owner":"org:alice"}}}`))
	}))
	defer srv.Close()
	c, err := NewClient(srv.URL, "password", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AuthorizeSession(toolpacks.WithOwner(context.Background(), "org:alice"), "ses_1"); err != nil {
		t.Fatal(err)
	}
	if err := c.AuthorizeSession(toolpacks.WithOwner(context.Background(), "org:bob"), "ses_1"); err == nil {
		t.Fatal("another employee can access the thread")
	}
}
