package productapi

import (
	"context"

	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
	"github.com/khangdoan514/hacktx-2026/internal/agents/dummy"
	"github.com/khangdoan514/hacktx-2026/internal/agents/runtime"
	"github.com/khangdoan514/hacktx-2026/internal/notifications"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

func Wire(ctx context.Context, cfg platform.Config, db *platform.DB) (*Server, *runtime.Worker, error) {
	definitions, err := dummy.LoadDefinitions()
	if err != nil {
		return nil, nil, err
	}
	catalog := map[string]contracts.AgentDefinition{}
	for _, definition := range definitions {
		catalog[definition.ID] = definition
	}
	script, err := dummy.LoadScript()
	if err != nil {
		return nil, nil, err
	}
	store := &runtime.Store{Pool: db.Pool}
	engine := &runtime.Engine{
		Store: store, Events: store, Adapter: script, Queue: store,
		Gateway:        runtime.NewGateway(dummy.CatalogTool(), dummy.DiffTool()),
		Definitions:    catalog,
		Mode:           cfg.AgentExecutionMode,
		AdapterVersion: platform.AdapterVersion,
	}
	if cfg.AgentAPIURL == "" {
		cfg.AgentAPIURL = "http://127.0.0.1:8080"
	}
	server := &Server{
		DB: db, Config: cfg, Engine: engine, Store: store,
		Hub: notifications.NewHub(), Keys: platform.NewJWKS(cfg.JWKSURL),
	}
	worker := &runtime.Worker{DB: db, Store: store, Engine: engine, Name: "nmc3-worker"}
	_ = ctx
	return server, worker, nil
}
