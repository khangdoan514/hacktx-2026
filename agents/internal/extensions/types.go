package extensions

import (
	"encoding/json"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

const ProtocolVersion = 1

type Class string

const (
	ClassMCP    Class = "mcp"
	ClassHTTP   Class = "http"
	ClassWorker Class = "worker"
)

type Lifecycle string

const (
	LifecycleDraft     Lifecycle = "draft"
	LifecyclePublished Lifecycle = "published"
	LifecycleRetired   Lifecycle = "retired"
)

type SideEffect string

const (
	SideEffectReadOnly SideEffect = "READ_ONLY"
	SideEffectWrite    SideEffect = "WRITE"
)

type ErrorCode string

const (
	CodeInvalidInput        ErrorCode = "INVALID_INPUT"
	CodeNotFound            ErrorCode = "NOT_FOUND"
	CodeToolNotAllowed      ErrorCode = "TOOL_NOT_ALLOWED"
	CodeTimeout             ErrorCode = "TIMEOUT"
	CodeOutputSchemaInvalid ErrorCode = "OUTPUT_SCHEMA_INVALID"
	CodeInternal            ErrorCode = "INTERNAL"
	CodeWorkerCrash         ErrorCode = "WORKER_CRASH"
	CodeCredentialDenied    ErrorCode = "CREDENTIAL_DENIED"
	CodeNetworkDenied       ErrorCode = "NETWORK_DENIED"
	CodeFilesystemDenied    ErrorCode = "FILESYSTEM_DENIED"
	CodeApprovalRequired    ErrorCode = "APPROVAL_REQUIRED"
	CodeConflict            ErrorCode = "CONFLICT"
	CodeUpstreamUnavailable ErrorCode = "UPSTREAM_UNAVAILABLE"
	CodeCancelled           ErrorCode = "CANCELLED"
	CodeResourceLimit       ErrorCode = "RESOURCE_LIMIT"
)

// Error is a safe, typed failure. Messages must not contain credentials.
type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code) + ": " + e.Message
}

func (e *Error) RunError() *contracts.RunError {
	if e == nil {
		return nil
	}
	return &contracts.RunError{Code: contracts.ErrorCode(e.Code), Message: e.Message}
}

type Endpoint struct {
	URL      string `json:"url"`
	MCPTool  string `json:"mcp_tool,omitempty"`
	Method   string `json:"method,omitempty"`
	WorkerID string `json:"worker_id,omitempty"`
	// Internal marks an HTTP tool that calls a private service. Transport
	// still uses the HTTP adapter.
	Internal bool `json:"internal,omitempty"`
}

type RetryPolicy struct {
	MaxAttempts int `json:"max_attempts"`
}

type ResourceLimits struct {
	MaxOutputBytes int `json:"max_output_bytes"`
}

// NetworkPolicy allows outbound IP:port targets. Hostnames are rejected so a
// tool cannot trigger a DNS lookup. An empty list denies every dial.
type NetworkPolicy struct {
	Allow []string `json:"allow"`
}

type FilesystemPolicy struct {
	Root string `json:"root,omitempty"`
}

// ToolRecord is the registry shape. It adds endpoint, auth reference,
// side-effect, and worker fields around the v1 tool definition. Credential
// values are not a field on this struct.
type ToolRecord struct {
	TenantID     string           `json:"tenant_id"`
	ID           string           `json:"id"`
	Version      string           `json:"version"`
	Description  string           `json:"description,omitempty"`
	InputSchema  json.RawMessage  `json:"input_schema,omitempty"`
	OutputSchema json.RawMessage  `json:"output_schema,omitempty"`
	SideEffect   SideEffect       `json:"side_effect"`
	TimeoutMS    int              `json:"timeout_ms"`
	Class        Class            `json:"class"`
	Endpoint     Endpoint         `json:"endpoint"`
	AuthRef      string           `json:"auth_ref,omitempty"`
	Permissions  []string         `json:"permissions,omitempty"`
	Retry        RetryPolicy      `json:"retry"`
	Resources    ResourceLimits   `json:"resources"`
	Network      NetworkPolicy    `json:"network"`
	Filesystem   FilesystemPolicy `json:"filesystem"`
	Lifecycle    Lifecycle        `json:"lifecycle"`
}

func (t ToolRecord) timeout() time.Duration {
	if t.TimeoutMS <= 0 {
		return time.Second
	}
	return time.Duration(t.TimeoutMS) * time.Millisecond
}

// V1 projects the registry record onto the shared first-pass tool definition.
func (t ToolRecord) V1() contracts.ToolDefinition {
	seconds := t.TimeoutMS / 1000
	if t.TimeoutMS > 0 && seconds == 0 {
		seconds = 1
	}
	return contracts.ToolDefinition{
		ID:             t.ID,
		Version:        t.Version,
		Description:    t.Description,
		InputSchema:    cloneRaw(t.InputSchema),
		OutputSchema:   cloneRaw(t.OutputSchema),
		SideEffect:     string(t.SideEffect),
		TimeoutSeconds: seconds,
	}
}

type Principal struct {
	ID          string   `json:"id"`
	TenantID    string   `json:"tenant_id"`
	Permissions []string `json:"permissions,omitempty"`
}

