package workflownode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// Service admits workflow node invocations against a pinned agent version.
type Service struct {
	catalog  Catalog
	executor Executor
	events   EventLog
	now      func() time.Time
	mu       sync.Mutex
	runs     map[string]*record
}

type eventBody struct {
	AgentID      string `json:"agent_id,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
	Action       string `json:"action,omitempty"`
	Name         string `json:"name,omitempty"`
	Code         string `json:"code,omitempty"`
}

type finger struct {
	WorkflowID       string           `json:"workflow_id"`
	WorkflowVersion  string           `json:"workflow_version"`
	WorkflowRunID    string           `json:"workflow_run_id"`
	NodeID           string           `json:"node_id"`
	NodeInvocationID string           `json:"node_invocation_id"`
	TraceID          string           `json:"trace_id"`
	AgentID          string           `json:"agent_id"`
	AgentVersion     string           `json:"agent_version"`
	TenantID         string           `json:"tenant_id"`
	PrincipalID      string           `json:"principal_id"`
	PolicyID         string           `json:"policy_id"`
	PolicyVersion    string           `json:"policy_version,omitempty"`
	Input            string           `json:"input"`
	Artifacts        []ArtifactRef    `json:"artifacts,omitempty"`
	Limits           contracts.Limits `json:"limits"`
	Approval         *ApprovalGate    `json:"approval,omitempty"`
}

// New returns a node service. now may be nil, in which case the service uses time.Now.
func New(catalog Catalog, executor Executor, events EventLog, now func() time.Time) (*Service, error) {
	if catalog == nil || executor == nil || events == nil {
		return nil, &Error{Code: CodeInvalidInput, Message: "catalog, executor, and event log are required"}
	}
	if now == nil {
		now = time.Now
	}
	return &Service{
		catalog:  catalog,
		executor: executor,
		events:   events,
		now:      now,
		runs:     map[string]*record{},
	}, nil
}

// Invoke runs one pinned agent version for one workflow node.
// A repeated call with the same workflow run and node invocation id returns
// the original invocation and does not open another agent run. A repeated
// call that changes the request returns CONFLICT.
// Cancelling ctx before the agent run opens marks the invocation cancelled
// and does not open a session. Use Cancel to cancel from the workflow while
// Invoke is in progress.
func (s *Service) Invoke(ctx context.Context, req InvokeRequest) (Invocation, error) {
	if ctx == nil {
		return Invocation{}, &Error{Code: CodeInvalidInput, Message: "context is required"}
	}
	norm, fp, err := normalize(req)
	if err != nil {
		return Invocation{}, err
	}
	rec, owned, err := s.begin(norm, fp)
	if err != nil {
		return Invocation{}, err
	}
	if !owned {
		if rec.doneAlready() {
			return resultOf(rec)
		}
		select {
		case <-rec.done:
			return resultOf(rec)
		case <-ctx.Done():
			snap := rec.snapshot()
			if snap.State == contracts.StateCancelled {
				return snap, snap.Error
			}
			return snap, &Error{Code: CodeCancelled, Message: "caller cancelled while waiting for the invocation"}
		}
	}
	return s.execute(ctx, rec, norm)
}

// Cancel marks an in-flight invocation cancelled and stops new actions.
// A terminal invocation is returned unchanged. The caller context does not
// have to be the Invoke context; Cancel is the workflow's cancellation signal.
func (s *Service) Cancel(ctx context.Context, workflowRunID, nodeInvocationID string) (Invocation, error) {
	if ctx == nil {
		return Invocation{}, &Error{Code: CodeInvalidInput, Message: "context is required"}
	}
	if workflowRunID == "" || nodeInvocationID == "" {
		return Invocation{}, &Error{Code: CodeInvalidInput, Message: "workflow run id and node invocation id are required"}
	}
	s.mu.Lock()
	rec := s.runs[runKey(workflowRunID, nodeInvocationID)]
	s.mu.Unlock()
	if rec == nil {
		return Invocation{}, &Error{Code: CodeNotFound, Message: "workflow node invocation not found"}
	}
	won := rec.complete(contracts.StateCancelled, nil, nil, cancelledErr(), s.now())
	if cancel := rec.cancelFunc(); cancel != nil {
		cancel()
	}
	if won {
		if err := s.emit(context.WithoutCancel(ctx), rec, EventCancelled, eventBody{Code: string(CodeCancelled)}); err != nil {
			return rec.snapshot(), &Error{Code: CodeInternal, Message: "could not record workflow node event"}
		}
		return resultOf(rec)
	}
	return rec.snapshot(), nil
}

// Get returns the current invocation snapshot.
func (s *Service) Get(ctx context.Context, workflowRunID, nodeInvocationID string) (Invocation, error) {
	if ctx == nil {
		return Invocation{}, &Error{Code: CodeInvalidInput, Message: "context is required"}
	}
	if workflowRunID == "" || nodeInvocationID == "" {
		return Invocation{}, &Error{Code: CodeInvalidInput, Message: "workflow run id and node invocation id are required"}
	}
	s.mu.Lock()
	rec := s.runs[runKey(workflowRunID, nodeInvocationID)]
	s.mu.Unlock()
	if rec == nil {
		return Invocation{}, &Error{Code: CodeNotFound, Message: "workflow node invocation not found"}
	}
	return rec.snapshot(), nil
}

// Events lists node events for a workflow run with sequence greater than after.
func (s *Service) Events(ctx context.Context, workflowRunID string, after uint64) ([]Event, error) {
	if ctx == nil {
		return nil, &Error{Code: CodeInvalidInput, Message: "context is required"}
	}
	return s.events.List(ctx, workflowRunID, after)
}

// Join loads the named invocations and returns their outputs only when every
// one has succeeded schema validation. It does not schedule the nodes.
func (s *Service) Join(ctx context.Context, workflowRunID string, nodeInvocationIDs []string) (JoinedOutput, error) {
	if ctx == nil {
		return JoinedOutput{}, &Error{Code: CodeInvalidInput, Message: "context is required"}
	}
	if len(nodeInvocationIDs) == 0 {
		return JoinedOutput{}, &Error{Code: CodeInvalidInput, Message: "join requires the node invocations to combine"}
	}
	seen := make(map[string]struct{}, len(nodeInvocationIDs))
	invocations := make([]Invocation, 0, len(nodeInvocationIDs))
	for _, id := range nodeInvocationIDs {
		if id == "" {
			return JoinedOutput{}, &Error{Code: CodeInvalidInput, Message: "join requires invocation ids"}
		}
		if _, ok := seen[id]; ok {
			return JoinedOutput{}, &Error{Code: CodeInvalidInput, Message: "join requires distinct invocation ids"}
		}
		seen[id] = struct{}{}
		inv, err := s.Get(ctx, workflowRunID, id)
		if err != nil {
			return JoinedOutput{}, err
		}
		invocations = append(invocations, inv)
	}
	return JoinValidated(workflowRunID, invocations)
}

func (s *Service) execute(ctx context.Context, rec *record, req InvokeRequest) (inv Invocation, err error) {
	runCtx, cancel := context.WithCancel(ctx)
	rec.setCancel(cancel)
	defer cancel()
	defer func() {
		if rec.complete(contracts.StateFailed, nil, nil, &Error{Code: CodeInternal, Message: "invocation ended without a terminal state"}, s.now()) {
			_ = s.emit(context.WithoutCancel(ctx), rec, EventFailed, eventBody{Code: string(CodeInternal)})
			inv = rec.snapshot()
			err = inv.Error
		}
	}()

	if runCtx.Err() != nil {
		return s.settle(ctx, rec, contracts.StateCancelled, nil, nil, cancelledErr(), EventCancelled, eventBody{Code: string(CodeCancelled)})
	}
	if err := s.emit(context.WithoutCancel(ctx), rec, EventAccepted, eventBody{AgentID: req.AgentID, AgentVersion: req.AgentVersion}); err != nil {
		return s.failInternal(ctx, rec)
	}
	if snap, term, stop := s.stopped(rec); stop {
		return snap, term
	}

	def, resolveErr := s.catalog.Resolve(runCtx, req.AgentID, req.AgentVersion)
	if snap, term, stop := s.stopped(rec); stop {
		return snap, term
	}
	if resolveErr != nil {
		if errors.Is(resolveErr, context.Canceled) || runCtx.Err() != nil {
			return s.settle(ctx, rec, contracts.StateCancelled, nil, nil, cancelledErr(), EventCancelled, eventBody{Code: string(CodeCancelled)})
		}
		mapped := mapErr(resolveErr)
		if mapped.Code == CodeInternal {
			mapped = &Error{Code: CodeInternal, Message: "could not resolve the pinned agent version"}
		}
		return s.settle(ctx, rec, contracts.StateFailed, nil, nil, mapped, EventFailed, eventBody{Code: string(mapped.Code), AgentID: req.AgentID, AgentVersion: req.AgentVersion})
	}
	if def.ID != req.AgentID || def.Version != req.AgentVersion {
		return s.settle(ctx, rec, contracts.StateFailed, nil, nil, &Error{Code: CodeNotFound, Message: "pinned agent version is not published"}, EventFailed, eventBody{Code: string(CodeNotFound), AgentID: req.AgentID, AgentVersion: req.AgentVersion})
	}
	if !rec.pin(def) {
		return s.stoppedResult(rec)
	}
	if err := ValidateSchemaDocument(def.InputSchema); err != nil || ValidateSchemaDocument(def.OutputSchema) != nil {
		return s.settle(ctx, rec, contracts.StateFailed, nil, nil, &Error{Code: CodeInvalidInput, Message: "pinned agent schema is not supported"}, EventFailed, eventBody{Code: string(CodeInvalidInput), AgentID: def.ID, AgentVersion: def.Version})
	}
	if err := ValidateSchema(def.InputSchema, req.Input); err != nil {
		return s.settle(ctx, rec, contracts.StateFailed, nil, nil, &Error{Code: CodeInvalidInput, Message: "input does not match the pinned agent schema: " + err.Error()}, EventInputRejected, eventBody{Code: string(CodeInvalidInput), AgentID: def.ID, AgentVersion: def.Version})
	}
	if runCtx.Err() != nil {
		return s.settle(ctx, rec, contracts.StateCancelled, nil, nil, cancelledErr(), EventCancelled, eventBody{Code: string(CodeCancelled)})
	}

	session, openErr := s.executor.Open(runCtx, ExecuteRequest{
		Correlation: req.Correlation,
		Identity:    req.Identity,
		Definition:  def,
		Input:       cloneRaw(req.Input),
		Artifacts:   cloneArtifacts(req.Artifacts),
		Limits:      req.Limits,
		Approval:    cloneApproval(req.Approval),
	})
	if snap, term, stop := s.stopped(rec); stop {
		return snap, term
	}
	if openErr != nil {
		if errors.Is(openErr, context.Canceled) || runCtx.Err() != nil {
			return s.settle(ctx, rec, contracts.StateCancelled, nil, nil, cancelledErr(), EventCancelled, eventBody{Code: string(CodeCancelled)})
		}
		mapped := mapErr(openErr)
		return s.settle(ctx, rec, contracts.StateFailed, nil, nil, mapped, EventFailed, eventBody{Code: string(mapped.Code), AgentID: def.ID, AgentVersion: def.Version})
	}
	if session == nil || session.RunID() == "" {
		return s.settle(ctx, rec, contracts.StateFailed, nil, nil, &Error{Code: CodeInternal, Message: "agent run id is required"}, EventFailed, eventBody{Code: string(CodeInternal)})
	}
	if !rec.markRunning(session.RunID(), s.now()) {
		return s.stoppedResult(rec)
	}
	if err := s.emit(context.WithoutCancel(ctx), rec, EventStarted, eventBody{AgentID: def.ID, AgentVersion: def.Version}); err != nil {
		return s.failInternal(ctx, rec)
	}
	return s.loop(runCtx, rec, session)
}

func (s *Service) loop(ctx context.Context, rec *record, session Session) (Invocation, error) {
	for {
		if snap, term, stop := s.stopped(rec); stop {
			return snap, term
		}
		if ctx.Err() != nil {
			return s.settle(ctx, rec, contracts.StateCancelled, nil, nil, cancelledErr(), EventCancelled, eventBody{Code: string(CodeCancelled)})
		}
		action, err := session.Next(ctx)
		if snap, term, stop := s.stopped(rec); stop {
			return snap, term
		}
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return s.settle(ctx, rec, contracts.StateCancelled, nil, nil, cancelledErr(), EventCancelled, eventBody{Code: string(CodeCancelled)})
			}
			mapped := mapErr(err)
			return s.settle(ctx, rec, contracts.StateFailed, nil, nil, mapped, EventFailed, eventBody{Code: string(mapped.Code), Action: "NEXT"})
		}
		if ctx.Err() != nil || rec.doneAlready() {
			return s.settle(ctx, rec, contracts.StateCancelled, nil, nil, cancelledErr(), EventCancelled, eventBody{Code: string(CodeCancelled)})
		}

		switch action.Kind {
		case ActionGraphMutation:
			return s.reject(ctx, rec, action, &Error{Code: CodeGraphImmutable, Message: "an agent node cannot modify the published workflow graph"})
		case ActionApprovalDecision:
			return s.reject(ctx, rec, action, &Error{Code: CodeApprovalBypass, Message: "an agent node cannot decide or skip a workflow approval"})
		case ActionPolicyOverride:
			return s.reject(ctx, rec, action, &Error{Code: CodePolicyImmutable, Message: "an agent node cannot replace the policy identity"})
		case ActionFinish:
			return s.finish(ctx, rec, session, action)
		case ActionTool, ActionDelegate:
			if err := callShape(action); err != nil {
				return s.reject(ctx, rec, action, err)
			}
			if action.Kind == ActionTool && action.Protected && !gateAllows(rec.snapshot().Approval) {
				return s.reject(ctx, rec, action, &Error{Code: CodeApprovalRequired, Message: "protected action requires a workflow approval"})
			}
			if err := s.commit(ctx, rec, session, action); err != nil {
				return s.afterCommit(ctx, rec, err)
			}
		default:
			return s.reject(ctx, rec, action, &Error{Code: CodeInvalidInput, Message: "unknown agent action"})
		}
	}
}

func (s *Service) finish(ctx context.Context, rec *record, session Session, action ProposedAction) (Invocation, error) {
	if err := finishShape(action); err != nil {
		return s.reject(ctx, rec, action, err)
	}
	schema := rec.snapshot().OutputSchema
	if err := ValidateSchema(schema, action.Output); err != nil {
		return s.settle(ctx, rec, contracts.StateFailed, action.Output, nil, &Error{Code: CodeOutputSchemaInvalid, Message: "output does not match the pinned agent schema: " + err.Error()}, EventOutputRejected, eventBody{Code: string(CodeOutputSchemaInvalid), Action: string(ActionFinish)})
	}
	if err := validateArtifacts(action.Artifacts); err != nil {
		var nodeErr *Error
		if !errors.As(err, &nodeErr) {
			nodeErr = &Error{Code: CodeInvalidInput, Message: "output artifact reference is invalid"}
		}
		return s.reject(ctx, rec, action, nodeErr)
	}
	if ctx.Err() != nil || rec.doneAlready() {
		return s.settle(ctx, rec, contracts.StateCancelled, nil, nil, cancelledErr(), EventCancelled, eventBody{Code: string(CodeCancelled)})
	}
	if err := s.commit(ctx, rec, session, action); err != nil {
		return s.afterCommit(ctx, rec, err)
	}
	if snap, term, stop := s.stopped(rec); stop {
		return snap, term
	}
	snap := rec.snapshot()
	return s.settle(ctx, rec, contracts.StateSucceeded, action.Output, action.Artifacts, nil, EventSucceeded, eventBody{AgentID: snap.AgentID, AgentVersion: snap.AgentVersion, Action: string(ActionFinish)})
}

func (s *Service) commit(ctx context.Context, rec *record, session Session, action ProposedAction) error {
	if ctx.Err() != nil || rec.doneAlready() {
		return cancelledErr()
	}
	body := eventBody{Action: string(action.Kind), Name: action.Name}
	if action.Kind == ActionTool || action.Kind == ActionDelegate {
		if err := s.emit(context.WithoutCancel(ctx), rec, EventActionStarted, body); err != nil {
			return &Error{Code: CodeInternal, Message: "could not record workflow node event"}
		}
	}
	if ctx.Err() != nil || rec.doneAlready() {
		return cancelledErr()
	}
	if err := session.Commit(ctx, action); err != nil {
		return err
	}
	if rec.doneAlready() || ctx.Err() != nil {
		return cancelledErr()
	}
	return nil
}

func (s *Service) afterCommit(ctx context.Context, rec *record, err error) (Invocation, error) {
	if errors.Is(err, context.Canceled) || ctx.Err() != nil || rec.doneAlready() || codeOf(err) == CodeCancelled {
		return s.settle(ctx, rec, contracts.StateCancelled, nil, nil, cancelledErr(), EventCancelled, eventBody{Code: string(CodeCancelled)})
	}
	mapped := mapErr(err)
	return s.settle(ctx, rec, contracts.StateFailed, nil, nil, mapped, EventFailed, eventBody{Code: string(mapped.Code)})
}

func (s *Service) reject(ctx context.Context, rec *record, action ProposedAction, nodeErr *Error) (Invocation, error) {
	return s.settle(ctx, rec, contracts.StateFailed, nil, nil, nodeErr, EventActionRejected, eventBody{Action: string(action.Kind), Code: string(nodeErr.Code)})
}

func (s *Service) failInternal(ctx context.Context, rec *record) (Invocation, error) {
	return s.settle(ctx, rec, contracts.StateFailed, nil, nil, &Error{Code: CodeInternal, Message: "could not record workflow node event"}, EventFailed, eventBody{Code: string(CodeInternal)})
}

func (s *Service) settle(ctx context.Context, rec *record, state contracts.State, output json.RawMessage, artifacts []ArtifactRef, terminal *Error, eventType EventType, body eventBody) (Invocation, error) {
	if rec.complete(state, output, artifacts, terminal, s.now()) {
		if err := s.emit(context.WithoutCancel(ctx), rec, eventType, body); err != nil {
			return rec.snapshot(), &Error{Code: CodeInternal, Message: "could not record workflow node event"}
		}
	}
	return resultOf(rec)
}

func (s *Service) stopped(rec *record) (Invocation, error, bool) {
	if !rec.doneAlready() {
		return Invocation{}, nil, false
	}
	inv, err := resultOf(rec)
	return inv, err, true
}

func (s *Service) stoppedResult(rec *record) (Invocation, error) {
	return resultOf(rec)
}

func resultOf(rec *record) (Invocation, error) {
	snap := rec.snapshot()
	if snap.Error != nil {
		return snap, snap.Error
	}
	return snap, nil
}

func (s *Service) emit(ctx context.Context, rec *record, typ EventType, body eventBody) error {
	snap := rec.snapshot()
	var raw json.RawMessage
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if string(encoded) != "{}" {
		raw = encoded
	}
	_, err = s.events.Append(ctx, Event{
		Type:        typ,
		Timestamp:   s.now().UTC(),
		Correlation: snap.Correlation,
		Data:        raw,
	})
	return err
}

func (s *Service) begin(req InvokeRequest, fp string) (*record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := runKey(req.Correlation.WorkflowRunID, req.Correlation.NodeInvocationID)
	if existing, ok := s.runs[key]; ok {
		if existing.fp != fp {
			return nil, false, &Error{Code: CodeConflict, Message: "invocation id already exists with a different request"}
		}
		return existing, false, nil
	}
	rec := &record{
		fp:   fp,
		done: make(chan struct{}),
		inv: Invocation{
			ID:           req.Correlation.NodeInvocationID,
			Correlation:  req.Correlation,
			Identity:     req.Identity,
			AgentID:      req.AgentID,
			AgentVersion: req.AgentVersion,
			State:        contracts.StatePending,
			Input:        cloneRaw(req.Input),
			Artifacts:    cloneArtifacts(req.Artifacts),
			Limits:       req.Limits,
			Approval:     cloneApproval(req.Approval),
		},
	}
	s.runs[key] = rec
	return rec, true, nil
}

func normalize(req InvokeRequest) (InvokeRequest, string, error) {
	c := req.Correlation
	if c.WorkflowID == "" || c.WorkflowVersion == "" || c.WorkflowRunID == "" || c.NodeID == "" || c.NodeInvocationID == "" || c.TraceID == "" {
		return InvokeRequest{}, "", &Error{Code: CodeInvalidInput, Message: "correlation ids are required"}
	}
	if c.AgentRunID != "" {
		return InvokeRequest{}, "", &Error{Code: CodeInvalidInput, Message: "the agent service assigns the agent run id"}
	}
	if req.Identity.TenantID == "" || req.Identity.PrincipalID == "" || req.Identity.PolicyID == "" {
		return InvokeRequest{}, "", &Error{Code: CodeInvalidInput, Message: "policy identity requires a tenant, principal, and policy"}
	}
	if req.AgentID == "" || req.AgentVersion == "" {
		return InvokeRequest{}, "", &Error{Code: CodeInvalidInput, Message: "a pinned agent id and version are required"}
	}
	if err := validateArtifacts(req.Artifacts); err != nil {
		return InvokeRequest{}, "", err
	}
	if !positiveLimits(req.Limits) {
		return InvokeRequest{}, "", &Error{Code: CodeInvalidInput, Message: "limits must be positive"}
	}
	if err := validateGate(req.Approval); err != nil {
		return InvokeRequest{}, "", err
	}
	fpInput := string(req.Input)
	if json.Valid(req.Input) {
		var buf bytes.Buffer
		if err := json.Compact(&buf, req.Input); err == nil {
			fpInput = buf.String()
		}
	}
	req.Input = cloneRaw(req.Input)
	req.Artifacts = cloneArtifacts(req.Artifacts)
	req.Approval = cloneApproval(req.Approval)
	raw, err := json.Marshal(finger{
		WorkflowID:       c.WorkflowID,
		WorkflowVersion:  c.WorkflowVersion,
		WorkflowRunID:    c.WorkflowRunID,
		NodeID:           c.NodeID,
		NodeInvocationID: c.NodeInvocationID,
		TraceID:          c.TraceID,
		AgentID:          req.AgentID,
		AgentVersion:     req.AgentVersion,
		TenantID:         req.Identity.TenantID,
		PrincipalID:      req.Identity.PrincipalID,
		PolicyID:         req.Identity.PolicyID,
		PolicyVersion:    req.Identity.PolicyVersion,
		Input:            fpInput,
		Artifacts:        req.Artifacts,
		Limits:           req.Limits,
		Approval:         req.Approval,
	})
	if err != nil {
		return InvokeRequest{}, "", &Error{Code: CodeInvalidInput, Message: "invocation request is not comparable"}
	}
	return req, string(raw), nil
}

func validateGate(g *ApprovalGate) error {
	if g == nil {
		return nil
	}
	if g.ID == "" {
		return &Error{Code: CodeInvalidInput, Message: "workflow approval requires an id"}
	}
	switch g.State {
	case ApprovalPending, ApprovalDenied, ApprovalExpired:
		return nil
	case ApprovalApproved:
		if g.DecisionPrincipal == "" {
			return &Error{Code: CodeInvalidInput, Message: "an approved workflow gate requires the decision principal"}
		}
		return nil
	default:
		return &Error{Code: CodeInvalidInput, Message: "workflow approval state is invalid"}
	}
}

func gateAllows(g *ApprovalGate) bool {
	return g != nil && g.State == ApprovalApproved && g.ID != "" && g.DecisionPrincipal != ""
}

func finishShape(action ProposedAction) *Error {
	if action.Name != "" || len(bytes.TrimSpace(action.Input)) != 0 || action.Protected || len(bytes.TrimSpace(action.Detail)) != 0 {
		return &Error{Code: CodeInvalidInput, Message: "FINISH carries only output and artifact references"}
	}
	return nil
}

func callShape(action ProposedAction) *Error {
	if action.Name == "" || len(bytes.TrimSpace(action.Input)) == 0 || !json.Valid(action.Input) {
		return &Error{Code: CodeInvalidInput, Message: "action requires a name and JSON input"}
	}
	if len(bytes.TrimSpace(action.Output)) != 0 || len(action.Artifacts) != 0 || len(bytes.TrimSpace(action.Detail)) != 0 {
		return &Error{Code: CodeInvalidInput, Message: "tool and delegate actions cannot carry output, artifacts, or side-channel detail"}
	}
	if action.Kind == ActionDelegate && action.Protected {
		return &Error{Code: CodeInvalidInput, Message: "delegation is not a protected workflow action"}
	}
	return nil
}

func positiveLimits(limits contracts.Limits) bool {
	return limits.MaxDelegations > 0 && limits.MaxConcurrentAgents > 0 && limits.MaxToolCalls > 0 &&
		limits.MaxModelTurns > 0 && limits.MaxTokens > 0 && limits.MaxCostUSD > 0 &&
		!math.IsNaN(limits.MaxCostUSD) && !math.IsInf(limits.MaxCostUSD, 0) && limits.TimeoutSeconds > 0
}

func mapErr(err error) *Error {
	if err == nil {
		return nil
	}
	var nodeErr *Error
	if errors.As(err, &nodeErr) && nodeErr != nil {
		return cloneErr(nodeErr)
	}
	var runErr *contracts.RunError
	if errors.As(err, &runErr) && runErr != nil {
		return &Error{Code: Code(runErr.Code), Message: runErr.Message}
	}
	if errors.Is(err, context.Canceled) {
		return cancelledErr()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: CodeTimeout, Message: "workflow invocation timed out"}
	}
	return &Error{Code: CodeInternal, Message: "agent execution failed"}
}

func codeOf(err error) Code {
	var nodeErr *Error
	if errors.As(err, &nodeErr) && nodeErr != nil {
		return nodeErr.Code
	}
	return ""
}

func cancelledErr() *Error {
	return &Error{Code: CodeCancelled, Message: "workflow invocation cancelled"}
}

func runKey(workflowRunID, nodeInvocationID string) string {
	return workflowRunID + "\x00" + nodeInvocationID
}
