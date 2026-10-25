package durable

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

func TestCompletedInvocationOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-once", f.clock.Now())
	now := f.clock.Now()
	lease, err := f.m.ClaimRun(ctx, run.ID, "worker-a", time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	step := TwoSpecialistPlan(run.ID).Steps[0]
	att, err := f.m.BeginAttempt(ctx, BeginRequest{
		RunID: run.ID, Step: step, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstOut := json.RawMessage(`{"n":1}`)
	if _, err := f.m.CommitStep(ctx, CommitRequest{
		AttemptID: att.ID, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken, Output: firstOut, Now: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.m.CommitStep(ctx, CommitRequest{
		AttemptID: att.ID, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken, Output: json.RawMessage(`{"n":2}`), Now: now,
	}); !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatal(err)
	}
	outcome, ok, err := f.m.Invocation(ctx, step.InvocationID)
	if err != nil || !ok || string(outcome.Output) != string(firstOut) {
		t.Fatalf("outcome changed ok=%v err=%v %s", ok, err, outcome.Output)
	}
	if _, err := f.m.BeginAttempt(ctx, BeginRequest{
		RunID: run.ID, Step: step, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken, Now: now,
	}); !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatal(err)
	}
	events, err := f.m.List(ctx, run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertOrdered(t, events)
	if countEvents(events, EventInvocationCompleted, step.InvocationID) != 1 {
		t.Fatal("invocation completed twice")
	}
}

func TestConcurrentCommitKeepsOneOutcome(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-race", f.clock.Now())
	now := f.clock.Now()
	lease, err := f.m.ClaimRun(ctx, run.ID, "worker-a", time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	step := TwoSpecialistPlan(run.ID).Steps[0]
	att, err := f.m.BeginAttempt(ctx, BeginRequest{
		RunID: run.ID, Step: step, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for _, output := range []string{`{"n":1}`, `{"n":2}`} {
		wg.Add(1)
		go func(output string) {
			defer wg.Done()
			_, err := f.m.CommitStep(ctx, CommitRequest{
				AttemptID: att.ID, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken,
				Output: json.RawMessage(output), Now: now,
			})
			errCh <- err
		}(output)
	}
	wg.Wait()
	close(errCh)
	var completed, rejected int
	for err := range errCh {
		switch {
		case err == nil:
			completed++
		case errors.Is(err, ErrAlreadyCompleted):
			rejected++
		default:
			t.Fatal(err)
		}
	}
	if completed != 1 || rejected != 1 {
		t.Fatalf("completed=%d rejected=%d", completed, rejected)
	}
	outcome, ok, err := f.m.Invocation(ctx, step.InvocationID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if _, err := f.m.CommitStep(ctx, CommitRequest{
		AttemptID: att.ID, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken,
		Output: json.RawMessage(`{"n":3}`), Now: now,
	}); !errors.Is(err, ErrAlreadyCompleted) {
		t.Fatal(err)
	}
	again, _, err := f.m.Invocation(ctx, step.InvocationID)
	if err != nil || string(again.Output) != string(outcome.Output) {
		t.Fatal("second completion changed the outcome")
	}
}

func TestConcurrentBeginAttempt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-attempt", f.clock.Now())
	now := f.clock.Now()
	lease, err := f.m.ClaimRun(ctx, run.ID, "worker-a", time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	step := TwoSpecialistPlan(run.ID).Steps[0]
	start := make(chan struct{})
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.m.BeginAttempt(ctx, BeginRequest{
				RunID: run.ID, Step: step, WorkerID: lease.WorkerID, FencingToken: lease.FencingToken, Now: now,
			})
			errCh <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	var won, denied int
	for err := range errCh {
		switch {
		case err == nil:
			won++
		case errors.Is(err, ErrAttemptClaimed):
			denied++
		default:
			t.Fatal(err)
		}
	}
	if won != 1 || denied != 1 {
		t.Fatalf("won=%d denied=%d", won, denied)
	}
	active := 0
	for _, att := range mustAttempts(t, f.m, run.ID) {
		if att.State == AttemptRunning {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("active attempts %d", active)
	}
}

func TestConcurrentClaimRun(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-lease", f.clock.Now())
	now := f.clock.Now()
	start := make(chan struct{})
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for _, worker := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(worker string) {
			defer wg.Done()
			<-start
			_, err := f.m.ClaimRun(ctx, run.ID, worker, time.Minute, now)
			errCh <- err
		}(worker)
	}
	close(start)
	wg.Wait()
	close(errCh)
	var won, denied int
	for err := range errCh {
		switch {
		case err == nil:
			won++
		case errors.Is(err, ErrLeaseHeld):
			denied++
		default:
			t.Fatal(err)
		}
	}
	if won != 1 || denied != 1 {
		t.Fatalf("won=%d denied=%d", won, denied)
	}
}

func TestStaleWorkerCannotComplete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-stale", f.clock.Now())
	now := f.clock.Now()
	lease, err := f.m.ClaimRun(ctx, run.ID, "worker-a", time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	step := TwoSpecialistPlan(run.ID).Steps[0]
	att, err := f.m.BeginAttempt(ctx, BeginRequest{
		RunID: run.ID, Step: step, WorkerID: "worker-a", FencingToken: lease.FencingToken, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(2 * time.Minute)
	next, abandoned, err := f.m.RecoverClaim(ctx, run.ID, "worker-b", time.Minute, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(abandoned) != 1 || abandoned[0].State != AttemptAbandoned {
		t.Fatalf("abandoned %+v", abandoned)
	}
	if got := mustGet(t, f.m, run.ID); got.State == contracts.StateSucceeded {
		t.Fatal("takeover marked the run successful")
	}
	_, err = f.m.CommitStep(ctx, CommitRequest{
		AttemptID: att.ID, WorkerID: "worker-a", FencingToken: lease.FencingToken,
		Output: json.RawMessage(`{"n":2}`), Now: later,
	})
	if !errors.Is(err, ErrStaleLease) {
		t.Fatal(err)
	}
	if _, ok, err := f.m.Invocation(ctx, step.InvocationID); err != nil || ok {
		t.Fatal("stale worker wrote an outcome")
	}
	if err := f.m.ReleaseLease(ctx, lease, later); !errors.Is(err, ErrStaleLease) {
		t.Fatal(err)
	}
	current, ok, err := f.m.CurrentLease(ctx, run.ID)
	if err != nil || !ok || current.FencingToken != next.FencingToken || current.WorkerID != "worker-b" {
		t.Fatalf("lease after stale release ok=%v err=%v %+v", ok, err, current)
	}
	_, err = f.m.CommitStep(ctx, CommitRequest{
		AttemptID: att.ID, WorkerID: "worker-b", FencingToken: next.FencingToken,
		Output: json.RawMessage(`{"n":2}`), Now: later,
	})
	if err == nil {
		t.Fatal("new worker completed the abandoned attempt")
	}
	if got := mustGet(t, f.m, run.ID); got.State == contracts.StateSucceeded {
		t.Fatal("abandoned attempt completed the run")
	}
}

func TestIdempotentRunCreation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := f.clock.Now()
	body := contracts.Run{ID: "run-idem", AgentID: "orchestrator", Version: "1", Input: json.RawMessage(`{"n":1}`)}
	var createdCount atomic.Int32
	ids := make(chan string, 16)
	errCh := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run, created, err := f.m.Admit(ctx, AdmitRequest{IdempotencyKey: "same", Run: body, Now: now})
			if err != nil {
				errCh <- err
				return
			}
			if created {
				createdCount.Add(1)
			}
			ids <- run.ID
		}()
	}
	wg.Wait()
	close(errCh)
	close(ids)
	for err := range errCh {
		t.Fatal(err)
	}
	if createdCount.Load() != 1 {
		t.Fatalf("created %d", createdCount.Load())
	}
	var first string
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != "run-idem" {
			t.Fatalf("id %s", id)
		}
	}
	replay, _, err := f.m.Admit(ctx, AdmitRequest{
		IdempotencyKey: "same",
		Run:            contracts.Run{ID: "other", AgentID: body.AgentID, Version: body.Version, Input: body.Input},
		Now:            now,
	})
	if err != nil || replay.ID != "run-idem" {
		t.Fatalf("replay %+v err=%v", replay, err)
	}
	if _, err := f.m.Get(ctx, "other"); err == nil {
		t.Fatal("second proposed run ID was stored")
	}
	if _, _, err := f.m.Admit(ctx, AdmitRequest{
		IdempotencyKey: "same",
		Run:            contracts.Run{ID: "run-idem", AgentID: body.AgentID, Version: body.Version, Input: json.RawMessage(`{"n":2}`)},
		Now:            now,
	}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatal(err)
	}
	if _, _, err := f.m.Admit(ctx, AdmitRequest{Run: body, Now: now}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	events, err := f.m.List(ctx, "run-idem", 0)
	if err != nil {
		t.Fatal(err)
	}
	if countEvents(events, EventRunAdmitted, "") != 1 {
		t.Fatal("admission event was duplicated")
	}
	other, created, err := f.m.Admit(ctx, AdmitRequest{
		TenantID: "other", IdempotencyKey: "same",
		Run: contracts.Run{ID: "run-other-tenant", AgentID: body.AgentID, Version: body.Version, Input: body.Input},
		Now: now,
	})
	if err != nil || !created || other.ID != "run-other-tenant" {
		t.Fatalf("tenant isolation created=%v err=%v %+v", created, err, other)
	}
}

func TestRetentionKeepsLatestCumulativeCheckpoint(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-retain", f.clock.Now())
	handler := HandlerFunc(func(_ context.Context, step Step, progress Progress) (json.RawMessage, error) {
		switch step.ID {
		case "specialist-a":
			return specialistOutput("a"), nil
		case "specialist-b":
			return specialistOutput("b"), nil
		case "finish":
			return json.RawMessage(`{"done":true}`), nil
		default:
			return nil, errors.New(step.ID)
		}
	})
	if _, err := f.executor("worker-a").Run(ctx, run.ID, TwoSpecialistPlan(run.ID), handler); err != nil {
		t.Fatal(err)
	}
	before, err := f.m.Checkpoints(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 3 {
		t.Fatalf("checkpoints %d", len(before))
	}
	attemptsBefore := len(mustAttempts(t, f.m, run.ID))

	paused := admit(t, f.m, "run-open", f.clock.Now())
	opener := f.executor("worker-open")
	opener.OnCheckpoint = func(cp Checkpoint) error {
		if cp.StepID == "specialist-a" {
			return ErrWorkerStopped
		}
		return nil
	}
	if _, err := opener.Run(ctx, paused.ID, TwoSpecialistPlan(paused.ID), handler); !errors.Is(err, ErrWorkerStopped) {
		t.Fatal(err)
	}
	writeRun := admit(t, f.m, "run-uncertain", f.clock.Now())
	writePlan := Plan{Steps: []Step{{ID: "send", Kind: KindWrite, InvocationID: writeRun.ID + ":send", SideEffect: SideEffectWrite}}}
	if _, err := f.executor("worker-write").Run(ctx, writeRun.ID, writePlan, HandlerFunc(func(context.Context, Step, Progress) (json.RawMessage, error) {
		t.Fatal("write handler called")
		return nil, nil
	})); !errors.Is(err, ErrProtectedWrite) {
		t.Fatal(err)
	}

	f.clock.Advance(2 * time.Hour)
	stats, err := f.m.ApplyRetention(ctx, RetentionPolicy{TerminalHorizon: time.Hour, KeepLatestCheckpoints: 1}, f.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if stats.CheckpointsRemoved != 2 {
		t.Fatalf("removed %d", stats.CheckpointsRemoved)
	}
	left, err := f.m.Checkpoints(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].StepID != "finish" {
		t.Fatalf("kept %+v", left)
	}
	progress := decodeProgress(left[0].Payload)
	if !containsStep(progress.CompletedSteps, "specialist-a") || !containsStep(progress.CompletedSteps, "specialist-b") || !containsStep(progress.CompletedSteps, "finish") {
		t.Fatalf("latest payload lost progress %+v", progress.CompletedSteps)
	}
	if len(mustAttempts(t, f.m, run.ID)) != attemptsBefore {
		t.Fatal("retention deleted attempt history")
	}
	openCheckpoints, err := f.m.Checkpoints(ctx, paused.ID)
	if err != nil || len(openCheckpoints) != 1 {
		t.Fatalf("open checkpoints %+v err=%v", openCheckpoints, err)
	}
	uncertain := 0
	for _, att := range mustAttempts(t, f.m, writeRun.ID) {
		if att.State == AttemptUncertain {
			uncertain++
		}
	}
	if uncertain != 1 {
		t.Fatal("uncertain attempt was dropped")
	}
	replay, created, err := f.m.Admit(ctx, AdmitRequest{
		IdempotencyKey: "key-run-retain",
		Run:            contracts.Run{ID: "run-retain", AgentID: "orchestrator", Version: "1", Input: json.RawMessage(`{"job":"demo"}`)},
		Now:            f.clock.Now(),
	})
	if err != nil || created || replay.ID != run.ID {
		t.Fatalf("idempotency after retention created=%v err=%v id=%s", created, err, replay.ID)
	}
}

func TestOrderedEventsUnderConcurrentAppend(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-events", f.clock.Now())
	const extra = 20
	var wg sync.WaitGroup
	errCh := make(chan error, extra)
	for i := 0; i < extra; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.m.Append(ctx, contracts.RunEvent{RunID: run.ID, Type: "NOTE", Data: json.RawMessage(`{"ok":true}`)})
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	events, err := f.m.List(ctx, run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertOrdered(t, events)
	if len(events) != extra+1 {
		t.Fatalf("events %d", len(events))
	}
}

func TestUpdateWhileLeaseHeld(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-update", f.clock.Now())
	now := f.clock.Now()
	lease, err := f.m.ClaimRun(ctx, run.ID, "worker-a", time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	current := mustGet(t, f.m, run.ID)
	current.UpdatedAt = now
	if err := f.m.Update(ctx, current); !errors.Is(err, ErrLeaseHeld) {
		t.Fatal(err)
	}
	if err := f.m.ReleaseLease(ctx, lease, now); err != nil {
		t.Fatal(err)
	}
	if err := f.m.Update(ctx, current); err != nil {
		t.Fatal(err)
	}
	current.State = contracts.StatePending
	if err := f.m.Update(ctx, current); err == nil {
		t.Fatal("running run accepted a transition back to pending")
	}
}

func TestTerminalSnapshotStaysPut(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-terminal", f.clock.Now())
	handler := HandlerFunc(func(_ context.Context, step Step, _ Progress) (json.RawMessage, error) {
		if step.ID == "finish" {
			return json.RawMessage(`{"done":true}`), nil
		}
		return specialistOutput(step.ID), nil
	})
	if _, err := f.executor("worker-a").Run(ctx, run.ID, TwoSpecialistPlan(run.ID), handler); err != nil {
		t.Fatal(err)
	}
	stored := mustGet(t, f.m, run.ID)
	if stored.State != contracts.StateSucceeded {
		t.Fatalf("state %s", stored.State)
	}
	if err := f.m.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}
	changed := stored
	changed.Output = json.RawMessage(`{"done":false}`)
	if err := f.m.Update(ctx, changed); err == nil {
		t.Fatal("succeeded run accepted a new output")
	}
	if got := mustGet(t, f.m, run.ID); string(got.Output) != `{"done":true}` {
		t.Fatalf("output changed to %s", got.Output)
	}
}

func TestMissingRun(t *testing.T) {
	m := NewMemory()
	_, err := m.Get(context.Background(), "missing")
	var runErr *contracts.RunError
	if !errors.As(err, &runErr) || runErr.Code != contracts.ErrNotFound {
		t.Fatal(err)
	}
}
