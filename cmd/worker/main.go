package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/khangdoan514/hacktx-2026/internal/platform"
	"github.com/khangdoan514/hacktx-2026/internal/productapi"
)

func main() {
	cfg := platform.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := platform.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	_, worker, err := productapi.Wire(ctx, cfg, db)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("NMC3 worker %s leasing jobs", worker.Name)
	if err := worker.Loop(ctx); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
