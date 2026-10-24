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
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	s, err := store.Open(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect to Postgres: %w", err)
	}
	defer s.Close()
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
	default:
		return fmt.Errorf("NMC3_MODEL_ADAPTER must be deterministic or openai")
	}
	coordinator, err := runtime.New(definitions, adapter, gateway, s, s)
	if err != nil {
		return err
	}
	addr := envDefault("NMC3_ADDR", "127.0.0.1:8080")
	server := &http.Server{Addr: addr, Handler: httpapi.New(coordinator, s, s, coordinator.Definitions()), ReadHeaderTimeout: 10 * time.Second}
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
