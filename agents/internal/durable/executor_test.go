package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

type manualClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type fixture struct {
	m     *Memory
	clock *manualClock
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return &fixture{
		m:     NewMemory(),
		clock: &manualClock{t: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)},
	}
}

func (f *fixture) executor(worker string) *Executor {
	return &Executor{
		Store:           f.m,
		WorkerID:        worker,
		LeaseTTL:        time.Minute,
		MaxReadAttempts: 2,
		Now:             f.clock.Now,
	}
}

type script struct {
	mu    sync.Mutex
	calls map[string]int
	fn    func(step Step, n int, progress Progress) (json.RawMessage, error)
}

func (s *script) Execute(_ context.Context, step Step, progress Progress) (json.RawMessage, error) {
	s.mu.Lock()
	if s.calls == nil {
		s.calls = map[string]int{}
	}
	s.calls[step.ID]++
	n := s.calls[step.ID]
	s.mu.Unlock()
	return s.fn(step, n, progress)
}

func (s *script) count(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[id]
}

func admit(t *testing.T, m *Memory, id string, now time.Time) contracts.Run {
	t.Helper()
	run, created, err := m.Admit(context.Background(), AdmitRequest{
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

func mustGet(t *testing.T, m *Memory, id string) contracts.Run {
	t.Helper()
	run, err := m.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func specialistOutput(name string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"specialist": name})
	return raw
}

func assertOrdered(t *testing.T, events []contracts.RunEvent) {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("no events")
	}
	for i, event := range events {
		if event.Sequence != uint64(i+1) {
			t.Fatalf("sequence %d at index %d", event.Sequence, i)
		}
		if event.Source != "nmc3" && event.Source != "" {
			t.Fatalf("source %s", event.Source)
		}
	}
}

func countEvents(events []contracts.RunEvent, typ, invocation string) int {
	n := 0
	for _, event := range events {
		if event.Type != typ {
			continue
		}
		if invocation != "" && (event.InvocationID == nil || *event.InvocationID != invocation) {
			continue
		}
		n++
	}
	return n
}

func TestRestartTwoSpecialistsFromCheckpoint(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-restart", f.clock.Now())
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
	first := f.executor("worker-a")
	first.OnCheckpoint = func(cp Checkpoint) error {
		if cp.StepID == "specialist-a" {
			return ErrWorkerStopped
		}
		return nil
	}
	stopped, err := first.Run(ctx, run.ID, plan, work)
	if !errors.Is(err, ErrWorkerStopped) {
		t.Fatal(err)
	}
	if stopped.State != contracts.StateRunning {
		t.Fatalf("stop marked the run %s", stopped.State)
	}
	if work.count("specialist-a") != 1 || work.count("specialist-b") != 0 || work.count("finish") != 0 {
		t.Fatalf("calls after stop %#v", work.calls)
	}

	second := f.executor("worker-b")
	if _, err := second.Run(ctx, run.ID, plan, work); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("live lease: %v", err)
	}
	f.clock.Advance(2 * time.Minute)
	if _, err := second.Run(ctx, run.ID, plan, work); !errors.Is(err, ErrNeedsRecovery) {
		t.Fatalf("expired lease: %v", err)
	}
	if got := mustGet(t, f.m, run.ID); got.State != contracts.StateRunning {
		t.Fatalf("Run after expiry changed state to %s", got.State)
	}
	if work.count("specialist-b") != 0 {
		t.Fatal("expired Run executed a specialist")
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
		t.Fatalf("finish did not keep recorded progress: %s", finished.Output)
	}
	if _, err := f.executor("worker-c").Recover(ctx, run.ID, plan, work); err != nil {
		t.Fatal(err)
	}
	if work.count("specialist-a") != 1 || work.count("specialist-b") != 1 || work.count("finish") != 1 {
		t.Fatal("completed run executed again")
	}
	events, err := f.m.List(ctx, run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertOrdered(t, events)
	again, err := f.m.List(ctx, run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(events) || again[len(again)-1].Sequence != events[len(events)-1].Sequence {
		t.Fatal("event listing changed")
	}
	if countEvents(events, EventInvocationCompleted, aID) != 1 || countEvents(events, EventInvocationCompleted, bID) != 1 {
		t.Fatal("invocation completion was not unique")
	}
	if countEvents(events, EventRunCompleted, "") != 1 {
		t.Fatal("run completion was not unique")
	}
	replay, err := second.Replay(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay.Checkpoints) < 3 || replay.Checkpoints[0].Sequence != 1 {
		t.Fatalf("checkpoints %+v", replay.Checkpoints)
	}
	for i := 1; i < len(replay.Checkpoints); i++ {
		if replay.Checkpoints[i].Sequence <= replay.Checkpoints[i-1].Sequence {
			t.Fatal("checkpoint sequence went backwards")
		}
	}
}

func TestInFlightAbandonIsNotSuccess(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-abandon", f.clock.Now())
	plan := TwoSpecialistPlan(run.ID)
	bID := run.ID + ":specialist-b"
	work := &script{calls: map[string]int{}, fn: func(step Step, n int, _ Progress) (json.RawMessage, error) {
		switch step.ID {
		case "specialist-a":
			return specialistOutput("a"), nil
		case "specialist-b":
			if n == 1 {
				return nil, ErrWorkerStopped
			}
			got := mustGet(t, f.m, run.ID)
			if got.State == contracts.StateSucceeded {
				t.Fatal("abandoned run was marked successful before the retry finished")
			}
			return specialistOutput("b"), nil
		case "finish":
			return json.RawMessage(`{"done":true}`), nil
		default:
			return nil, errors.New("unexpected step")
		}
	}}
	stopped, err := f.executor("worker-a").Run(ctx, run.ID, plan, work)
	if !errors.Is(err, ErrWorkerStopped) {
		t.Fatal(err)
	}
	if stopped.State != contracts.StateRunning {
		t.Fatalf("state after crash %s", stopped.State)
	}
	attempts, err := f.m.Attempts(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var running bool
	for _, att := range attempts {
		if att.InvocationID == bID && att.State == AttemptRunning {
			running = true
		}
		if att.InvocationID == bID && att.State == AttemptSucceeded {
			t.Fatal("in-flight attempt was marked successful")
		}
	}
	if !running {
		t.Fatal("expected a running attempt for specialist B")
	}
	if _, ok, err := f.m.Invocation(ctx, bID); err != nil || ok {
		t.Fatal("abandoned specialist recorded an outcome")
	}
	if _, err := f.executor("worker-b").Recover(ctx, run.ID, plan, work); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("recover before expiry: %v", err)
	}
	if got := mustGet(t, f.m, run.ID); got.State != contracts.StateRunning {
		t.Fatalf("early recover changed state to %s", got.State)
	}

	f.clock.Advance(2 * time.Minute)
	lease, abandoned, err := f.m.RecoverClaim(ctx, run.ID, "inspector", time.Minute, f.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(abandoned) != 1 || abandoned[0].State != AttemptAbandoned || abandoned[0].InvocationID != bID {
		t.Fatalf("abandoned %+v", abandoned)
	}
	if got := mustGet(t, f.m, run.ID); got.State != contracts.StateRunning {
		t.Fatalf("RecoverClaim marked the run %s", got.State)
	}
	for _, inv := range mustGet(t, f.m, run.ID).Invocations {
		if inv.ID == bID && inv.State == contracts.StateSucceeded {
			t.Fatal("invocation snapshot says the abandoned specialist succeeded")
		}
	}
	if err := f.m.ReleaseLease(ctx, lease, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	finished, err := f.executor("worker-b").Recover(ctx, run.ID, plan, work)
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != contracts.StateSucceeded {
		t.Fatalf("state %s", finished.State)
	}
	if work.count("specialist-a") != 1 || work.count("specialist-b") != 2 {
		t.Fatalf("calls %#v", work.calls)
	}
	outcome, ok, err := f.m.Invocation(ctx, bID)
	if err != nil || !ok || outcome.State != contracts.StateSucceeded {
		t.Fatalf("outcome ok=%v err=%v %+v", ok, err, outcome)
	}
	successes := 0
	for _, att := range mustAttempts(t, f.m, run.ID) {
		if att.InvocationID == bID && att.State == AttemptSucceeded {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful B attempts %d", successes)
	}
}

func TestCancellationSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-cancel", f.clock.Now())
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
	first := f.executor("worker-a")
	first.OnCheckpoint = func(cp Checkpoint) error {
		if cp.StepID == "specialist-a" {
			return errors.New("killed")
		}
		return nil
	}
	if _, err := first.Run(ctx, run.ID, plan, work); !errors.Is(err, ErrWorkerStopped) {
		t.Fatal(err)
	}
	cancel, err := f.executor("api").Cancel(ctx, run.ID, "user stopped the run")
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.m.Cancel(ctx, run.ID, "second reason", f.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !again.RequestedAt.Equal(cancel.RequestedAt) || again.Reason != cancel.Reason {
		t.Fatal("cancellation was not idempotent")
	}
	events, err := f.m.List(ctx, run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if countEvents(events, EventCancellationRequested, "") != 1 {
		t.Fatal("cancellation event repeated")
	}
	if got := mustGet(t, f.m, run.ID); got.State != contracts.StateCancelled {
		t.Fatalf("state %s", got.State)
	}
	if _, err := f.executor("worker-b").Recover(ctx, run.ID, plan, work); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("recover while the dead lease is live: %v", err)
	}
	f.clock.Advance(2 * time.Minute)
	resumed, err := f.executor("worker-b").Recover(ctx, run.ID, plan, work)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.State != contracts.StateCancelled {
		t.Fatalf("restarted state %s", resumed.State)
	}
	if work.count("specialist-a") != 1 || work.count("specialist-b") != 0 || work.count("finish") != 0 {
		t.Fatalf("calls after cancelled restart %#v", work.calls)
	}
	record, ok, err := f.m.Cancellation(ctx, run.ID)
	if err != nil || !ok || record.Reason != "user stopped the run" {
		t.Fatalf("cancellation record ok=%v err=%v %+v", ok, err, record)
	}
	if _, err := f.executor("worker-c").Run(ctx, run.ID, plan, work); err != nil {
		t.Fatal(err)
	}
	if work.count("specialist-b") != 0 {
		t.Fatal("Run started work on a cancelled run")
	}
}

func TestLiveCancellationStopsNextStep(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-live-cancel", f.clock.Now())
	work := &script{calls: map[string]int{}, fn: func(step Step, _ int, _ Progress) (json.RawMessage, error) {
		if step.ID == "specialist-a" {
			if _, err := f.m.Cancel(ctx, run.ID, "stop", f.clock.Now()); err != nil {
				return nil, err
			}
			return specialistOutput("a"), nil
		}
		return json.RawMessage(`{"ran":true}`), nil
	}}
	got, err := f.executor("worker-a").Run(ctx, run.ID, TwoSpecialistPlan(run.ID), work)
	if !errors.Is(err, ErrCancelled) {
		t.Fatal(err)
	}
	if got.State != contracts.StateCancelled {
		t.Fatalf("state %s", got.State)
	}
	if work.count("specialist-a") != 1 || work.count("specialist-b") != 0 || work.count("finish") != 0 {
		t.Fatalf("calls %#v", work.calls)
	}
	outcome, ok, err := f.m.Invocation(ctx, run.ID+":specialist-a")
	if err != nil || !ok || outcome.State != contracts.StateSucceeded {
		t.Fatal("in-flight specialist result was dropped")
	}
}

func TestReadLostResponseRecordsSecondAttempt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-read", f.clock.Now())
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
	finished, err := f.executor("worker-a").Run(ctx, run.ID, plan, handler)
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
	for _, att := range mustAttempts(t, f.m, run.ID) {
		if att.InvocationID != toolInvocation {
			continue
		}
		switch att.State {
		case AttemptFailed:
			failed++
			if att.AttemptNumber != 1 || att.Error == nil || att.Error.Code != contracts.ErrTimeout {
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
	if _, ok, err := f.m.Invocation(ctx, toolInvocation); err != nil || !ok {
		t.Fatal("read invocation missing")
	}
	if len(finished.ToolInvocations) != 2 {
		t.Fatalf("tool rows %d", len(finished.ToolInvocations))
	}
	events, err := f.m.List(ctx, run.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertOrdered(t, events)
	if countEvents(events, EventAttemptStarted, toolInvocation) != 2 {
		t.Fatal("expected two recorded read attempts")
	}
	if countEvents(events, EventInvocationCompleted, toolInvocation) != 1 {
		t.Fatal("read invocation completed more than once")
	}
}

func TestReadRetriesStopAtBound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-read-bound", f.clock.Now())
	reads := 0
	handler := HandlerFunc(func(_ context.Context, step Step, _ Progress) (json.RawMessage, error) {
		if step.Kind == KindReadTool {
			reads++
			return nil, ErrLostResponse
		}
		t.Fatal("finish ran after exhausted reads")
		return nil, nil
	})
	worker := f.executor("worker-a")
	worker.MaxReadAttempts = 2
	got, err := worker.Run(ctx, run.ID, ReadToolPlan(run.ID, "catalog"), handler)
	if !errors.Is(err, ErrRetriesExhausted) {
		t.Fatal(err)
	}
	if got.State != contracts.StateFailed {
		t.Fatalf("state %s", got.State)
	}
	if reads != 2 {
		t.Fatalf("reads %d", reads)
	}
	if _, err := worker.Recover(ctx, run.ID, ReadToolPlan(run.ID, "catalog"), handler); err != nil {
		t.Fatal(err)
	}
	if reads != 2 {
		t.Fatal("failed run retried the read")
	}
}

func TestUncertainReadIsNotRetried(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-unknown", f.clock.Now())
	calls := 0
	handler := HandlerFunc(func(_ context.Context, step Step, _ Progress) (json.RawMessage, error) {
		calls++
		if step.Kind == KindReadTool {
			return nil, ErrOutcomeUnknown
		}
		return nil, errors.New("finish should not run")
	})
	worker := f.executor("worker-a")
	got, err := worker.Run(ctx, run.ID, ReadToolPlan(run.ID, "catalog"), handler)
	if !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	if got.State == contracts.StateSucceeded || got.State == contracts.StateFailed {
		t.Fatalf("uncertain run became %s", got.State)
	}
	uncertain := 0
	for _, att := range mustAttempts(t, f.m, run.ID) {
		if att.State == AttemptUncertain {
			uncertain++
		}
	}
	if uncertain != 1 || calls != 1 {
		t.Fatalf("uncertain=%d calls=%d", uncertain, calls)
	}
	if _, err := worker.Run(ctx, run.ID, ReadToolPlan(run.ID, "catalog"), handler); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("uncertain operation was retried")
	}
}

func TestProtectedWriteIsNotDispatched(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-write", f.clock.Now())
	plan := Plan{Steps: []Step{
		{ID: "send", Kind: KindWrite, InvocationID: run.ID + ":send", SideEffect: SideEffectWrite},
		{ID: "finish", Kind: KindFinish, InvocationID: RootInvocationID(run.ID), SideEffect: SideEffectNone},
	}}
	handler := HandlerFunc(func(context.Context, Step, Progress) (json.RawMessage, error) {
		t.Fatal("protected write reached a handler")
		return nil, nil
	})
	worker := f.executor("worker-a")
	got, err := worker.Run(ctx, run.ID, plan, handler)
	if !errors.Is(err, ErrProtectedWrite) {
		t.Fatal(err)
	}
	if got.State == contracts.StateSucceeded {
		t.Fatal("refused write completed the run")
	}
	found := false
	for _, att := range mustAttempts(t, f.m, run.ID) {
		if att.SideEffect == SideEffectWrite && att.State == AttemptUncertain {
			found = true
		}
	}
	if !found {
		t.Fatal("write was not recorded as uncertain")
	}
	if _, err := worker.Recover(ctx, run.ID, plan, handler); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
}

func TestSpecialistErrorFailsWithoutRetry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-fail", f.clock.Now())
	calls := map[string]int{}
	handler := HandlerFunc(func(_ context.Context, step Step, _ Progress) (json.RawMessage, error) {
		calls[step.ID]++
		if step.ID == "specialist-a" {
			return nil, errors.New("boom")
		}
		return json.RawMessage(`{}`), nil
	})
	got, err := f.executor("worker-a").Run(ctx, run.ID, TwoSpecialistPlan(run.ID), handler)
	if err == nil || got.State != contracts.StateFailed {
		t.Fatalf("err=%v state=%s", err, got.State)
	}
	if calls["specialist-a"] != 1 || calls["specialist-b"] != 0 {
		t.Fatalf("calls %#v", calls)
	}
}

func TestConcurrentWorkersCannotClaimActiveAttempt(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := admit(t, f.m, "run-claim", f.clock.Now())
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	work := &script{calls: map[string]int{}, fn: func(step Step, n int, _ Progress) (json.RawMessage, error) {
		if step.ID == "specialist-a" && n == 1 {
			once.Do(func() { close(entered) })
			<-release
		}
		if step.ID == "finish" {
			return json.RawMessage(`{"done":true}`), nil
		}
		return specialistOutput(step.ID), nil
	}}
	results := make(chan error, 2)
	for _, worker := range []string{"worker-a", "worker-b"} {
		worker := worker
		go func() {
			_, err := f.executor(worker).Run(ctx, run.ID, TwoSpecialistPlan(run.ID), work)
			results <- err
		}()
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("no worker entered the specialist")
	}
	select {
	case err := <-results:
		if !errors.Is(err, ErrLeaseHeld) {
			t.Fatalf("loser: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second worker did not return while the attempt was active")
	}
	close(release)
	select {
	case err := <-results:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lease holder did not finish")
	}
	if work.count("specialist-a") != 1 || work.count("specialist-b") != 1 || work.count("finish") != 1 {
		t.Fatalf("calls %#v", work.calls)
	}
	if got := mustGet(t, f.m, run.ID); got.State != contracts.StateSucceeded {
		t.Fatalf("state %s", got.State)
	}
}

func mustAttempts(t *testing.T, m *Memory, runID string) []Attempt {
	t.Helper()
	attempts, err := m.Attempts(context.Background(), runID)
	if err != nil {
		t.Fatal(err)
	}
	return attempts
}
