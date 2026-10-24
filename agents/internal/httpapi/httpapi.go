package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
)

// New returns the versioned agent API. The coordinator owns execution; this
// handler only validates the wire request and exposes persisted inspection data.
func New(coordinator contracts.Coordinator, runs contracts.RunStore, events contracts.EventSink, definitions []contracts.AgentDefinition) http.Handler {
	h := &handler{coordinator: coordinator, runs: runs, events: events, agents: make(map[string]contracts.AgentDefinition)}
	for _, d := range definitions {
		h.agents[d.ID+"@"+d.Version] = d
	}
	return h
}

type handler struct {
	coordinator contracts.Coordinator
	runs        contracts.RunStore
	events      contracts.EventSink
	agents      map[string]contracts.AgentDefinition
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/v1/agents" && r.Method == http.MethodGet {
		h.listAgents(w)
		return
	}
	if r.URL.Path == "/v1/agent-runs" && r.Method == http.MethodPost {
		h.start(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/agent-runs/") && r.Method == http.MethodGet {
		tail := strings.TrimPrefix(r.URL.Path, "/v1/agent-runs/")
		if strings.HasSuffix(tail, "/events") {
			id := strings.TrimSuffix(tail, "/events")
			if id != "" && !strings.Contains(id, "/") {
				h.listEvents(w, r, id)
				return
			}
		} else if tail != "" && !strings.Contains(tail, "/") {
			h.getRun(w, r, tail)
			return
		}
	}
	if r.URL.Path == "/v1/agents" || r.URL.Path == "/v1/agent-runs" || strings.HasPrefix(r.URL.Path, "/v1/agent-runs/") {
		writeError(w, http.StatusMethodNotAllowed, contracts.ErrInvalidInput, "method not allowed")
		return
	}
	writeError(w, http.StatusNotFound, contracts.ErrNotFound, "endpoint not found")
}

func (h *handler) start(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req contracts.StartRunRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, contracts.ErrInvalidInput, "invalid request JSON")
		return
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, contracts.ErrInvalidInput, "request must contain one JSON object")
		return
	}
	if req.AgentID == "" || req.AgentVersion == "" || len(req.Input) == 0 || !json.Valid(req.Input) || !positiveLimits(req.Limits) {
		writeError(w, http.StatusBadRequest, contracts.ErrInvalidInput, "invalid run request")
		return
	}
	if req.ModelID != "" && !contracts.ValidModelID(req.ModelID) {
		writeError(w, http.StatusBadRequest, contracts.ErrInvalidInput, "invalid model ID")
		return
	}
	if _, ok := h.agents[req.AgentID+"@"+req.AgentVersion]; !ok {
		writeError(w, http.StatusNotFound, contracts.ErrNotFound, "agent version not found")
		return
	}
	run, err := h.coordinator.Start(r.Context(), req)
	if err != nil {
		writeRunError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, contracts.StartRunResponse{RunID: run.ID, State: run.State})
}

func positiveLimits(l contracts.Limits) bool {
	return l.MaxDelegations > 0 && l.MaxConcurrentAgents > 0 && l.MaxToolCalls > 0 && l.MaxModelTurns > 0 && l.MaxTokens > 0 && l.MaxCostUSD > 0 && !math.IsNaN(l.MaxCostUSD) && !math.IsInf(l.MaxCostUSD, 0) && l.TimeoutSeconds > 0
}

func (h *handler) getRun(w http.ResponseWriter, r *http.Request, id string) {
	run, err := h.runs.Get(r.Context(), id)
	if err != nil {
		writeRunError(w, err)
		return
	}
	if run.Invocations == nil {
		run.Invocations = []contracts.AgentInvocation{}
	}
	if run.ToolInvocations == nil {
		run.ToolInvocations = []contracts.ToolInvocation{}
	}
	writeJSON(w, http.StatusOK, contracts.RunResponse{Run: run})
}

func (h *handler) listEvents(w http.ResponseWriter, r *http.Request, id string) {
	after := uint64(0)
	if vals, ok := r.URL.Query()["after"]; ok {
		if len(vals) != 1 || vals[0] == "" {
			writeError(w, http.StatusBadRequest, contracts.ErrInvalidInput, "invalid event cursor")
			return
		}
		n, err := strconv.ParseUint(vals[0], 10, 64)
		if err != nil || n > math.MaxInt64 {
			writeError(w, http.StatusBadRequest, contracts.ErrInvalidInput, "invalid event cursor")
			return
		}
		after = n
	}
	if _, err := h.runs.Get(r.Context(), id); err != nil {
		writeRunError(w, err)
		return
	}
	events, err := h.events.List(r.Context(), id, after)
	if err != nil {
		writeRunError(w, err)
		return
	}
	if events == nil {
		events = []contracts.RunEvent{}
	}
	writeJSON(w, http.StatusOK, contracts.EventsResponse{RunID: id, Events: events})
}

func (h *handler) listAgents(w http.ResponseWriter) {
	agents := make([]contracts.AgentSummary, 0, len(h.agents))
	for _, d := range h.agents {
		agents = append(agents, contracts.AgentSummary{ID: d.ID, Version: d.Version, Name: d.Name})
	}
	// Stable output supports clients and tests.
	for i := 0; i < len(agents); i++ {
		for j := i + 1; j < len(agents); j++ {
			if agents[j].ID+agents[j].Version < agents[i].ID+agents[i].Version {
				agents[i], agents[j] = agents[j], agents[i]
			}
		}
	}
	writeJSON(w, http.StatusOK, contracts.AgentsResponse{Agents: agents})
}

func writeRunError(w http.ResponseWriter, err error) {
	var e *contracts.RunError
	if errors.As(err, &e) {
		status := http.StatusInternalServerError
		switch e.Code {
		case contracts.ErrInvalidInput:
			status = http.StatusBadRequest
		case contracts.ErrNotFound:
			status = http.StatusNotFound
		}
		writeError(w, status, e.Code, e.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, contracts.ErrInternal, "internal server error")
}
func writeError(w http.ResponseWriter, status int, code contracts.ErrorCode, message string) {
	writeJSON(w, status, contracts.ErrorResponse{Error: contracts.RunError{Code: code, Message: message}})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
