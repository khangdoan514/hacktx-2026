package main

// Transitional entrypoint: the custom execution engine (runtime, model, and
// tool packages) was removed in favor of OpenCode; see
// agents/docs/opencode-adoption.md. The full implementation remains in git
// history. This binary serves persisted run history from Supabase until the
// OpenCode execution backend lands; run submission is disabled.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	"github.com/khangdoan514/hacktx-2026/agents/internal/httpapi"
	"github.com/khangdoan514/hacktx-2026/agents/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// disabled rejects run submission while history stays readable.
type disabled struct{}

func (disabled) Start(context.Context, contracts.StartRunRequest) (contracts.Run, error) {
	return contracts.Run{}, &contracts.RunError{Code: contracts.ErrInternal, Message: "agent execution is disabled during the OpenCode migration"}
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
	// Definitions still feed GET /v1/agents; execution no longer reads them.
	definitions := []contracts.AgentDefinition{}
	if err := readJSON(envDefault("NMC3_DEFINITIONS_FILE", "examples/definitions.json"), &definitions); err != nil {
		return err
	}
	addr := envDefault("NMC3_ADDR", "127.0.0.1:8080")
	server := http.Server{Addr: addr, Handler: httpapi.New(disabled{}, runs, events, definitions), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("NMC3 API listening on %s (history only; execution disabled)", addr)
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
