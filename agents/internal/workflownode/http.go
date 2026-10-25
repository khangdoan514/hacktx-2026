package workflownode

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
)

// NewHandler serves Invoke, Cancel, Get, Events, and Join for one Service.
//
// POST /v1/workflow-node/invocations blocks until that invocation is terminal
// and returns it with HTTP 200, including a failed or cancelled outcome. The
// same workflow run id and node invocation id returns the original invocation
// and does not open another session. A different body for that pair is HTTP
// 409 with code CONFLICT.
//
// Cancel marks a non-terminal invocation cancelled and does not commit another
// action. Join returns HTTP 200 only when every named invocation is a
// schema-valid success. The handler does not schedule a graph, edit one, or
// decide an approval.
func NewHandler(svc *Service) http.Handler {
	h := &nodeHandler{svc: svc}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/workflow-node/invocations", h.invoke)
	mux.HandleFunc("POST /v1/workflow-node/runs/{workflowRunID}/invocations/{nodeInvocationID}/cancel", h.cancel)
	mux.HandleFunc("GET /v1/workflow-node/runs/{workflowRunID}/invocations/{nodeInvocationID}", h.get)
	mux.HandleFunc("GET /v1/workflow-node/runs/{workflowRunID}/events", h.events)
	mux.HandleFunc("POST /v1/workflow-node/runs/{workflowRunID}/join", h.join)
	// A method-less pattern is less specific than the method routes above, so
	// other verbs on a known path land here instead of the 404 catch-all.
	for _, pattern := range []string{
		"/v1/workflow-node/invocations",
		"/v1/workflow-node/runs/{workflowRunID}/invocations/{nodeInvocationID}/cancel",
		"/v1/workflow-node/runs/{workflowRunID}/invocations/{nodeInvocationID}",
		"/v1/workflow-node/runs/{workflowRunID}/events",
		"/v1/workflow-node/runs/{workflowRunID}/join",
	} {
		mux.HandleFunc(pattern, methodNotAllowed)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, CodeNotFound, "endpoint not found")
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if h.svc == nil {
			writeError(w, http.StatusInternalServerError, CodeInternal, "workflow node service is not configured")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

type nodeHandler struct {
	svc *Service
}

type errorResponse struct {
	Error *Error `json:"error"`
}

type eventsResponse struct {
	WorkflowRunID string  `json:"workflow_run_id"`
	Events        []Event `json:"events"`
}

type joinRequest struct {
	NodeInvocationIDs []string `json:"node_invocation_ids"`
}

func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, CodeInvalidInput, "method not allowed")
}

func (h *nodeHandler) invoke(w http.ResponseWriter, r *http.Request) {
	var req InvokeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	inv, err := h.svc.Invoke(r.Context(), req)
	writeInvocation(w, inv, err)
}

func (h *nodeHandler) cancel(w http.ResponseWriter, r *http.Request) {
	if !emptyBody(w, r) {
		return
	}
	inv, err := h.svc.Cancel(r.Context(), r.PathValue("workflowRunID"), r.PathValue("nodeInvocationID"))
	writeInvocation(w, inv, err)
}

func (h *nodeHandler) get(w http.ResponseWriter, r *http.Request) {
	if !emptyBody(w, r) {
		return
	}
	inv, err := h.svc.Get(r.Context(), r.PathValue("workflowRunID"), r.PathValue("nodeInvocationID"))
	writeInvocation(w, inv, err)
}

func (h *nodeHandler) events(w http.ResponseWriter, r *http.Request) {
	if !emptyBody(w, r) {
		return
	}
	after, ok := parseAfter(r)
	if !ok {
		writeError(w, http.StatusBadRequest, CodeInvalidInput, "invalid event cursor")
		return
	}
	runID := r.PathValue("workflowRunID")
	events, err := h.svc.Events(r.Context(), runID, after)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if events == nil {
		events = []Event{}
	}
	writeJSON(w, http.StatusOK, eventsResponse{WorkflowRunID: runID, Events: events})
}

func (h *nodeHandler) join(w http.ResponseWriter, r *http.Request) {
	var body joinRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	out, err := h.svc.Join(r.Context(), r.PathValue("workflowRunID"), body.NodeInvocationIDs)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if out.Nodes == nil {
		out.Nodes = []JoinedNode{}
	}
	writeJSON(w, http.StatusOK, out)
}

func writeInvocation(w http.ResponseWriter, inv Invocation, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, inv)
		return
	}
	if inv.ID == "" {
		writeServiceError(w, err)
		return
	}
	mapped := mapErr(err)
	if mapped != nil && (inv.Error == nil || inv.Error.Code != mapped.Code) {
		writeJSON(w, http.StatusInternalServerError, inv)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, CodeInvalidInput, "invalid request JSON")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, CodeInvalidInput, "request must contain one JSON object")
		return false
	}
	return true
}

func emptyBody(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	raw, err := io.ReadAll(r.Body)
	if err != nil || len(bytes.TrimSpace(raw)) != 0 {
		writeError(w, http.StatusBadRequest, CodeInvalidInput, "request body must be empty")
		return false
	}
	return true
}

func parseAfter(r *http.Request) (uint64, bool) {
	vals, ok := r.URL.Query()["after"]
	if !ok {
		return 0, true
	}
	if len(vals) != 1 || vals[0] == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(vals[0], 10, 64)
	if err != nil || n > math.MaxInt64 {
		return 0, false
	}
	return n, true
}

func writeServiceError(w http.ResponseWriter, err error) {
	mapped := mapErr(err)
	if mapped == nil {
		writeError(w, http.StatusInternalServerError, CodeInternal, "internal server error")
		return
	}
	writeJSON(w, statusFor(mapped.Code), errorResponse{Error: mapped})
}

func writeError(w http.ResponseWriter, status int, code Code, message string) {
	writeJSON(w, status, errorResponse{Error: &Error{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}

func statusFor(code Code) int {
	switch code {
	case CodeInvalidInput:
		return http.StatusBadRequest
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict, CodeJoinNotReady, CodeJoinFailed, CodeCancelled:
		return http.StatusConflict
	case CodeOutputSchemaInvalid:
		return http.StatusUnprocessableEntity
	case CodeGraphImmutable, CodeApprovalRequired, CodeApprovalBypass, CodePolicyImmutable, CodeDelegationDepthExceeded:
		return http.StatusForbidden
	case CodeTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}
