package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/khangdoan514/hacktx-2026/internal/organization"
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
	ctx := context.Background()
	if cfg.Environment == "local" || cfg.Environment == "test" {
		if err := platform.ApplyMigrations(ctx, cfg.DatabaseURL); err != nil {
			log.Fatal(err)
		}
	}
	db, err := platform.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if cfg.Environment == "local" || cfg.Environment == "test" {
		if err := organization.Seed(ctx, db); err != nil {
			log.Fatal(err)
		}
	}
	server, worker, err := productapi.Wire(ctx, cfg, db)
	if err != nil {
		log.Fatal(err)
	}
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	go (&productapi.AutomationWorker{Server: server}).Loop(workerCtx)
	go (&productapi.GitHubDeliveryWorker{Server: server}).Loop(workerCtx)
	if cfg.Environment == "local" || cfg.Environment == "test" {
		go func() {
			if err := worker.Loop(workerCtx); err != nil && workerCtx.Err() == nil {
				log.Printf("worker stopped: %v", err)
			}
		}()
	}
	httpServer := &http.Server{Addr: cfg.Addr, Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("NMC3 API listening on %s", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	stopWorker()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdown)
}
