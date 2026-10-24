package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
)

type Memory struct {
	mu     sync.Mutex
	runs   map[string]contracts.Run
	events map[string][]contracts.RunEvent
}

func NewMemory() *Memory {
	return &Memory{runs: map[string]contracts.Run{}, events: map[string][]contracts.RunEvent{}}
}

func (m *Memory) Create(_ context.Context, run contracts.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[run.ID]; ok {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "run already exists"}
	}
	if run.Invocations == nil {
		run.Invocations = []contracts.AgentInvocation{}
	}
	if run.ToolInvocations == nil {
		run.ToolInvocations = []contracts.ToolInvocation{}
	}
	m.runs[run.ID] = run
	return nil
}

func (m *Memory) Get(_ context.Context, runID string) (contracts.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, ok := m.runs[runID]
	if !ok {
		return contracts.Run{}, &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	return cloneRun(run), nil
}

func (m *Memory) Update(_ context.Context, run contracts.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.runs[run.ID]
	if !ok {
		return &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	if !transitionOK(current.State, run.State) {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "invalid run transition"}
	}
	m.runs[run.ID] = run
	return nil
}

func (m *Memory) Append(_ context.Context, event contracts.RunEvent) (contracts.RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[event.RunID]; !ok {
		return contracts.RunEvent{}, &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	event.Sequence = uint64(len(m.events[event.RunID]) + 1)
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	m.events[event.RunID] = append(m.events[event.RunID], event)
	return event, nil
}

func (m *Memory) List(_ context.Context, runID string, after uint64) ([]contracts.RunEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[runID]; !ok {
		return nil, &contracts.RunError{Code: contracts.ErrNotFound, Message: "run not found"}
	}
	out := []contracts.RunEvent{}
	for _, event := range m.events[runID] {
		if event.Sequence > after {
			out = append(out, event)
		}
	}
	return out, nil
}

func transitionOK(from, to contracts.State) bool {
	if from == to {
		return true
	}
	switch from {
	case contracts.StatePending:
		return to == contracts.StateRunning || to == contracts.StateFailed || to == contracts.StateCancelled
	case contracts.StateRunning:
		return to == contracts.StateSucceeded || to == contracts.StateFailed || to == contracts.StateCancelled
	default:
		return false
	}
}

func cloneRun(run contracts.Run) contracts.Run {
	if run.Invocations == nil {
		run.Invocations = []contracts.AgentInvocation{}
	}
	if run.ToolInvocations == nil {
		run.ToolInvocations = []contracts.ToolInvocation{}
	}
	return run
}
