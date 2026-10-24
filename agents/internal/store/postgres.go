package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// Postgres stores run snapshots and an append-only, per-run ordered event log.
// The pool must use a direct or session-pooled Supabase Postgres connection.
type Postgres struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, databaseURL string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{Pool: pool}, nil
}

func (s *Postgres) Close() { s.Pool.Close() }

func (s *Postgres) Create(ctx context.Context, run contracts.Run) error {
	if run.ID == "" {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "run ID is required"}
	}
	if run.Invocations == nil {
		run.Invocations = []contracts.AgentInvocation{}
	}
	if run.ToolInvocations == nil {
		run.ToolInvocations = []contracts.ToolInvocation{}
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = run.CreatedAt
	}
	snapshot, err := json.Marshal(run)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `insert into nmc3.runs (id,state,agent_id,agent_version,snapshot,created_at,updated_at) values ($1,$2,$3,$4,$5,$6,$7)`, run.ID, run.State, run.AgentID, run.Version, snapshot, run.CreatedAt, run.UpdatedAt)
	return classify(err)
}

func (s *Postgres) Get(ctx context.Context, runID string) (contracts.Run, error) {
	var raw []byte
	err := s.Pool.QueryRow(ctx, `select snapshot from nmc3.runs where id=$1`, runID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	if err != nil {
		return contracts.Run{}, err
	}
	var run contracts.Run
	if err := json.Unmarshal(raw, &run); err != nil {
		return contracts.Run{}, err
	}
	if run.Invocations == nil {
		run.Invocations = []contracts.AgentInvocation{}
	}
	if run.ToolInvocations == nil {
		run.ToolInvocations = []contracts.ToolInvocation{}
	}
	return run, nil
}

func (s *Postgres) Update(ctx context.Context, run contracts.Run) error {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var oldState contracts.State
	var oldSnapshot []byte
	err = tx.QueryRow(ctx, `select state,snapshot from nmc3.runs where id=$1 for update`, run.ID).Scan(&oldState, &oldSnapshot)
	if errors.Is(err, pgx.ErrNoRows) {
		return &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	if err != nil {
		return err
	}
	if !transition(oldState, run.State) {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "invalid run state transition"}
	}
	if run.Invocations == nil {
		run.Invocations = []contracts.AgentInvocation{}
	}
	if run.ToolInvocations == nil {
		run.ToolInvocations = []contracts.ToolInvocation{}
	}
	raw, err := json.Marshal(run)
	if err != nil {
		return err
	}
	if oldState == contracts.StateSucceeded || oldState == contracts.StateFailed || oldState == contracts.StateCancelled {
		var before, after any
		if err := json.Unmarshal(oldSnapshot, &before); err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &after); err != nil {
			return err
		}
		oldCanonical, _ := json.Marshal(before)
		newCanonical, _ := json.Marshal(after)
		if string(oldCanonical) != string(newCanonical) {
			return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "terminal run cannot change"}
		}
	}
	_, err = tx.Exec(ctx, `update nmc3.runs set state=$2,snapshot=$3,updated_at=$4 where id=$1`, run.ID, run.State, raw, run.UpdatedAt)
	if err != nil {
		return err
	}
	for _, inv := range run.Invocations {
		payload, err := json.Marshal(inv)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `insert into nmc3.agent_invocations(id,run_id,parent_id,agent_id,agent_version,depth,state,snapshot) values($1,$2,$3,$4,$5,$6,$7,$8) on conflict(id) do update set state=excluded.state,snapshot=excluded.snapshot`, inv.ID, run.ID, inv.ParentID, inv.AgentID, inv.AgentVersion, inv.Depth, inv.State, payload)
		if err != nil {
			return err
		}
	}
	for _, tool := range run.ToolInvocations {
		payload, err := json.Marshal(tool)
		if err != nil {
			return err
		}
		state := "SUCCEEDED"
		if tool.Error != nil {
			state = "FAILED"
		} else if tool.FinishedAt == nil {
			state = "RUNNING"
		}
		_, err = tx.Exec(ctx, `insert into nmc3.tool_invocations(id,invocation_id,tool_id,tool_version,state,snapshot,started_at,finished_at) values($1,$2,$3,$4,$5,$6,$7,$8) on conflict(id) do update set state=excluded.state,snapshot=excluded.snapshot,finished_at=excluded.finished_at`, tool.ID, tool.AgentInvocationID, tool.ToolID, tool.ToolVersion, state, payload, tool.StartedAt, tool.FinishedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func transition(from, to contracts.State) bool {
	switch from {
	case contracts.StatePending:
		return to == contracts.StatePending || to == contracts.StateRunning || to == contracts.StateFailed || to == contracts.StateCancelled
	case contracts.StateRunning:
		return to == contracts.StateRunning || to == contracts.StateSucceeded || to == contracts.StateFailed || to == contracts.StateCancelled
	default:
		return from == to
	}
}

func (s *Postgres) Append(ctx context.Context, event contracts.RunEvent) (contracts.RunEvent, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return event, err
	}
	defer tx.Rollback(ctx)
	var seq int64
	err = tx.QueryRow(ctx, `update nmc3.runs set next_event_sequence=next_event_sequence+1 where id=$1 returning next_event_sequence`, event.RunID).Scan(&seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return event, &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	if err != nil {
		return event, err
	}
	event.Sequence = uint64(seq)
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	var data any
	if len(event.Data) > 0 {
		data = event.Data
	}
	_, err = tx.Exec(ctx, `insert into nmc3.run_events(run_id,sequence,invocation_id,event_type,occurred_at,data) values($1,$2,$3,$4,$5,$6)`, event.RunID, seq, event.InvocationID, event.Type, event.Timestamp, data)
	if err != nil {
		return event, err
	}
	if err := projectOperation(ctx, tx, event); err != nil {
		return event, err
	}
	return event, tx.Commit(ctx)
}

// Project operation metadata from the safe event stream. Run snapshots remain
// authoritative; these tables make individual turns and tool attempts queryable.
func projectOperation(ctx context.Context, tx pgx.Tx, e contracts.RunEvent) error {
	if e.InvocationID == nil {
		return nil
	}
	switch e.Type {
	case "MODEL_STARTED":
		_, err := tx.Exec(ctx, `insert into nmc3.model_turns(invocation_id,ordinal,occurred_at) select $1,coalesce(max(ordinal),0)+1,$2 from nmc3.model_turns where invocation_id=$1`, *e.InvocationID, e.Timestamp)
		return err
	case "MODEL_SUCCEEDED", "MODEL_FAILED":
		var meta struct {
			Action string          `json:"action"`
			Usage  contracts.Usage `json:"usage"`
			Code   string          `json:"code"`
		}
		if len(e.Data) > 0 {
			if err := json.Unmarshal(e.Data, &meta); err != nil {
				return err
			}
		}
		usage, _ := json.Marshal(meta.Usage)
		var errorJSON any
		if meta.Code != "" {
			errorJSON, _ = json.Marshal(map[string]string{"code": meta.Code})
		}
		_, err := tx.Exec(ctx, `update nmc3.model_turns set action_type=$2,usage=$3,error=$4 where invocation_id=$1 and ordinal=(select max(ordinal) from nmc3.model_turns where invocation_id=$1)`, *e.InvocationID, meta.Action, usage, errorJSON)
		return err
	}
	return nil
}

func (s *Postgres) List(ctx context.Context, runID string, after uint64) ([]contracts.RunEvent, error) {
	var exists bool
	if err := s.Pool.QueryRow(ctx, `select exists(select 1 from nmc3.runs where id=$1)`, runID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	rows, err := s.Pool.Query(ctx, `select sequence,invocation_id,event_type,occurred_at,data from nmc3.run_events where run_id=$1 and sequence>$2 order by sequence`, runID, int64(after))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []contracts.RunEvent{}
	for rows.Next() {
		e := contracts.RunEvent{RunID: runID}
		var seq int64
		var data []byte
		if err := rows.Scan(&seq, &e.InvocationID, &e.Type, &e.Timestamp, &data); err != nil {
			return nil, err
		}
		e.Sequence = uint64(seq)
		e.Data = data
		events = append(events, e)
	}
	return events, rows.Err()
}

func classify(err error) error {
	if err == nil {
		return nil
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23505" {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "run ID already exists"}
	}
	return fmt.Errorf("database operation failed: %w", err)
}
