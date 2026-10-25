// Package workflownode is the contract an external workflow uses to invoke one
// pinned agent version. The workflow engine owns graph scheduling, joins, and
// approval placement; this package admits a single node invocation and checks
// the boundary the agent must not cross.
package workflownode

import (
	"context"
	"encoding/json"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// Code is a stable error identifier. The JSON shape matches the v1 error
// object: {"code","message"}. Messages are safe to show to a caller.
type Code string

const (
	CodeInvalidInput            Code = "INVALID_INPUT"
	CodeNotFound                Code = "NOT_FOUND"
	CodeConflict                Code = "CONFLICT"
	CodeOutputSchemaInvalid     Code = "OUTPUT_SCHEMA_INVALID"
	CodeGraphImmutable          Code = "GRAPH_IMMUTABLE"
	CodeApprovalRequired        Code = "APPROVAL_REQUIRED"
	CodeApprovalBypass          Code = "APPROVAL_BYPASS"
	CodePolicyImmutable         Code = "POLICY_IMMUTABLE"
	CodeCancelled               Code = "CANCELLED"
	CodeJoinNotReady            Code = "JOIN_NOT_READY"
	CodeJoinFailed              Code = "JOIN_FAILED"
	CodeInternal                Code = "INTERNAL"
	CodeTimeout                 Code = "TIMEOUT"
	CodeDelegationDepthExceeded Code = "DELEGATION_DEPTH_EXCEEDED"
)

// Error is a typed node-contract failure.
type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code) + ": " + e.Message
}

// Correlation binds one workflow node execution to the agent run it starts.
// The workflow allocates every field except AgentRunID, which the agent
// service assigns when the pinned run opens. IDs are opaque.
type Correlation struct {
	WorkflowID       string `json:"workflow_id"`
	WorkflowVersion  string `json:"workflow_version"`
	WorkflowRunID    string `json:"workflow_run_id"`
	NodeID           string `json:"node_id"`
	NodeInvocationID string `json:"node_invocation_id"`
	AgentRunID       string `json:"agent_run_id,omitempty"`
	TraceID          string `json:"trace_id"`
}

// PolicyIdentity is the tenant, principal, and policy the workflow presents.
// The node records this identity and does not accept a replacement from the agent.
type PolicyIdentity struct {
	TenantID      string `json:"tenant_id"`
	PrincipalID   string `json:"principal_id"`
	PolicyID      string `json:"policy_id"`
	PolicyVersion string `json:"policy_version,omitempty"`
}

// ArtifactRef points at stored bytes. Payloads and secrets stay in the
// artifact store; only the reference crosses this contract.
type ArtifactRef struct {
	ID             string `json:"id"`
	ObjectKey      string `json:"object_key"`
	ContentType    string `json:"content_type,omitempty"`
	Checksum       string `json:"checksum,omitempty"`
	ByteSize       int64  `json:"byte_size,omitempty"`
	Classification string `json:"classification,omitempty"`
}

// ApprovalState is the workflow's record of a gate. This package does not
// decide the gate.
type ApprovalState string

const (
	ApprovalPending  ApprovalState = "PENDING"
	ApprovalApproved ApprovalState = "APPROVED"
	ApprovalDenied   ApprovalState = "DENIED"
	ApprovalExpired  ApprovalState = "EXPIRED"
)

// ApprovalGate is a workflow-owned approval. An agent invocation cannot
// create, approve, deny, or skip one. A protected action is admitted only
// when the workflow passes a gate it has already marked APPROVED.
type ApprovalGate struct {
	ID                string        `json:"id"`
	State             ApprovalState `json:"state"`
	DecisionPrincipal string        `json:"decision_principal,omitempty"`
}

// ActionKind is one proposed step inside a pinned agent run.
type ActionKind string

const (
	ActionFinish           ActionKind = "FINISH"
	ActionTool             ActionKind = "TOOL"
	ActionDelegate         ActionKind = "DELEGATE"
	ActionGraphMutation    ActionKind = "GRAPH_MUTATION"
	ActionApprovalDecision ActionKind = "APPROVAL_DECISION"
	ActionPolicyOverride   ActionKind = "POLICY_OVERRIDE"
)

