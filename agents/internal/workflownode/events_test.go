package workflownode

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMemoryLogAssignsSequencePerWorkflowRun(t *testing.T) {
	log := NewMemoryLog()
	ctx := context.Background()
	first, err := log.Append(ctx, Event{Sequence: 99, Type: EventAccepted, Timestamp: time.Unix(0, 0).UTC(), Correlation: Correlation{WorkflowRunID: "run-a"}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 {
		t.Fatalf("sequence %d", first.Sequence)
	}
	if _, err := log.Append(ctx, Event{Type: EventStarted, Correlation: Correlation{WorkflowRunID: "run-b"}}); err != nil {
		t.Fatal(err)
	}
	second, err := log.Append(ctx, Event{Type: EventSucceeded, Correlation: Correlation{WorkflowRunID: "run-a"}})
	if err != nil || second.Sequence != 2 {
		t.Fatalf("second %+v %v", second, err)
	}
	events, err := log.List(ctx, "run-a", 1)
	if err != nil || len(events) != 1 || events[0].Sequence != 2 {
		t.Fatalf("%+v %v", events, err)
	}
	empty, err := log.List(ctx, "missing", 0)
	if err != nil || len(empty) != 0 {
		t.Fatalf("%+v %v", empty, err)
	}
	if _, err := log.Append(ctx, Event{Type: EventAccepted}); err == nil {
		t.Fatal("accepted an event without a workflow run id")
	}
}

func TestMemoryLogConcurrentAppend(t *testing.T) {
	log := NewMemoryLog()
	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := log.Append(context.Background(), Event{Type: EventAccepted, Correlation: Correlation{WorkflowRunID: "run"}})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	events, err := log.List(context.Background(), "run", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != n {
		t.Fatalf("len %d", len(events))
	}
	for i, event := range events {
		if event.Sequence != uint64(i+1) {
			t.Fatalf("sequence %d at %d", event.Sequence, i)
		}
	}
}
