package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type envelope struct {
	Run  contracts.Run `json:"run"`
	Meta Meta          `json:"meta"`
}

type Store struct {
	Pool *pgxpool.Pool
	mu   sync.Mutex
}

func txOf(ctx context.Context, pool *pgxpool.Pool) (pgx.Tx, bool, error) {
	if tx := platform.Tx(ctx); tx != nil {
		return tx, false, nil
	}
	tx, err := pool.Begin(ctx)
	return tx, true, err
}

func (s *Store) Create(ctx context.Context, run contracts.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, _ := MetaFrom(ctx)
	body, err := json.Marshal(envelope{Run: run, Meta: meta})
	if err != nil {
		return err
	}
	tx, owned, err := txOf(ctx, s.Pool)
	if err != nil {
		return err
	}
	if owned {
		defer tx.Rollback(ctx)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO nmc3.runs (
			id, organization_id, actor_id, project_id, state, agent_id, agent_version,
			execution_mode, adapter_version, snapshot, created_at, updated_at
		) VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10::jsonb,$11,$12)
	`, run.ID, meta.OrganizationID, meta.ActorID, meta.ProjectID, run.State, run.AgentID, run.Version, meta.ExecutionMode, meta.AdapterVersion, string(body), run.CreatedAt, run.UpdatedAt)
	if err != nil {
		return err
	}
	if owned {
		return tx.Commit(ctx)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, runID string) (contracts.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, _, err := s.load(ctx, runID)
	if err != nil {
		return contracts.Run{}, err
	}
	return record.Run, nil
}

func (s *Store) LoadMeta(ctx context.Context, runID string) (Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, meta, err := s.load(ctx, runID)
	return meta, err
}

func (s *Store) load(ctx context.Context, runID string) (envelope, Meta, error) {
	tx, owned, err := txOf(ctx, s.Pool)
	if err != nil {
		return envelope{}, Meta{}, err
	}
	if owned {
		defer tx.Rollback(ctx)
	}
	var raw []byte
	var state string
	err = tx.QueryRow(ctx, `SELECT state, snapshot FROM nmc3.runs WHERE id = $1`, runID).Scan(&state, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return envelope{}, Meta{}, &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	if err != nil {
		return envelope{}, Meta{}, err
	}
	var record envelope
	if err := json.Unmarshal(raw, &record); err != nil {
		return envelope{}, Meta{}, err
	}
	record.Run.State = contracts.State(state)
	if owned {
		_ = tx.Commit(ctx)
	}
	return record, record.Meta, nil
}

func (s *Store) Update(ctx context.Context, run contracts.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, owned, err := txOf(ctx, s.Pool)
	if err != nil {
		return err
	}
	if owned {
		defer tx.Rollback(ctx)
	}
	var state string
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT state, snapshot FROM nmc3.runs WHERE id = $1 FOR UPDATE`, run.ID).Scan(&state, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	if err != nil {
		return err
	}
	if !transitionOK(contracts.State(state), run.State) {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "invalid run transition"}
	}
	var record envelope
	_ = json.Unmarshal(raw, &record)
	record.Run = run
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE nmc3.runs SET state = $2, snapshot = $3::jsonb, updated_at = $4 WHERE id = $1`, run.ID, run.State, string(body), run.UpdatedAt)
	if err != nil {
		return err
	}
	if owned {
		return tx.Commit(ctx)
	}
	return nil
}

func (s *Store) Append(ctx context.Context, event contracts.RunEvent) (contracts.RunEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, owned, err := txOf(ctx, s.Pool)
	if err != nil {
		return contracts.RunEvent{}, err
	}
	if owned {
		defer tx.Rollback(ctx)
	}
	body, _ := json.Marshal(event)
	err = tx.QueryRow(ctx, `
		INSERT INTO nmc3.run_events (run_id, sequence, snapshot)
		VALUES ($1, (SELECT COALESCE(MAX(sequence), 0) + 1 FROM nmc3.run_events WHERE run_id = $1), $2::jsonb)
		RETURNING sequence
	`, event.RunID, string(body)).Scan(&event.Sequence)
	if err != nil {
		return contracts.RunEvent{}, err
	}
	if owned {
		if err := tx.Commit(ctx); err != nil {
			return contracts.RunEvent{}, err
		}
	}
	return event, nil
}

func (s *Store) List(ctx context.Context, runID string, after uint64) ([]contracts.RunEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, owned, err := txOf(ctx, s.Pool)
	if err != nil {
		return nil, err
	}
	if owned {
		defer tx.Rollback(ctx)
	}
	var exists int
	if err := tx.QueryRow(ctx, `SELECT 1 FROM nmc3.runs WHERE id = $1`, runID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
		}
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT snapshot FROM nmc3.run_events WHERE run_id = $1 AND sequence > $2 ORDER BY sequence`, runID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []contracts.RunEvent{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var event contracts.RunEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if owned {
		_ = tx.Commit(ctx)
	}
	return events, rows.Err()
}

func (s *Store) Enqueue(ctx context.Context, runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := platform.Tx(ctx)
	_, err := tx.Exec(ctx, `SELECT nmc3.enqueue_job($1)`, runID)
	return err
}

func (s *Store) RequestCancel(ctx context.Context, runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, meta, err := s.load(ctx, runID)
	if err != nil {
		return err
	}
	meta.CancelRequested = true
	record.Meta = meta
	body, _ := json.Marshal(record)
	tx := platform.Tx(ctx)
	_, err = tx.Exec(ctx, `UPDATE nmc3.runs SET snapshot = $2::jsonb, updated_at = now() WHERE id = $1`, runID, string(body))
	return err
}
