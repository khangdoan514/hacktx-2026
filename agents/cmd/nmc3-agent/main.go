package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	"github.com/khangdoan514/hacktx-2026/agents/internal/httpapi"
	"github.com/khangdoan514/hacktx-2026/agents/internal/model"
	"github.com/khangdoan514/hacktx-2026/agents/internal/runtime"
	"github.com/khangdoan514/hacktx-2026/agents/internal/store"
	"github.com/khangdoan514/hacktx-2026/agents/internal/tools"
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
		// Supabase is the Pass 1 store. SUPABASE_URL defaults to the configured
		// project; the server-side SUPABASE_SECRET_KEY stays out of git and
		// out of the browser. Direct Postgres remains via NMC3_STORE=postgres.
		s, err := store.NewSupabase(envDefault("SUPABASE_URL", "https://vocvwmiypifqnncxhfpj.supabase.co"), os.Getenv("SUPABASE_SECRET_KEY"), nil)
		if err != nil {
			return err
		}
		runs, events = s, s
	}
	definitions := []contracts.AgentDefinition{}
	if err := readJSON(envDefault("NMC3_DEFINITIONS_FILE", "examples/definitions.json"), &definitions); err != nil {
		return err
	}
	catalog, err := tools.NewCatalog()
	if err != nil {
		return err
	}
	gateway, err := tools.NewGateway(catalog)
	if err != nil {
		return err
	}
	registeredTools := map[string]bool{}
	for _, tool := range gateway.Definitions() {
		registeredTools[tool.ID] = true
	}
	for _, definition := range definitions {
		for _, toolID := range definition.AllowedTools {
			if !registeredTools[toolID] {
				return fmt.Errorf("agent %s references unknown tool %s", definition.ID, toolID)
			}
		}
	}
	var adapter contracts.ModelAdapter
	switch envDefault("NMC3_MODEL_ADAPTER", "deterministic") {
	case "deterministic":
		steps := map[string][]contracts.ModelResult{}
		if err := readJSON(envDefault("NMC3_DETERMINISTIC_FILE", "examples/deterministic-actions.json"), &steps); err != nil {
			return err
		}
		adapter = model.NewDeterministic(steps)
	case "openai":
		inputRate, err := positiveFloatEnv("OPENAI_INPUT_USD_PER_MILLION")
		if err != nil {
			return err
		}
		outputRate, err := positiveFloatEnv("OPENAI_OUTPUT_USD_PER_MILLION")
		if err != nil {
			return err
		}
		adapter, err = model.NewOpenAI(model.OpenAIConfig{APIKey: os.Getenv("OPENAI_API_KEY"), Model: os.Getenv("OPENAI_MODEL"), Endpoint: os.Getenv("OPENAI_ENDPOINT"), InputUSDPerMillion: inputRate, OutputUSDPerMillion: outputRate})
		if err != nil {
			return err
		}
	case "openrouter":
		// Default is Muse Spark 1.3 Contributor: verified on OpenRouter at
		// $0.10/$0.20 per million tokens. Any OpenRouter ID (free OSS, GPT,
		// Claude) may be supplied per run via StartRunRequest.model_id or
		// OPENROUTER_MODEL; the full option list is
		// examples/openrouter-models.json.
		adapter, err = model.NewOpenRouter(model.OpenRouterConfig{
			APIKey:   os.Getenv("OPENROUTER_API_KEY"),
			Model:    envDefault("OPENROUTER_MODEL", "meta/muse-spark-1.3-contributor"),
			Endpoint: os.Getenv("OPENROUTER_ENDPOINT"),
		})
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("NMC3_MODEL_ADAPTER must be deterministic, openrouter, or openai")
	}
	coordinator, err := runtime.New(definitions, adapter, gateway, runs, events)
	if err != nil {
		return err
	}
	addr := envDefault("NMC3_ADDR", "127.0.0.1:8080")
	server := &http.Server{Addr: addr, Handler: httpapi.New(coordinator, runs, events, coordinator.Definitions()), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("NMC3 API listening on %s", addr)
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

func positiveFloatEnv(key string) (float64, error) {
	v, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("%s must be a positive rate", key)
	}
	return v, nil
}