// ProposedAction is the next step an agent session wants to take. The node
// commits TOOL, DELEGATE, and schema-valid FINISH actions. It never commits a
// graph edit, an approval decision, or a policy override.
type ProposedAction struct {
	Kind      ActionKind      `json:"kind"`
	Output    json.RawMessage `json:"output,omitempty"`
	Artifacts []ArtifactRef   `json:"artifacts,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Protected bool            `json:"protected,omitempty"`
	Detail    json.RawMessage `json:"detail,omitempty"`
}

// InvokeRequest starts one node. AgentID and AgentVersion select the published
// definition to pin; a later publish does not change this call.
type InvokeRequest struct {
	Correlation  Correlation      `json:"correlation"`
	Identity     PolicyIdentity   `json:"identity"`
	AgentID      string           `json:"agent_id"`
	AgentVersion string           `json:"agent_version"`
	Input        json.RawMessage  `json:"input"`
	Artifacts    []ArtifactRef    `json:"artifacts,omitempty"`
	Limits       contracts.Limits `json:"limits"`
	Approval     *ApprovalGate    `json:"approval,omitempty"`
}

// Invocation is the inspectable record of one node call.
type Invocation struct {
	ID              string           `json:"id"`
	Correlation     Correlation      `json:"correlation"`
	Identity        PolicyIdentity   `json:"identity"`
	AgentID         string           `json:"agent_id"`
	AgentVersion    string           `json:"agent_version"`
	State           contracts.State  `json:"state"`
	Input           json.RawMessage  `json:"input"`
	Output          json.RawMessage  `json:"output,omitempty"`
	OutputSchema    json.RawMessage  `json:"output_schema,omitempty"`
	OutputValidated bool             `json:"output_validated,omitempty"`
	Error           *Error           `json:"error,omitempty"`
	Artifacts       []ArtifactRef    `json:"artifacts,omitempty"`
	OutputArtifacts []ArtifactRef    `json:"output_artifacts,omitempty"`
	Limits          contracts.Limits `json:"limits"`
	Approval        *ApprovalGate    `json:"approval,omitempty"`
	StartedAt       *time.Time       `json:"started_at,omitempty"`
	FinishedAt      *time.Time       `json:"finished_at,omitempty"`
}

// EventType is a workflow-facing node event. Tool and model events stay on
// the agent run identified by Correlation.AgentRunID.
type EventType string

const (
	EventAccepted       EventType = "NODE_ACCEPTED"
	EventStarted        EventType = "NODE_STARTED"
	EventInputRejected  EventType = "NODE_INPUT_REJECTED"
	EventActionStarted  EventType = "NODE_ACTION_STARTED"
	EventActionRejected EventType = "NODE_ACTION_REJECTED"
	EventOutputRejected EventType = "NODE_OUTPUT_REJECTED"
	EventSucceeded      EventType = "NODE_SUCCEEDED"
	EventFailed         EventType = "NODE_FAILED"
	EventCancelled      EventType = "NODE_CANCELLED"
)

// Event is one node-owned record. Sequence is assigned per workflow run.
// The supplied sequence on append is ignored.
type Event struct {
	Sequence    uint64          `json:"sequence"`
	Type        EventType       `json:"type"`
	Timestamp   time.Time       `json:"timestamp"`
	Correlation Correlation     `json:"correlation"`
	Data        json.RawMessage `json:"data,omitempty"`
}

// JoinedNode is one validated output inside a join result.
type JoinedNode struct {
	NodeID           string          `json:"node_id"`
	NodeInvocationID string          `json:"node_invocation_id"`
	AgentID          string          `json:"agent_id"`
	AgentVersion     string          `json:"agent_version"`
	AgentRunID       string          `json:"agent_run_id"`
	Output           json.RawMessage `json:"output"`
	Artifacts        []ArtifactRef   `json:"artifacts,omitempty"`
}

// JoinedOutput is produced only after every participating invocation has a
// schema-valid success. It is not a graph and it does not start work.
type JoinedOutput struct {
	WorkflowRunID   string       `json:"workflow_run_id"`
	WorkflowVersion string       `json:"workflow_version"`
	TraceID         string       `json:"trace_id"`
	Nodes           []JoinedNode `json:"nodes"`
}

// Catalog resolves the immutable definition for an agent id and version.
// A missing or retired version returns an error with CodeNotFound.
type Catalog interface {
	Resolve(ctx context.Context, agentID, version string) (contracts.AgentDefinition, error)
}

// ExecuteRequest is the pinned call handed to the agent service.
type ExecuteRequest struct {
	Correlation Correlation               `json:"correlation"`
	Identity    PolicyIdentity            `json:"identity"`
	Definition  contracts.AgentDefinition `json:"definition"`
	Input       json.RawMessage           `json:"input"`
	Artifacts   []ArtifactRef             `json:"artifacts,omitempty"`
	Limits      contracts.Limits          `json:"limits"`
	Approval    *ApprovalGate             `json:"approval,omitempty"`
}

// Session is one open agent run. Next proposes an action. Commit applies an
// action this package has already authorized. The session must not apply an
// action that was not committed, and it must stop proposing work when ctx is
// cancelled. Depth and tool allowlists are enforced inside the agent service
// when Commit runs, not by this package.
type Session interface {
	RunID() string
	Next(ctx context.Context) (ProposedAction, error)
	Commit(ctx context.Context, action ProposedAction) error
}

// Executor opens a pinned agent run. Implementations live in the agent service.
type Executor interface {
	Open(ctx context.Context, req ExecuteRequest) (Session, error)
}

// EventLog stores node events for one correlated workflow history.
type EventLog interface {
	Append(ctx context.Context, event Event) (Event, error)
	List(ctx context.Context, workflowRunID string, after uint64) ([]Event, error)
}
