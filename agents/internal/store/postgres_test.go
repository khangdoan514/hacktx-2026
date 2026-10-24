package store

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

func TestTransitions(t *testing.T) {
	if transition(contracts.StateSucceeded, contracts.StateRunning) {
		t.Fatal("terminal regression allowed")
	}
	if transition(contracts.StatePending, contracts.StateSucceeded) {
		t.Fatal("pending skipped running")
	}
	if !transition(contracts.StateRunning, contracts.StateCancelled) {
		t.Fatal("cancellation denied")
	}
}

func TestPostgresRunAndOrderedEvents(t *testing.T) {
	url := os.Getenv("NMC3_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set NMC3_TEST_DATABASE_URL to run Supabase integration")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id := fmt.Sprintf("test-%d", time.Now().UnixNano())
	now := time.Now().UTC()
	run := contracts.Run{ID: id, State: contracts.StatePending, AgentID: "test", Version: "1.0.0", Input: []byte(`{}`), Invocations: []contracts.AgentInvocation{}, CreatedAt: now, UpdatedAt: now}
	if err := s.Create(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, run); err == nil {
		t.Fatal("duplicate create accepted")
	}
	const count = 20
	var wg sync.WaitGroup
	errCh := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Append(ctx, contracts.RunEvent{RunID: id, Type: "TEST"})
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
	events, err := s.List(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != count {
		t.Fatalf("got %d events", len(events))
	}
	for i, e := range events {
		if e.Sequence != uint64(i+1) {
			t.Fatalf("sequence %d at index %d", e.Sequence, i)
		}
	}
	run.State = contracts.StateRunning
	run.UpdatedAt = time.Now().UTC()
	if err := s.Update(ctx, run); err != nil {
		t.Fatal(err)
	}
	run.State = contracts.StateSucceeded
	run.UpdatedAt = time.Now().UTC()
	if err := s.Update(ctx, run); err != nil {
		t.Fatal(err)
	}
	// A fresh pool represents the API's next process after restart.
	fresh, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	stored, err := fresh.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != contracts.StateSucceeded {
		t.Fatalf("state %s", stored.State)
	}
	after, err := fresh.List(ctx, id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 10 || after[0].Sequence != 11 {
		t.Fatal("event cursor failed")
	}
	run.State = contracts.StateRunning
	if err := fresh.Update(ctx, run); err == nil {
		t.Fatal("terminal regression accepted")
	}
}
