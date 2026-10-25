package extensions

import (
	"context"
	"encoding/json"
	"net/http"
)

// HTTPAdapter posts JSON to an internal or external HTTP tool. Internal
// services set Endpoint.Internal; they use this same client.
type HTTPAdapter struct {
	client *http.Client
}

func NewHTTPAdapter() *HTTPAdapter {
	return &HTTPAdapter{client: newSafeClient()}
}

func (a *HTTPAdapter) Invoke(ctx context.Context, d Dispatch) (json.RawMessage, error) {
	if !json.Valid(d.Input) {
		return nil, fail(CodeInvalidInput, "tool input does not match the schema")
	}
	headers := correlationHeaders(d)
	if err := bearer(headers, d.Credential, d.HasCredential); err != nil {
		return nil, err
	}
	res, err := doCall(ctx, a.client, callSpec{
		Method:   http.MethodPost,
		URL:      d.Tool.Endpoint.URL,
		Policy:   d.Tool.Network,
		Body:     d.Input,
		Headers:  headers,
		MaxBytes: d.Tool.Resources.MaxOutputBytes,
		Class:    ClassHTTP,
		Side:     d.Tool.SideEffect,
	})
	if err != nil {
		return nil, err
	}
	if err := classifyStatus(ClassHTTP, d.Tool.SideEffect, res.Status); err != nil {
		return nil, err
	}
	if !json.Valid(res.Body) {
		return nil, fail(CodeOutputSchemaInvalid, "tool output must be JSON")
	}
	return json.RawMessage(res.Body), nil
}
