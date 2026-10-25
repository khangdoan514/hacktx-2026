package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"
)

// Handler is a custom tool. Input is untrusted data. CredentialFromContext,
// Dial, and ReadFile expose only the resources attached to this call.
type Handler func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)

// ServiceTool is one tool a worker process can serve.
type ServiceTool struct {
	ID           string
	Version      string
	Description  string
	InputSchema  json.RawMessage
	OutputSchema json.RawMessage
	SideEffect   SideEffect
	Handle       Handler
}

// Serve hosts tools on ln until ctx is cancelled. One handler panic is
// recorded as a tool error and does not stop the process.
func Serve(ctx context.Context, ln net.Listener, tools []ServiceTool) error {
	if ctx == nil || ln == nil {
		return fail(CodeInvalidInput, "worker listener is invalid")
	}
	index := map[string]ServiceTool{}
	health := healthBody{Protocol: ProtocolVersion, Tools: []DiscoveredTool{}}
	for _, tool := range tools {
		if tool.Handle == nil || !validToken(tool.ID) || !validToken(tool.Version) {
			return fail(CodeInvalidInput, "worker tool is invalid")
		}
		if tool.SideEffect != SideEffectReadOnly && tool.SideEffect != SideEffectWrite {
			return fail(CodeInvalidInput, "tool side effect is invalid")
		}
		if _, ok := index[tool.ID]; ok {
			return fail(CodeConflict, "worker tool is already registered")
		}
		if err := ValidateSchemaDocument(tool.InputSchema); err != nil {
			return err
		}
		if err := ValidateSchemaDocument(tool.OutputSchema); err != nil {
			return err
		}
		index[tool.ID] = tool
		health.Tools = append(health.Tools, DiscoveredTool{
			ID:           tool.ID,
			Version:      tool.Version,
			Description:  tool.Description,
			InputSchema:  tool.InputSchema,
			OutputSchema: tool.OutputSchema,
			SideEffect:   tool.SideEffect,
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, health)
	})
	mux.HandleFunc("POST /v1/invoke", func(w http.ResponseWriter, r *http.Request) {
		serveInvoke(w, r, index)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	err := server.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func serveInvoke(w http.ResponseWriter, r *http.Request, index map[string]ServiceTool) {
	defer func() {
		if recover() != nil {
			writeJSON(w, http.StatusOK, wireResponse{
				Protocol: ProtocolVersion,
				Error:    &Error{Code: CodeInternal, Message: "tool failed"},
			})
		}
	}()
	if r.Header.Get("NMC3-Extension-Protocol") != "1" {
		writeJSON(w, http.StatusBadRequest, wireResponse{
			Protocol: ProtocolVersion,
			Error:    &Error{Code: CodeInvalidInput, Message: "worker protocol mismatch"},
		})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	var req wireRequest
	if err := dec.Decode(&req); err != nil || req.Protocol != ProtocolVersion {
		writeJSON(w, http.StatusBadRequest, wireResponse{
			Protocol: ProtocolVersion,
			Error:    &Error{Code: CodeInvalidInput, Message: "worker protocol mismatch"},
		})
		return
	}
	tool, ok := index[req.ToolID]
	if !ok || tool.Version != req.ToolVersion {
		writeJSON(w, http.StatusOK, wireResponse{
			Protocol: ProtocolVersion,
			Error:    &Error{Code: CodeNotFound, Message: "pinned tool version is not served"},
		})
		return
	}
	iso := isolation{network: req.Network, root: req.Filesystem.Root}
	if req.Credential != nil {
		iso.cred = Credential{Ref: req.Credential.Ref, TenantID: req.TenantID, Value: req.Credential.Value}
		iso.hasCred = true
	}
	ctx := withIsolation(r.Context(), iso)
	output, err := tool.Handle(ctx, req.Input)
	if err != nil {
		writeJSON(w, http.StatusOK, wireResponse{Protocol: ProtocolVersion, Error: handlerError(err)})
		return
	}
	limit := req.MaxOutputBytes
	if limit <= 0 {
		limit = 1 << 20
	}
	if !json.Valid(output) {
		writeJSON(w, http.StatusOK, wireResponse{
			Protocol: ProtocolVersion,
			Error:    &Error{Code: CodeOutputSchemaInvalid, Message: "tool output must be JSON"},
		})
		return
	}
	if len(output) > limit {
		writeJSON(w, http.StatusOK, wireResponse{
			Protocol: ProtocolVersion,
			Error:    &Error{Code: CodeResourceLimit, Message: "tool output exceeded the size limit"},
		})
		return
	}
	writeJSON(w, http.StatusOK, wireResponse{Protocol: ProtocolVersion, Output: output})
}

func handlerError(err error) *Error {
	var typed *Error
	if errors.As(err, &typed) && typed != nil {
		return &Error{Code: typed.Code, Message: typed.Message}
	}
	return &Error{Code: CodeInternal, Message: "tool failed"}
}
