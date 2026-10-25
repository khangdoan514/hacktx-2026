package main

// Execution backend: OpenCode (see agents/docs/opencode-adoption.md). The
// previous custom engine was removed; its implementation remains in git
// history. Run submission creates an OpenCode session; run history stays in
// Supabase and remains inspectable after restarts.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	defreg "github.com/khangdoan514/hacktx-2026/agents/internal/definitions"
	"github.com/khangdoan514/hacktx-2026/agents/internal/extensions"
	"github.com/khangdoan514/hacktx-2026/agents/internal/httpapi"
	"github.com/khangdoan514/hacktx-2026/agents/internal/opencode"
	"github.com/khangdoan514/hacktx-2026/agents/internal/store"
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
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		openCtx, openCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defRegistry, err := defreg.Open(openCtx, databaseURL)
		openCancel()
		if err != nil {
			return fmt.Errorf("open definition registry: %w", err)
		}
		defer defRegistry.Close()
		defreg.Mount(mux, defRegistry)
	}
	server := http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("NMC3 API listening on %s (OpenCode backend %s)", addr, opencodeURL)
	return server.ListenAndServe()
}

func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
