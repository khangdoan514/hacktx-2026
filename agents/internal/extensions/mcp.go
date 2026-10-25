package extensions

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
)

// MCPAdapter is a stateless JSON-RPC client for the tools/call subset.
// Each invocation sends initialize, then tools/call, over HTTP POST.
// The tool result is result.structuredContent.
type MCPAdapter struct {
	client *http.Client
	seq    atomic.Int64
}

func NewMCPAdapter() *MCPAdapter {
	return &MCPAdapter{client: newSafeClient()}
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (a *MCPAdapter) Invoke(ctx context.Context, d Dispatch) (json.RawMessage, error) {
	if _, err := a.roundTrip(ctx, d, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "nmc3-extensions", "version": "1"},
	}); err != nil {
		return nil, err
	}
	result, err := a.roundTrip(ctx, d, "tools/call", map[string]any{
		"name":      d.Tool.Endpoint.MCPTool,
		"arguments": json.RawMessage(d.Input),
	})
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Structured json.RawMessage `json:"structuredContent"`
		IsError    bool            `json:"isError"`
	}
	if err := json.Unmarshal(result, &decoded); err != nil || decoded.IsError || blank(decoded.Structured) || !json.Valid(decoded.Structured) {
		return nil, &callFailure{
			errorCode: CodeInternal,
			message:   "MCP tool returned an unreadable result",
			terminal:  d.Tool.SideEffect == SideEffectWrite,
		}
	}
	return decoded.Structured, nil
}

func (a *MCPAdapter) roundTrip(ctx context.Context, d Dispatch, method string, params any) (json.RawMessage, error) {
	payload, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      a.seq.Add(1),
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return nil, fail(CodeInternal, "tool request could not be encoded")
	}
	headers := correlationHeaders(d)
	if err := bearer(headers, d.Credential, d.HasCredential); err != nil {
		return nil, err
	}
	res, err := doCall(ctx, a.client, callSpec{
		Method:   http.MethodPost,
		URL:      d.Tool.Endpoint.URL,
		Policy:   d.Tool.Network,
		Body:     payload,
		Headers:  headers,
		MaxBytes: d.Tool.Resources.MaxOutputBytes,
		Class:    d.Tool.Class,
		Side:     d.Tool.SideEffect,
	})
	if err != nil {
		return nil, err
	}
	if err := classifyStatus(d.Tool.Class, d.Tool.SideEffect, res.Status); err != nil {
		return nil, err
	}
	var decoded rpcResponse
	if err := json.Unmarshal(res.Body, &decoded); err != nil || decoded.JSONRPC != "2.0" {
		return nil, &callFailure{
			errorCode: CodeInternal,
			message:   "MCP response was unreadable",
			terminal:  d.Tool.SideEffect == SideEffectWrite,
		}
	}
	if decoded.Error != nil {
		return nil, &callFailure{
			errorCode: CodeInternal,
			message:   "MCP tool call failed",
			terminal:  d.Tool.SideEffect == SideEffectWrite,
		}
	}
	return decoded.Result, nil
}
