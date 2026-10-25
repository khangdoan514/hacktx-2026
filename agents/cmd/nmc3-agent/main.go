package main

// Execution backend: OpenCode (see agents/docs/opencode-adoption.md). The
// previous custom engine was removed; its implementation remains in git
// history. Run submission creates an OpenCode session; run history stays in
// Supabase and remains inspectable after restarts.

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	defreg "github.com/khangdoan514/hacktx-2026/agents/internal/definitions"
	"github.com/khangdoan514/hacktx-2026/agents/internal/extensions"
	"github.com/khangdoan514/hacktx-2026/agents/internal/httpapi"
	"github.com/khangdoan514/hacktx-2026/agents/internal/opencode"
	"github.com/khangdoan514/hacktx-2026/agents/internal/policy"
	"github.com/khangdoan514/hacktx-2026/agents/internal/store"
	"github.com/khangdoan514/hacktx-2026/agents/internal/toolpacks"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var runs contracts.RunStore
	var events contracts.EventSink
	if os.Getenv("NMC3_STORE") == "postgres" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s, err := store.Open(ctx, os.Getenv("DATABASE_URL"))
		if err != nil {
			return fmt.Errorf("connect to Postgres: %w", err)
		}
		defer s.Close()
		runs, events = s, s
	} else {
		// The Supabase project ref is fixed for this deployment so a local
		// checkout works with only the server-side secret configured.
		// Agents/.env (gitignored) provides SUPABASE_SECRET_KEY; the browser
		// never receives it. Direct Postgres remains via NMC3_STORE=postgres.
		supabaseURL := envDefault("SUPABASE_URL", "https://vocvwmiypifqnncxhfpj.supabase.co")
		s, err := store.NewSupabase(supabaseURL, os.Getenv("SUPABASE_SECRET_KEY"), nil)
		if err != nil {
			return err
		}
		runs, events = s, s
	}
	// Definitions feed GET /v1/agents and the OpenCode session prompts.
	definitions := []contracts.AgentDefinition{}
	if err := readJSON(envDefault("NMC3_DEFINITIONS_FILE", "examples/definitions.json"), &definitions); err != nil {
		return err
	}
	opencodeURL := envDefault("OPENCODE_URL", "http://127.0.0.1:49374")
	password, err := opencode.ResolvePassword()
	if err != nil {
		return err
	}
	backend, err := opencode.NewClient(opencodeURL, password, nil)
	if err != nil {
		return err
	}
	model := envDefault("OPENCODE_MODEL", "meta/muse-spark-1.3-contributor")
	// The session directory must be this module so OpenCode loads
	// opencode.jsonc (deny-by-default agents and the catalog MCP server).
	// A temp directory would fall through to the unrestricted built-in agent.
	workdir := envDefault("NMC3_WORKDIR", ".")
	if _, err := os.Stat(filepath.Join(workdir, "opencode.jsonc")); err != nil {
		return fmt.Errorf("OpenCode config %s: run from agents/ or set NMC3_WORKDIR to that directory", filepath.Join(workdir, "opencode.jsonc"))
	}
	coordinator, err := opencode.New(definitions, backend, runs, events, workdir, model)
	if err != nil {
		return err
	}
	addr := envDefault("NMC3_ADDR", "127.0.0.1:8080")
	mux := http.NewServeMux()
	mux.Handle("/", httpapi.New(coordinator, runs, events, definitions))
	reg := extensions.NewRegistry()
	gw := extensions.NewGateway(reg, extensions.NewMemoryCredentials(), extensions.Options{})
	extensions.NewAPI(reg, gw).Mount(mux)
	// Built-in GitHub and Google Drive packs. The tools-mcp server named in
	// opencode.jsonc reads the same credential directory.
	toolpacks.NewAPI(
		toolpacks.NewRunner(toolpacks.NewStore(envDefault("NMC3_TOOLS_DIR", ".nmc3"))),
		envDefault("NMC3_PUBLIC_URL", "http://127.0.0.1"+portSuffix(addr)),
		envDefault("NMC3_APP_URL", "http://localhost:3000"),
	).Mount(mux)
	closePlatform, err := mountPlatform(mux, platformDatabaseURL(), envDefault("NMC3_DEFINITIONS_FILE", "examples/definitions.json"))
	if err != nil {
		return err
	}
	defer closePlatform()
	server := http.Server{Addr: addr, Handler: guardInternal(mux), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("NMC3 API listening on %s (OpenCode backend %s)", addr, opencodeURL)
	return server.ListenAndServe()
}

// portSuffix turns a listen address like 127.0.0.1:8080 into ":8080".
func portSuffix(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i:]
	}
	return ""
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// platformDatabaseURL is the dedicated agent database. Run history may still
// use Supabase. The business API database is not a fallback: it has a
// different nmc3 schema.
func platformDatabaseURL() string {
	if v := os.Getenv("NMC3_DATABASE_URL"); v != "" {
		return v
	}
	if os.Getenv("NMC3_STORE") == "postgres" {
		return os.Getenv("DATABASE_URL")
	}
	return ""
}

func mountPlatform(mux *http.ServeMux, databaseURL, definitionsFile string) (func(), error) {
	if databaseURL == "" {
		return func() {}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	registry, err := defreg.Open(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open definition registry: %w", err)
	}
	raw, err := os.ReadFile(definitionsFile)
	if err != nil {
		registry.Close()
		return nil, fmt.Errorf("read %s: %w", definitionsFile, err)
	}
	if _, err := registry.Import(ctx, "local", raw, defreg.ImportPublish); err != nil {
		registry.Close()
		return nil, fmt.Errorf("import definitions: %w", err)
	}
	defreg.Mount(mux, registry)
	policyStore, err := policy.OpenPostgres(ctx, databaseURL)
	if err != nil {
		registry.Close()
		return nil, fmt.Errorf("open policy store: %w", err)
	}
	mux.Handle("/v1/approvals/", policy.Handler(policy.New(policyStore), forwardedEmployee))
	return func() {
		policyStore.Close()
		registry.Close()
	}, nil
}

// guardInternal rejects callers that did not come through cmd/api when
// NMC3_INTERNAL_TOKEN is set. Direct requests to this process are not a
// second login.
func guardInternal(next http.Handler) http.Handler {
	token := os.Getenv("NMC3_INTERNAL_TOKEN")
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The OAuth redirect comes straight from the browser. Its single-use
		// state value is the check.
		if toolpacks.IsCallback(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get(platform.InternalHeader)
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// forwardedEmployee is the employee cmd/api already resolved. This process
// does not verify Supabase tokens.
func forwardedEmployee(r *http.Request) (policy.Principal, error) {
	var roles []string
	for _, role := range strings.Split(r.Header.Get(platform.RolesHeader), ",") {
		role = strings.TrimSpace(role)
		if role != "" {
			roles = append(roles, role)
		}
	}
	return policy.PrincipalFromActor(platform.Actor{
		ID:    r.Header.Get(platform.UserHeader),
		Email: r.Header.Get(platform.EmailHeader),
		OrgID: r.Header.Get(platform.TenantHeader),
		Roles: roles,
	})
}

func readJSON(path string, value any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, value); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}
