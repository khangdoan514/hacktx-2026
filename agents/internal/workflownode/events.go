package workflownode

import (
	"context"
	"sync"
)

// MemoryLog is an in-memory node event log. Sequence numbers are assigned per
// workflow run, starting at 1, and the sequence on the supplied event is ignored.
// List returns a copy. Append records the cancellation event even when the
// caller's context is already cancelled; the audit row is the point of the write.
type MemoryLog struct {
	mu     sync.Mutex
	seq    map[string]uint64
	events map[string][]Event
}

// NewMemoryLog returns an empty log.
func NewMemoryLog() *MemoryLog {
	return &MemoryLog{
		seq:    map[string]uint64{},
		events: map[string][]Event{},
	}
}

// Append assigns the next sequence for the event's workflow run.
func (l *MemoryLog) Append(_ context.Context, event Event) (Event, error) {
	if event.Correlation.WorkflowRunID == "" {
		return Event{}, &Error{Code: CodeInvalidInput, Message: "event requires a workflow run id"}
	}
	if event.Type == "" {
		return Event{}, &Error{Code: CodeInvalidInput, Message: "event requires a type"}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq[event.Correlation.WorkflowRunID]++
	event.Sequence = l.seq[event.Correlation.WorkflowRunID]
	event.Data = cloneRaw(event.Data)
	l.events[event.Correlation.WorkflowRunID] = append(l.events[event.Correlation.WorkflowRunID], event)
	return event, nil
}

// List returns events for workflowRunID with Sequence > after, in increasing order.
// A run with no events yields an empty slice.
func (l *MemoryLog) List(_ context.Context, workflowRunID string, after uint64) ([]Event, error) {
	if workflowRunID == "" {
		return nil, &Error{Code: CodeInvalidInput, Message: "workflow run id is required"}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	src := l.events[workflowRunID]
	out := make([]Event, 0, len(src))
	for _, event := range src {
		if event.Sequence <= after {
			continue
		}
		event.Data = cloneRaw(event.Data)
		out = append(out, event)
	}
	return out, nil
}
