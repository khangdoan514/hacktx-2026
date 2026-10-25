package policy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

// PrincipalFunc returns the caller for one request. It must use the
// authenticated employee (platform.Verify, then organization.Load, then
// PrincipalFromActor). The request body cannot name the approver.
type PrincipalFunc func(*http.Request) (Principal, error)

// Handler serves the approval inbox.
//
//	GET  /v1/approvals/pending
//	POST /v1/approvals/{id}/decision
//
// cmd/nmc3-agent mounts this when the dedicated agent database is configured.
// The local console principal is the caller. The product API is unchanged:
//
//	root := http.NewServeMux()
//	root.Handle("/v1/approvals/", policy.Handler(engine, principals))
//	root.Handle("/", httpapi.New(coordinator, runs, events, definitions))
//	server := http.Server{Addr: addr, Handler: root, ReadHeaderTimeout: 10 * time.Second}
func Handler(engine *Engine, principals PrincipalFunc) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /v1/approvals/pending", ListPending(engine, principals))
	mux.Handle("POST /v1/approvals/{id}/decision", DecideApproval(engine, principals))
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writePolicyHTTP(w, http.StatusNotFound, CodeNotFound, "endpoint not found")
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mux.ServeHTTP(w, r)
	})
}

// ListPending handles GET /v1/approvals/pending. The list is the caller's
// tenant only, and expired requests are omitted.
func ListPending(engine *Engine, principals PrincipalFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			writePolicyHTTP(w, http.StatusMethodNotAllowed, CodeInvalidInput, "method not allowed")
			return
		}
		principal, ok := principalFrom(w, r, principals)
		if !ok {
			return
		}
		items, err := engine.Inbox(r.Context(), principal)
		if err != nil {
			writeEngineErr(w, err)
			return
		}
		if items == nil {
			items = []ApprovalRequest{}
		}
		wire := make([]approvalWire, len(items))
		for i, item := range items {
			wire[i] = approvalToWire(item)
		}
		writePolicyJSON(w, http.StatusOK, map[string]any{"approvals": wire})
	})
}

// DecideApproval handles POST /v1/approvals/{id}/decision.
// The body is {"outcome":"APPROVED"} or {"outcome":"DENIED"}.
func DecideApproval(engine *Engine, principals PrincipalFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			writePolicyHTTP(w, http.StatusMethodNotAllowed, CodeInvalidInput, "method not allowed")
			return
		}
		principal, ok := principalFrom(w, r, principals)
		if !ok {
			return
		}
		id := r.PathValue("id")
		if id == "" {
			id = approvalIDFromPath(r.URL.Path)
		}
		var body struct {
			Outcome string `json:"outcome"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		decision, err := engine.Decide(r.Context(), principal, id, body.Outcome)
		if err != nil {
			writeEngineErr(w, err)
			return
		}
		writePolicyJSON(w, http.StatusOK, decisionToWire(decision))
	})
}

func approvalIDFromPath(path string) string {
	const prefix = "/v1/approvals/"
	const suffix = "/decision"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
}

type approvalWire struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	RunID          string    `json:"run_id"`
	InvocationID   string    `json:"invocation_id"`
	ToolID         string    `json:"tool_id"`
	ToolVersion    string    `json:"tool_version"`
	IdempotencyKey string    `json:"idempotency_key"`
	PolicyID       string    `json:"policy_id"`
	PolicyRevision int       `json:"policy_revision"`
	RequesterID    string    `json:"requester_id"`
	State          string    `json:"state"`
	InputDigest    string    `json:"input_digest"`
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
}

type decisionWire struct {
	ID          string    `json:"id"`
	RequestID   string    `json:"request_id"`
	TenantID    string    `json:"tenant_id"`
	ApproverID  string    `json:"approver_id"`
	RequesterID string    `json:"requester_id"`
	Outcome     string    `json:"outcome"`
	DecidedAt   time.Time `json:"decided_at"`
}

func approvalToWire(item ApprovalRequest) approvalWire {
	return approvalWire{
		ID: item.ID, TenantID: item.TenantID, RunID: item.RunID, InvocationID: item.InvocationID,
		ToolID: item.ToolID, ToolVersion: item.ToolVersion, IdempotencyKey: item.IdempotencyKey,
		PolicyID: item.PolicyID, PolicyRevision: item.PolicyRevision, RequesterID: item.RequesterID,
		State: string(item.State), InputDigest: item.InputDigest, ExpiresAt: item.ExpiresAt, CreatedAt: item.CreatedAt,
	}
}

func decisionToWire(item ApprovalDecision) decisionWire {
	return decisionWire{
		ID: item.ID, RequestID: item.RequestID, TenantID: item.TenantID, ApproverID: item.ApproverID,
		RequesterID: item.RequesterID, Outcome: item.Outcome, DecidedAt: item.DecidedAt,
	}
}

func principalFrom(w http.ResponseWriter, r *http.Request, principals PrincipalFunc) (Principal, bool) {
	if principals == nil {
		writePolicyHTTP(w, http.StatusUnauthorized, CodeDenied, "authentication is required")
		return Principal{}, false
	}
	principal, err := principals(r)
	if err != nil {
		var target *Error
		if errors.As(err, &target) {
			writeEngineErr(w, err)
			return Principal{}, false
		}
		writePolicyHTTP(w, http.StatusUnauthorized, CodeDenied, "authentication is required")
		return Principal{}, false
	}
	return principal, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writePolicyHTTP(w, http.StatusBadRequest, CodeInvalidInput, "invalid request JSON")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writePolicyHTTP(w, http.StatusBadRequest, CodeInvalidInput, "request must contain one JSON object")
		return false
	}
	return true
}

func writeEngineErr(w http.ResponseWriter, err error) {
	var target *Error
	if !errors.As(err, &target) {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			writePolicyHTTP(w, http.StatusServiceUnavailable, CodeInternal, "request was cancelled")
			return
		}
		writePolicyHTTP(w, http.StatusInternalServerError, CodeInternal, "policy request failed")
		return
	}
	if target.Code == CodeCrossTenant {
		writePolicyHTTP(w, http.StatusNotFound, CodeNotFound, "approval request not found")
		return
	}
	writePolicyHTTP(w, statusOf(target.Code), target.Code, target.Message)
}

func statusOf(code string) int {
	switch code {
	case CodeInvalidInput:
		return http.StatusBadRequest
	case CodeDenied, CodeUnauthorizedApprover, CodeSelfApproval:
		return http.StatusForbidden
	case CodeCrossTenant, CodeNotFound:
		return http.StatusNotFound
	case CodeExpired, CodeAlreadyDecided, CodeConflict, CodeNotDispatchable, CodeDuplicateWrite, CodeUncertain:
		return http.StatusConflict
	case CodeInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusBadRequest
	}
}

func writePolicyHTTP(w http.ResponseWriter, status int, code, message string) {
	writePolicyJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func writePolicyJSON(w http.ResponseWriter, status int, body any) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
