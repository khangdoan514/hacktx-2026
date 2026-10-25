package workflownode

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// record is the mutable invocation owned by Service.
type record struct {
	mu       sync.Mutex
	fp       string
	done     chan struct{}
	finished bool
	cancel   context.CancelFunc
	inv      Invocation
}

func (r *record) setCancel(fn context.CancelFunc) {
	r.mu.Lock()
	r.cancel = fn
	r.mu.Unlock()
}

func (r *record) cancelFunc() context.CancelFunc {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancel
}

func (r *record) doneAlready() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.finished
}

// pin records the published definition used for this invocation.
func (r *record) pin(def contracts.AgentDefinition) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return false
	}
	r.inv.AgentID = def.ID
	r.inv.AgentVersion = def.Version
	r.inv.OutputSchema = cloneRaw(def.OutputSchema)
	return true
}

func (r *record) markRunning(runID string, at time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return false
	}
	started := at.UTC()
	r.inv.State = contracts.StateRunning
	r.inv.Correlation.AgentRunID = runID
	r.inv.StartedAt = &started
	return true
}

func (r *record) complete(state contracts.State, output json.RawMessage, artifacts []ArtifactRef, terminal *Error, at time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return false
	}
	r.finished = true
	finished := at.UTC()
	r.inv.State = state
	r.inv.FinishedAt = &finished
	if state == contracts.StateSucceeded {
		r.inv.Output = cloneRaw(output)
		r.inv.OutputValidated = true
		r.inv.OutputArtifacts = cloneArtifacts(artifacts)
		r.inv.Error = nil
	} else {
		r.inv.Output = cloneRaw(output)
		r.inv.OutputValidated = false
		r.inv.OutputArtifacts = nil
		r.inv.Error = cloneErr(terminal)
	}
	close(r.done)
	return true
}

func (r *record) snapshot() Invocation {
	r.mu.Lock()
	defer r.mu.Unlock()
	inv := r.inv
	inv.Input = cloneRaw(r.inv.Input)
	inv.Output = cloneRaw(r.inv.Output)
	inv.OutputSchema = cloneRaw(r.inv.OutputSchema)
	inv.Artifacts = cloneArtifacts(r.inv.Artifacts)
	inv.OutputArtifacts = cloneArtifacts(r.inv.OutputArtifacts)
	inv.Approval = cloneApproval(r.inv.Approval)
	inv.Error = cloneErr(r.inv.Error)
	if r.inv.StartedAt != nil {
		started := r.inv.StartedAt.UTC()
		inv.StartedAt = &started
	}
	if r.inv.FinishedAt != nil {
		finished := r.inv.FinishedAt.UTC()
		inv.FinishedAt = &finished
	}
	return inv
}

func cloneRaw(m json.RawMessage) json.RawMessage {
	if len(m) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), m...)
}

func cloneArtifacts(in []ArtifactRef) []ArtifactRef {
	if len(in) == 0 {
		return nil
	}
	return append([]ArtifactRef(nil), in...)
}

func cloneApproval(g *ApprovalGate) *ApprovalGate {
	if g == nil {
		return nil
	}
	cp := *g
	return &cp
}

func cloneErr(e *Error) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	return &cp
}
