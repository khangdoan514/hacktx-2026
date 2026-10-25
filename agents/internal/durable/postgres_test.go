package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

func pass3DSN() string {
	if v := os.Getenv("NMC3_PASS3_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://nmc3@127.0.0.1:54332/nmc3?sslmode=disable"
}

func postgresReachable(dsn string) bool {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return false
	}
	host := cfg.ConnConfig.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.ConnConfig.Port
	if port == 0 {
		port = 5432
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)), time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func TestPostgresDurableRestartProof(t *testing.T) {
	dsn := pass3DSN()
	if !postgresReachable(dsn) {
		t.Skip("nmc3-pass3-pg is not accepting connections on 127.0.0.1:54332")
	}
	ctx := context.Background()
	applyDurableMigrations(t, dsn)
	journal, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(journal.Close)
	// The disposable database keeps rows between runs. Keys stay unique per process.
	n := 0
	runID := func(name string) string {
		n++
		return fmt.Sprintf("%s-%d-%d", name, time.Now().UnixNano(), n)
	}

	t.Run("two specialists after lease expiry", func(t *testing.T) {
		clock := &manualClock{t: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
		run := admitStore(t, journal, runID("pg-restart"), clock.Now())
		plan := TwoSpecialistPlan(run.ID)
		aID := run.ID + ":specialist-a"
		bID := run.ID + ":specialist-b"
		work := &script{calls: map[string]int{}, fn: func(step Step, _ int, progress Progress) (json.RawMessage, error) {
			switch step.ID {
			case "specialist-a":
				return specialistOutput("a"), nil
			case "specialist-b":
				return specialistOutput("b"), nil
			case "finish":
				raw, err := json.Marshal(map[string]json.RawMessage{"a": progress.Outputs[aID], "b": progress.Outputs[bID]})
				return raw, err
			default:
				return nil, errors.New("unexpected step")
			}
		}}
		first := executorOn(journal, "worker-a", clock)
		first.OnCheckpoint = func(cp Checkpoint) error {
			if cp.StepID == "specialist-a" {
				return ErrWorkerStopped
			}
			return nil
		}
		if _, err := first.Run(ctx, run.ID, plan, work); !errors.Is(err, ErrWorkerStopped) {
			t.Fatal(err)
		}
		old, held, err := journal.CurrentLease(ctx, run.ID)
		if err != nil || !held {
			t.Fatalf("lease after stop held=%v err=%v", held, err)
		}
		second := executorOn(journal, "worker-b", clock)
		if _, err := second.Run(ctx, run.ID, plan, work); !errors.Is(err, ErrLeaseHeld) {
			t.Fatalf("live lease: %v", err)
		}
		clock.Advance(2 * time.Minute)
		if _, err := second.Run(ctx, run.ID, plan, work); !errors.Is(err, ErrNeedsRecovery) {
			t.Fatalf("expired lease: %v", err)
		}
		if work.count("specialist-b") != 0 {
			t.Fatal("expired Run dispatched a specialist")
		}
		finished, err := second.Recover(ctx, run.ID, plan, work)
		if err != nil {
			t.Fatal(err)
		}
		if finished.State != contracts.StateSucceeded {
			t.Fatalf("recovered state %s", finished.State)
		}
		if work.count("specialist-a") != 1 || work.count("specialist-b") != 1 || work.count("finish") != 1 {
			t.Fatalf("calls after recover %#v", work.calls)
		}
		var output map[string]json.RawMessage
		if err := json.Unmarshal(finished.Output, &output); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(output["a"], []byte(`"a"`)) || !bytes.Contains(output["b"], []byte(`"b"`)) {
			t.Fatalf("finish lost recorded progress: %s", finished.Output)
		}
		if _, err := executorOn(journal, "worker-c", clock).Recover(ctx, run.ID, plan, work); err != nil {
			t.Fatal(err)
		}
		if work.count("specialist-a") != 1 || work.count("specialist-b") != 1 || work.count("finish") != 1 {
			t.Fatal("completed run executed again")
		}
		events, err := journal.List(ctx, run.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		assertOrdered(t, events)
		if countEvents(events, EventInvocationCompleted, aID) != 1 || countEvents(events, EventInvocationCompleted, bID) != 1 {
			t.Fatal("invocation completion was not unique")
		}
		if countEvents(events, EventRunCompleted, "") != 1 {
			t.Fatal("run completion was not unique")
		}
		attempts, err := journal.Attempts(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		var specialistA Attempt
		for _, att := range attempts {
			if att.InvocationID == aID && att.State == AttemptSucceeded {
				specialistA = att
			}
		}
		if specialistA.ID == "" {
			t.Fatal("specialist A has no succeeded attempt")
		}
		if _, err := journal.CommitStep(ctx, CommitRequest{
			AttemptID: specialistA.ID, WorkerID: old.WorkerID, FencingToken: old.FencingToken,
			Output: json.RawMessage(`{"specialist":"again"}`), Now: clock.Now(),
		}); !errors.Is(err, ErrStaleLease) {
			t.Fatalf("stale fencing token: %v", err)
		}
		outcome, ok, err := journal.Invocation(ctx, aID)
		if err != nil || !ok || outcome.State != contracts.StateSucceeded || !bytes.Contains(outcome.Output, []byte(`"a"`)) {
			t.Fatalf("stale commit changed the outcome ok=%v err=%v %+v", ok, err, outcome)
		}
		replay, err := second.Replay(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(replay.Checkpoints) < 3 || replay.Checkpoints[0].Sequence != 1 {
			t.Fatalf("checkpoints %+v", replay.Checkpoints)
		}
	})

	t.Run("stale fence while the new lease is held", func(t *testing.T) {
		clock := &manualClock{t: time.Date(2026, 10, 24, 13, 0, 0, 0, time.UTC)}
		run := admitStore(t, journal, runID("pg-fence"), clock.Now())
		plan := TwoSpecialistPlan(run.ID)
		aID := run.ID + ":specialist-a"
		work := &script{calls: map[string]int{}, fn: func(step Step, n int, _ Progress) (json.RawMessage, error) {
			if step.ID == "specialist-a" && n == 1 {
				return nil, ErrWorkerStopped
			}
			if step.ID == "finish" {
				return json.RawMessage(`{"done":true}`), nil
			}
			return specialistOutput(step.ID), nil
		}}
		if _, err := executorOn(journal, "worker-a", clock).Run(ctx, run.ID, plan, work); !errors.Is(err, ErrWorkerStopped) {
			t.Fatal(err)
		}
		old, held, err := journal.CurrentLease(ctx, run.ID)
		if err != nil || !held {
			t.Fatalf("lease held=%v err=%v", held, err)
		}
		clock.Advance(2 * time.Minute)
		next, abandoned, err := journal.RecoverClaim(ctx, run.ID, "worker-b", time.Minute, clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		if len(abandoned) != 1 || abandoned[0].State != AttemptAbandoned || abandoned[0].FencingToken != old.FencingToken {
			t.Fatalf("abandoned %+v", abandoned)
		}
		if _, err := journal.CommitStep(ctx, CommitRequest{
			AttemptID: abandoned[0].ID, WorkerID: old.WorkerID, FencingToken: old.FencingToken,
			Output: json.RawMessage(`{"specialist":"stale"}`), Now: clock.Now(),
		}); !errors.Is(err, ErrStaleLease) {
			t.Fatalf("stale token: %v", err)
		}
		if _, ok, err := journal.Invocation(ctx, aID); err != nil || ok {
			t.Fatal("stale worker wrote an invocation outcome")
		}
		if _, err := journal.CommitStep(ctx, CommitRequest{
			AttemptID: abandoned[0].ID, WorkerID: next.WorkerID, FencingToken: next.FencingToken,
			Output: json.RawMessage(`{"specialist":"taken"}`), Now: clock.Now(),
		}); err == nil {
			t.Fatal("new worker completed the abandoned attempt")
		}
		if _, ok, err := journal.Invocation(ctx, aID); err != nil || ok {
			t.Fatal("abandoned attempt gained an outcome")
		}
		if err := journal.ReleaseLease(ctx, next, clock.Now()); err != nil {
			t.Fatal(err)
		}
		finished, err := executorOn(journal, "worker-b", clock).Recover(ctx, run.ID, plan, work)
		if err != nil {
			t.Fatal(err)
		}
		if finished.State != contracts.StateSucceeded {
			t.Fatalf("state %s", finished.State)
		}
		if work.count("specialist-a") != 2 || work.count("specialist-b") != 1 || work.count("finish") != 1 {
			t.Fatalf("calls %#v", work.calls)
		}
		outcome, ok, err := journal.Invocation(ctx, aID)
		if err != nil || !ok || outcome.State != contracts.StateSucceeded {
			t.Fatalf("outcome ok=%v err=%v %+v", ok, err, outcome)
		}
		successes := 0
		for _, att := range mustStoreAttempts(t, journal, run.ID) {
			if att.InvocationID == aID && att.State == AttemptSucceeded {
				successes++
			}
		}
		if successes != 1 {
			t.Fatalf("successful A attempts %d", successes)
		}
	})

	t.Run("read retry stays within MaxReadAttempts", func(t *testing.T) {
		clock := &manualClock{t: time.Date(2026, 10, 24, 14, 0, 0, 0, time.UTC)}
		run := admitStore(t, journal, runID("pg-read"), clock.Now())
		plan := ReadToolPlan(run.ID, "catalog")
		toolInvocation := run.ID + ":tool:catalog"
		reads := 0
		handler := HandlerFunc(func(_ context.Context, step Step, progress Progress) (json.RawMessage, error) {
			if step.Kind == KindReadTool {
				reads++
				if reads == 1 {
					return nil, ErrLostResponse
				}
				return json.RawMessage(`{"rows":1}`), nil
			}
			raw, err := json.Marshal(map[string]json.RawMessage{"tool": progress.Outputs[toolInvocation]})
			return raw, err
		})
		worker := executorOn(journal, "worker-a", clock)
		worker.MaxReadAttempts = 2
		finished, err := worker.Run(ctx, run.ID, plan, handler)
		if err != nil {
			t.Fatal(err)
		}
		if finished.State != contracts.StateSucceeded {
			t.Fatalf("state %s", finished.State)
		}
		if reads != 2 {
			t.Fatalf("read calls %d", reads)
		}
		var failed, succeeded int
		for _, att := range mustStoreAttempts(t, journal, run.ID) {
			if att.InvocationID != toolInvocation {
				continue
			}
			switch att.State {
			case AttemptFailed:
				failed++
				if att.AttemptNumber != 1 {
					t.Fatalf("failed attempt %+v", att)
				}
			case AttemptSucceeded:
				succeeded++
				if att.AttemptNumber != 2 {
					t.Fatalf("succeeded attempt %+v", att)
				}
			}
		}
		if failed != 1 || succeeded != 1 {
			t.Fatalf("failed=%d succeeded=%d", failed, succeeded)
		}
		if _, ok, err := journal.Invocation(ctx, toolInvocation); err != nil || !ok {
			t.Fatal("read invocation missing")
		}
		events, err := journal.List(ctx, run.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		assertOrdered(t, events)
		if countEvents(events, EventInvocationCompleted, toolInvocation) != 1 {
			t.Fatal("read invocation completed more than once")
		}

		boundRun := admitStore(t, journal, runID("pg-read-bound"), clock.Now())
		boundReads := 0
		bound := executorOn(journal, "worker-bound", clock)
		bound.MaxReadAttempts = 2
		boundHandler := HandlerFunc(func(_ context.Context, step Step, _ Progress) (json.RawMessage, error) {
			if step.Kind == KindReadTool {
				boundReads++
				return nil, ErrLostResponse
			}
			t.Fatal("finish ran after exhausted reads")
			return nil, nil
		})
		got, err := bound.Run(ctx, boundRun.ID, ReadToolPlan(boundRun.ID, "catalog"), boundHandler)
		if !errors.Is(err, ErrRetriesExhausted) || got.State != contracts.StateFailed || boundReads != 2 {
			t.Fatalf("err=%v state=%s reads=%d", err, got.State, boundReads)
		}
		if _, err := bound.Recover(ctx, boundRun.ID, ReadToolPlan(boundRun.ID, "catalog"), boundHandler); err != nil {
			t.Fatal(err)
		}
		if boundReads != 2 {
			t.Fatal("failed run retried the read past MaxReadAttempts")
		}
	})

	t.Run("cancellation survives a new executor", func(t *testing.T) {
		clock := &manualClock{t: time.Date(2026, 10, 24, 15, 0, 0, 0, time.UTC)}
		run := admitStore(t, journal, runID("pg-cancel"), clock.Now())
		plan := TwoSpecialistPlan(run.ID)
		work := &script{calls: map[string]int{}, fn: func(step Step, _ int, _ Progress) (json.RawMessage, error) {
			switch step.ID {
			case "specialist-a":
				return specialistOutput("a"), nil
			case "specialist-b", "finish":
				return json.RawMessage(`{"ran":true}`), nil
			default:
				return nil, errors.New("unexpected step")
			}
		}}
		first := executorOn(journal, "worker-a", clock)
		first.OnCheckpoint = func(cp Checkpoint) error {
			if cp.StepID == "specialist-a" {
				return errors.New("killed")
			}
			return nil
		}
		if _, err := first.Run(ctx, run.ID, plan, work); !errors.Is(err, ErrWorkerStopped) {
			t.Fatal(err)
		}
		cancel, err := executorOn(journal, "api", clock).Cancel(ctx, run.ID, "user stopped the run")
		if err != nil {
			t.Fatal(err)
		}
		again, err := journal.Cancel(ctx, run.ID, "second reason", clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		if !again.RequestedAt.Equal(cancel.RequestedAt) || again.Reason != cancel.Reason {
			t.Fatal("cancellation was not idempotent")
		}
		if _, err := executorOn(journal, "worker-b", clock).Recover(ctx, run.ID, plan, work); !errors.Is(err, ErrLeaseHeld) {
			t.Fatalf("recover while the first lease is live: %v", err)
		}
		clock.Advance(2 * time.Minute)
		resumed, err := executorOn(journal, "worker-b", clock).Recover(ctx, run.ID, plan, work)
		if err != nil {
			t.Fatal(err)
		}
		if resumed.State != contracts.StateCancelled {
			t.Fatalf("restarted state %s", resumed.State)
		}
		if work.count("specialist-a") != 1 || work.count("specialist-b") != 0 || work.count("finish") != 0 {
			t.Fatalf("calls after cancelled restart %#v", work.calls)
		}
		record, ok, err := journal.Cancellation(ctx, run.ID)
		if err != nil || !ok || record.Reason != "user stopped the run" {
			t.Fatalf("cancellation record ok=%v err=%v %+v", ok, err, record)
		}
		if _, err := executorOn(journal, "worker-c", clock).Run(ctx, run.ID, plan, work); err != nil {
			t.Fatal(err)
		}
		if work.count("specialist-b") != 0 {
			t.Fatal("new executor started work on a cancelled run")
		}
	})

	t.Run("write stays uncertain and is not dispatched", func(t *testing.T) {
		clock := &manualClock{t: time.Date(2026, 10, 24, 16, 0, 0, 0, time.UTC)}
		run := admitStore(t, journal, runID("pg-write"), clock.Now())
		plan := Plan{Steps: []Step{
			{ID: "send", Kind: KindWrite, InvocationID: run.ID + ":send", SideEffect: SideEffectWrite},
			{ID: "finish", Kind: KindFinish, InvocationID: RootInvocationID(run.ID), SideEffect: SideEffectNone},
		}}
		calls := 0
		handler := HandlerFunc(func(context.Context, Step, Progress) (json.RawMessage, error) {
			calls++
			return json.RawMessage(`{"sent":true}`), nil
		})
		worker := executorOn(journal, "worker-a", clock)
		got, err := worker.Run(ctx, run.ID, plan, handler)
		if !errors.Is(err, ErrProtectedWrite) {
			t.Fatal(err)
		}
		if got.State == contracts.StateSucceeded {
			t.Fatal("refused write completed the run")
		}
		if calls != 0 {
			t.Fatalf("write handler calls %d", calls)
		}
		found := false
		for _, att := range mustStoreAttempts(t, journal, run.ID) {
			if att.SideEffect == SideEffectWrite && att.State == AttemptUncertain {
				found = true
			}
		}
		if !found {
			t.Fatal("write was not recorded as UNCERTAIN")
		}
		if _, ok, err := journal.Invocation(ctx, run.ID+":send"); err != nil || ok {
			t.Fatal("write recorded an invocation outcome")
		}
		if _, err := executorOn(journal, "worker-b", clock).Recover(ctx, run.ID, plan, handler); !errors.Is(err, ErrUncertain) {
			t.Fatal(err)
		}
		if calls != 0 {
			t.Fatal("recovered executor dispatched the write")
		}
		uncertain := 0
		for _, att := range mustStoreAttempts(t, journal, run.ID) {
			if att.State == AttemptUncertain && att.SideEffect == SideEffectWrite {
				uncertain++
			}
		}
		if uncertain != 1 {
			t.Fatalf("uncertain write attempts %d", uncertain)
		}
	})
}

func executorOn(store Store, worker string, clock *manualClock) *Executor {
	return &Executor{
		Store:           store,
		WorkerID:        worker,
		LeaseTTL:        time.Minute,
		MaxReadAttempts: 2,
		Now:             clock.Now,
	}
}

func admitStore(t *testing.T, store Store, id string, now time.Time) contracts.Run {
	t.Helper()
	run, created, err := store.Admit(context.Background(), AdmitRequest{
		IdempotencyKey: "key-" + id,
		Run: contracts.Run{
			ID: id, AgentID: "orchestrator", Version: "1",
			Input: json.RawMessage(`{"job":"demo"}`),
		},
		Now: now,
	})
	if err != nil || !created {
		t.Fatalf("admit created=%v err=%v", created, err)
	}
	return run
}

func mustStoreAttempts(t *testing.T, store Store, runID string) []Attempt {
	t.Helper()
	attempts, err := store.Attempts(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	return attempts
}

func applyDurableMigrations(t *testing.T, dsn string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgconn.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	execSQL(t, ctx, conn, `
		do $$
		begin
		  if not exists (select 1 from pg_roles where rolname = 'anon') then
		    create role anon;
		  end if;
		  if not exists (select 1 from pg_roles where rolname = 'authenticated') then
		    create role authenticated;
		  end if;
		  if not exists (select 1 from pg_roles where rolname = 'service_role') then
		    create role service_role;
		  end if;
		end
		$$;`)
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("migration path")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "db", "migrations")
	// 005 is applied before any later durable migration. 008 is not required:
	// parent links are recovered from the run snapshot already stored by 001.
	for _, name := range []string{
		"202610240001_nmc3_pass1.sql",
		"202610240003_nmc3_event_source.sql",
		"202610240005_nmc3_durable.sql",
	} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		execSQL(t, ctx, conn, string(body))
	}
}

func execSQL(t *testing.T, ctx context.Context, conn *pgconn.PgConn, sql string) {
	t.Helper()
	results, err := conn.Exec(ctx, sql).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Err != nil {
			t.Fatal(result.Err)
		}
	}
}