// Approval is a Pass 4 decision for a write tool. The key is the idempotency
// key; the credential value is not part of the decision.
type Approval struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	ToolID         string    `json:"tool_id"`
	ToolVersion    string    `json:"tool_version"`
	IdempotencyKey string    `json:"idempotency_key"`
	Approved       bool      `json:"approved"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type Attempt struct {
	Number     int       `json:"number"`
	Code       ErrorCode `json:"code,omitempty"`
	Crash      bool      `json:"crash,omitempty"`
	Uncertain  bool      `json:"uncertain,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// Invocation is the extended tool call record: v1 identity and payload plus
// endpoint, auth reference, side-effect, attempt, and worker metadata.
type Invocation struct {
	ID                string          `json:"id"`
	RunID             string          `json:"run_id"`
	AgentInvocationID string          `json:"agent_invocation_id,omitempty"`
	TenantID          string          `json:"tenant_id,omitempty"`
	ToolID            string          `json:"tool_id"`
	ToolVersion       string          `json:"tool_version,omitempty"`
	Class             Class           `json:"class,omitempty"`
	SideEffect        SideEffect      `json:"side_effect,omitempty"`
	Endpoint          string          `json:"endpoint,omitempty"`
	AuthRef           string          `json:"auth_ref,omitempty"`
	WorkerID          string          `json:"worker_id,omitempty"`
	Internal          bool            `json:"internal,omitempty"`
	Attempt           int             `json:"attempt"`
	Attempts          int             `json:"attempts"`
	History           []Attempt       `json:"history"`
	Input             json.RawMessage `json:"input,omitempty"`
	Output            json.RawMessage `json:"output,omitempty"`
	Error             *Error          `json:"error,omitempty"`
	Crash             bool            `json:"crash,omitempty"`
	Uncertain         bool            `json:"uncertain,omitempty"`
	StartedAt         time.Time       `json:"started_at"`
	FinishedAt        time.Time       `json:"finished_at"`
}

func (inv Invocation) V1() contracts.ToolInvocation {
	finished := inv.FinishedAt
	return contracts.ToolInvocation{
		ID:                inv.ID,
		RunID:             inv.RunID,
		AgentInvocationID: inv.AgentInvocationID,
		ToolID:            inv.ToolID,
		ToolVersion:       inv.ToolVersion,
		Input:             cloneRaw(inv.Input),
		Output:            cloneRaw(inv.Output),
		Error:             inv.Error.RunError(),
		StartedAt:         inv.StartedAt,
		FinishedAt:        &finished,
	}
}

// Metadata is the event payload. It omits tool input, output, and credentials.
type Metadata struct {
	ToolID      string     `json:"tool_id"`
	ToolVersion string     `json:"tool_version,omitempty"`
	Class       Class      `json:"class,omitempty"`
	SideEffect  SideEffect `json:"side_effect,omitempty"`
	Endpoint    string     `json:"endpoint,omitempty"`
	AuthRef     string     `json:"auth_ref,omitempty"`
	WorkerID    string     `json:"worker_id,omitempty"`
	Attempt     int        `json:"attempt"`
	Crash       bool       `json:"crash,omitempty"`
	Uncertain   bool       `json:"uncertain,omitempty"`
	ErrorCode   ErrorCode  `json:"error_code,omitempty"`
}

func (inv Invocation) Metadata() Metadata {
	meta := Metadata{
		ToolID:      inv.ToolID,
		ToolVersion: inv.ToolVersion,
		Class:       inv.Class,
		SideEffect:  inv.SideEffect,
		Endpoint:    inv.Endpoint,
		AuthRef:     inv.AuthRef,
		WorkerID:    inv.WorkerID,
		Attempt:     inv.Attempt,
		Crash:       inv.Crash,
		Uncertain:   inv.Uncertain,
	}
	if inv.Error != nil {
		meta.ErrorCode = inv.Error.Code
	}
	return meta
}

// Pin freezes tool id → version for one run.
type Pin struct {
	RunID    string            `json:"run_id"`
	TenantID string            `json:"tenant_id"`
	Tools    map[string]string `json:"tools"`
}

type InvokeRequest struct {
	Principal  Principal
	Agent      contracts.AgentDefinition
	Invocation contracts.AgentInvocation
	ToolID     string
	Input      json.RawMessage
	Approval   *Approval
}

// Dispatch is the adapter input. Credential is empty unless HasCredential.
type Dispatch struct {
	RunID             string
	AgentInvocationID string
	InvocationID      string
	PrincipalID       string
	Tool              ToolRecord
	Credential        Credential
	HasCredential     bool
	Input             json.RawMessage
}

type Credential struct {
	Ref      string
	TenantID string
	Value    string
}

// DiscoveredTool is worker registration metadata. It has no handler and no secret.
type DiscoveredTool struct {
	ID           string          `json:"id"`
	Version      string          `json:"version"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	SideEffect   SideEffect      `json:"side_effect"`
}

type ImportOptions struct {
	TenantID       string
	Endpoint       string
	WorkerID       string
	Network        NetworkPolicy
	Filesystem     FilesystemPolicy
	Permissions    []string
	AuthRef        string
	TimeoutMS      int
	MaxOutputBytes int
	RetryMax       int
}

func fail(code ErrorCode, message string) error {
	return &Error{Code: code, Message: message}
}

func asError(err error) *Error {
	if err == nil {
		return nil
	}
	var ee *Error
	if ok := errorAs(err, &ee); ok {
		return ee
	}
	var cf *callFailure
	if errorAs(err, &cf) {
		return &Error{Code: cf.errorCode, Message: cf.message}
	}
	return &Error{Code: CodeInternal, Message: "tool failed"}
}
