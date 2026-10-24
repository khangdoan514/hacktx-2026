package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type Worker struct {
	DB     *platform.DB
	Store  *Store
	Engine *Engine
	Name   string
}

func (w *Worker) Abandon(ctx context.Context) (int, error) {
	var affected int
	err := w.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT nmc3.abandon_leased_jobs()`).Scan(&affected)
	})
	return affected, err
}

func (w *Worker) Once(ctx context.Context) (bool, error) {
	var runID, actorID, orgID string
	err := w.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT run_id, actor_id::text, organization_id::text FROM nmc3.lease_next_job($1)`, w.Name).Scan(&runID, &actorID, &orgID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lease: %w", err)
	}
	meta, err := w.Store.LoadMeta(ctx, runID)
	if err != nil {
		_ = w.finish(ctx, runID, "done")
		return true, fmt.Errorf("meta: %w", err)
	}
	w.Engine.mu.Lock()
	if w.Engine.limits == nil {
		w.Engine.limits = map[string]contracts.Limits{}
	}
	w.Engine.limits[runID] = meta.Limits
	w.Engine.mu.Unlock()
	var outcome error
	execErr := w.DB.WithUser(ctx, actorID, "", func(ctx context.Context, tx pgx.Tx) error {
		_, outcome = w.Engine.Execute(WithMeta(ctx, meta), runID)
		var runErr *contracts.RunError
		if errors.As(outcome, &runErr) {
			return nil
		}
		if outcome != nil {
			return fmt.Errorf("execute: %w", outcome)
		}
		return nil
	})
	if finishErr := w.finish(ctx, runID, "done"); finishErr != nil && execErr == nil {
		execErr = fmt.Errorf("finish: %w", finishErr)
	}
	return true, execErr
}

func (w *Worker) finish(ctx context.Context, runID, state string) error {
	return w.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT nmc3.finish_job($1, $2)`, runID, state)
		return err
	})
}

func (w *Worker) Loop(ctx context.Context) error {
	if _, err := w.Abandon(ctx); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		worked, err := w.Once(ctx)
		if err != nil {
			return err
		}
		if !worked {
			timer := time.NewTimer(200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
}
