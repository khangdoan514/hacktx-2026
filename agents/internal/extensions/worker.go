package extensions

import (
	"context"
	"encoding/json"
	"net/http"
)

// WorkerAdapter calls a separate extension process over the v1 worker protocol.
type WorkerAdapter struct {
	client *http.Client
}

func NewWorkerAdapter() *WorkerAdapter {
	return &WorkerAdapter{client: newSafeClient()}
}

func (a *WorkerAdapter) Invoke(ctx context.Context, d Dispatch) (json.RawMessage, error) {
	endpoint, err := joinPath(d.Tool.Endpoint.URL, "/v1/invoke")
	if err != nil {
		return nil, err
	}
	payload := wireRequest{
		Protocol:          ProtocolVersion,
		InvocationID:      d.InvocationID,
		RunID:             d.RunID,
		AgentInvocationID: d.AgentInvocationID,
		PrincipalID:       d.PrincipalID,
		TenantID:          d.Tool.TenantID,
		ToolID:            d.Tool.ID,
		ToolVersion:       d.Tool.Version,
		Input:             d.Input,
		Network:           d.Tool.Network,
		Filesystem:        d.Tool.Filesystem,
		MaxOutputBytes:    d.Tool.Resources.MaxOutputBytes,
	}
	if d.HasCredential {
		if d.Credential.Value == "" {
			return nil, fail(CodeCredentialDenied, "credential value is not allowed")
		}
		payload.Credential = &wireCredential{Ref: d.Credential.Ref, Value: d.Credential.Value}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fail(CodeInternal, "tool request could not be encoded")
	}
	headers := correlationHeaders(d)
	headers["NMC3-Extension-Protocol"] = "1"
	res, err := doCall(ctx, a.client, callSpec{
		Method:   http.MethodPost,
		URL:      endpoint,
		Policy:   d.Tool.Network,
		Body:     body,
		Headers:  headers,
		MaxBytes: d.Tool.Resources.MaxOutputBytes,
		Class:    ClassWorker,
		Side:     d.Tool.SideEffect,
	})
	if err != nil {
		return nil, err
	}
	if err := classifyStatus(ClassWorker, d.Tool.SideEffect, res.Status); err != nil {
		return nil, err
	}
	var decoded wireResponse
	if err := json.Unmarshal(res.Body, &decoded); err != nil || decoded.Protocol != ProtocolVersion {
		return nil, &callFailure{
			errorCode: CodeWorkerCrash,
			message:   "extension worker crashed",
			crash:     true,
			uncertain: d.Tool.SideEffect == SideEffectWrite,
			terminal:  d.Tool.SideEffect == SideEffectWrite,
		}
	}
	if decoded.Error != nil {
		return nil, &callFailure{
			errorCode: decoded.Error.Code,
			message:   decoded.Error.Message,
			terminal:  d.Tool.SideEffect == SideEffectWrite,
		}
	}
	if !json.Valid(decoded.Output) {
		return nil, fail(CodeOutputSchemaInvalid, "tool output must be JSON")
	}
	return decoded.Output, nil
}

// Discover reads the worker's registration list. It does not publish tools
// and it does not receive credentials.
func (a *WorkerAdapter) Discover(ctx context.Context, baseURL string, policy NetworkPolicy) ([]DiscoveredTool, error) {
	endpoint, err := joinPath(baseURL, "/v1/health")
	if err != nil {
		return nil, err
	}
	res, err := doCall(ctx, a.client, callSpec{
		Method:   http.MethodGet,
		URL:      endpoint,
		Policy:   policy,
		MaxBytes: 1 << 20,
		Class:    ClassWorker,
		Side:     SideEffectReadOnly,
	})
	if err != nil {
		return nil, err
	}
	if err := classifyStatus(ClassWorker, SideEffectReadOnly, res.Status); err != nil {
		return nil, err
	}
	var health healthBody
	if err := json.Unmarshal(res.Body, &health); err != nil || health.Protocol != ProtocolVersion {
		return nil, fail(CodeWorkerCrash, "extension worker crashed")
	}
	return health.Tools, nil
}
