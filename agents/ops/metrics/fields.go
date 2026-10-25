package metrics

// JSON field names shared by logs and traces. A run-scoped record always
// includes run_id. Trace correlation uses trace_id and span_id. tenant_id is
// the organization boundary used by the concurrency limiter.
const (
	KeyTimestamp    = "timestamp"
	KeyLevel        = "level"
	KeyMessage      = "message"
	KeyName         = "name"
	KeyRunID        = "run_id"
	KeyTraceID      = "trace_id"
	KeySpanID       = "span_id"
	KeyParentSpanID = "parent_span_id"
	KeyTenantID     = "tenant_id"
	KeyPrincipalID  = "principal_id"
	KeyInvocationID = "invocation_id"
	KeyAgentID      = "agent_id"
	KeyAgentVersion = "agent_version"
	KeyState        = "state"
	KeyErrorCode    = "error_code"
	KeyAttempt      = "attempt"
	KeyAttrs        = "attrs"
	KeyInputTokens  = "input_tokens"
	KeyOutputTokens = "output_tokens"
	KeyCostStatus   = "cost_status"
	KeyCostMicroUSD = "cost_micro_usd"

	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"

	// Redacted replaces a secret value. Restricted content is omitted instead
	// of being replaced, so unauthorized text is not present under another key.
	Redacted = "[redacted]"

	// TargetUnset means no measured service target exists yet.
	TargetUnset = "unset"
	// CostUnset means one or more observations did not include a provider-reported cost.
	CostUnset = "unset"
	// CostObserved means every observation in the total included a caller-supplied cost.
	CostObserved = "observed"
)

// Fields is the identity carried on a log line and a trace span.
// RunID is required. PrincipalID is an identity id, not a credential.
type Fields struct {
	RunID        string
	TraceID      string
	SpanID       string
	ParentSpanID string
	TenantID     string
	PrincipalID  string
	InvocationID string
	AgentID      string
	AgentVersion string
	State        string
	ErrorCode    string
	Attempt      int
}
