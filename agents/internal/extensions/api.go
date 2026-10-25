package extensions

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// NewAPI is the tool management and test surface. It does not accept
// credential values; those stay in MemoryCredentials. Mount it only behind
// the service authenticator. A valid invoke body returns 200 with the
// invocation record, including denied, crashed, and timed-out calls.
func NewAPI(reg *Registry, gw *Gateway) http.Handler {
	mux := http.NewServeMux()
	api := &api{reg: reg, gw: gw}
	mux.HandleFunc("POST /v1/extension-tools", api.draft)
	mux.HandleFunc("POST /v1/extension-tools/publish", api.publish)
	mux.HandleFunc("POST /v1/extension-tools/retire", api.retire)
	mux.HandleFunc("POST /v1/extension-tools/pin", api.pin)
	mux.HandleFunc("POST /v1/extension-tools/invoke", api.invoke)
	mux.HandleFunc("GET /v1/extension-tools", api.list)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeErr(w, http.StatusNotFound, CodeNotFound, "endpoint not found")
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mux.ServeHTTP(w, r)
	})
}

type api struct {
	reg *Registry
	gw  *Gateway
}

func (a *api) draft(w http.ResponseWriter, r *http.Request) {
	var rec ToolRecord
	if !decodeJSON(w, r, &rec) {
		return
	}
	if err := a.reg.PutDraft(rec); err != nil {
		writeAPIError(w, err)
		return
	}
	stored, err := a.reg.Get(rec.TenantID, rec.ID, rec.Version)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, stored)
}

func (a *api) publish(w http.ResponseWriter, r *http.Request) {
	var body idBody
	if !decodeJSON(w, r, &body) {
		return
	}
	rec, err := a.reg.Publish(body.TenantID, body.ID, body.Version)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (a *api) retire(w http.ResponseWriter, r *http.Request) {
	var body idBody
	if !decodeJSON(w, r, &body) {
		return
	}
	rec, err := a.reg.Retire(body.TenantID, body.ID, body.Version)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (a *api) pin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RunID    string   `json:"run_id"`
		TenantID string   `json:"tenant_id"`
		ToolIDs  []string `json:"tool_ids"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	pin, err := a.reg.Pin(body.RunID, body.TenantID, body.ToolIDs)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pin)
}

func (a *api) invoke(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RunID        string          `json:"run_id"`
		Principal    Principal       `json:"principal"`
		AgentID      string          `json:"agent_id"`
		AgentVersion string          `json:"agent_version"`
		AllowedTools []string        `json:"allowed_tools"`
		InvocationID string          `json:"invocation_id"`
		ToolID       string          `json:"tool_id"`
		Input        json.RawMessage `json:"input"`
		Approval     *Approval       `json:"approval,omitempty"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.RunID == "" || body.ToolID == "" || body.InvocationID == "" || body.Principal.TenantID == "" {
		writeErr(w, http.StatusBadRequest, CodeInvalidInput, "invoke request is invalid")
		return
	}
	inv, err := a.gw.Invoke(r.Context(), body.RunID, InvokeRequest{
		Principal: body.Principal,
		Agent: contracts.AgentDefinition{
			ID:           body.AgentID,
			Version:      body.AgentVersion,
			AllowedTools: body.AllowedTools,
		},
		Invocation: contracts.AgentInvocation{ID: body.InvocationID, RunID: body.RunID, AgentID: body.AgentID, AgentVersion: body.AgentVersion},
		ToolID:     body.ToolID,
		Input:      body.Input,
		Approval:   body.Approval,
	})
	if inv.ID == "" && err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (a *api) list(w http.ResponseWriter, r *http.Request) {
	tenant := r.URL.Query().Get("tenant_id")
	if !validToken(tenant) {
		writeErr(w, http.StatusBadRequest, CodeInvalidInput, "tenant is invalid")
		return
	}
	id := r.URL.Query().Get("id")
	version := r.URL.Query().Get("version")
	if version != "" {
		rec, err := a.reg.Get(tenant, id, version)
		if err != nil {
			writeAPIError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tools": []ToolRecord{rec}})
		return
	}
	tools := a.reg.List(tenant)
	if id != "" {
		filtered := []ToolRecord{}
		for _, tool := range tools {
			if tool.ID == id {
				filtered = append(filtered, tool)
			}
		}
		tools = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

type idBody struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	Version  string `json:"version"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, CodeInvalidInput, "invalid request JSON")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeErr(w, http.StatusBadRequest, CodeInvalidInput, "request must contain one JSON object")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}

func writeErr(w http.ResponseWriter, status int, code ErrorCode, message string) {
	writeJSON(w, status, map[string]any{"error": Error{Code: code, Message: message}})
}

func writeAPIError(w http.ResponseWriter, err error) {
	typed := asError(err)
	writeErr(w, statusOf(typed.Code), typed.Code, typed.Message)
}

func statusOf(code ErrorCode) int {
	switch code {
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeToolNotAllowed, CodeCredentialDenied, CodeNetworkDenied, CodeFilesystemDenied, CodeApprovalRequired:
		return http.StatusForbidden
	default:
		return http.StatusBadRequest
	}
}
