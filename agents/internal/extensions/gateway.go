package extensions

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Options configures the gateway. Now is optional and defaults to the clock.
type Options struct {
	Now func() time.Time
}

// Gateway is the only dispatch path for MCP, HTTP, and worker tools.
type Gateway struct {
	reg      *Registry
	creds    *MemoryCredentials
	mcp      *MCPAdapter
	http     *HTTPAdapter
	workers  *WorkerAdapter
	now      func() time.Time
	mu       sync.Mutex
	writes   map[string]writeOutcome
	inflight map[string]struct{}
}

type writeOutcome struct {
	inv Invocation
	err *Error
}

func NewGateway(reg *Registry, creds *MemoryCredentials, opts Options) *Gateway {
	if reg == nil {
		panic("extensions: registry is required")
	}
	return &Gateway{
		reg:      reg,
		creds:    creds,
		mcp:      NewMCPAdapter(),
		http:     NewHTTPAdapter(),
		workers:  NewWorkerAdapter(),
		now:      opts.Now,
		writes:   map[string]writeOutcome{},
		inflight: map[string]struct{}{},
	}
}

// WorkerDiscovery exposes registration reads for the worker adapter.
func (g *Gateway) WorkerDiscovery() *WorkerAdapter { return g.workers }

func (g *Gateway) nowUTC() time.Time {
	if g.now == nil {
		return time.Now().UTC()
	}
	return g.now().UTC()
}

// Invoke checks the pinned tool, authorizes the caller, and dispatches one
// adapter. A worker crash or timeout is returned on the invocation record.
func (g *Gateway) Invoke(ctx context.Context, runID string, req InvokeRequest) (Invocation, error) {
	inv := Invocation{
		ID:                newID("tool"),
		RunID:             runID,
		AgentInvocationID: req.Invocation.ID,
		TenantID:          req.Principal.TenantID,
		ToolID:            req.ToolID,
		Input:             cloneRaw(req.Input),
		History:           []Attempt{},
		StartedAt:         g.nowUTC(),
	}
	tool, err := g.reg.Resolve(runID, req.Principal.TenantID, req.ToolID)
	if err != nil {
		return g.finish(inv, "", err)
	}
	inv.ToolVersion = tool.Version
	inv.Class = tool.Class
	inv.SideEffect = tool.SideEffect
	inv.Endpoint = tool.Endpoint.URL
	inv.AuthRef = tool.AuthRef
	inv.WorkerID = tool.Endpoint.WorkerID
	inv.Internal = tool.Endpoint.Internal
	if err := Authorize(req.Principal, req.Agent, tool); err != nil {
		return g.finish(inv, "", err)
	}
	if err := ValidateValue(tool.InputSchema, req.Input); err != nil {
		return g.finish(inv, "", fail(CodeInvalidInput, "tool input does not match the schema"))
	}
	var key string
	if tool.SideEffect == SideEffectWrite {
		if err := checkApproval(g.nowUTC(), tool, req.Approval); err != nil {
			return g.finish(inv, "", err)
		}
		key = writeKey(tool, req.Approval.IdempotencyKey)
		if prev, prevErr, ok := g.replay(key); ok {
			return prev, prevErr
		}
		if !g.begin(key) {
			if prev, prevErr, ok := g.replay(key); ok {
				return prev, prevErr
			}
			return g.finish(inv, "", fail(CodeConflict, "write is already in progress"))
		}
		defer g.end(key)
	}
	var cred Credential
	var has bool
	if tool.AuthRef != "" {
		resolved, ok := g.creds.Resolve(tool.TenantID, tool.AuthRef)
		if !ok {
			return g.finish(inv, "", fail(CodeCredentialDenied, "credential is not available"))
		}
		cred = resolved
		has = true
	}
	dispatch := Dispatch{
		RunID:             runID,
		AgentInvocationID: req.Invocation.ID,
		InvocationID:      inv.ID,
		PrincipalID:       req.Principal.ID,
		Tool:              tool,
		Credential:        cred,
		HasCredential:     has,
		Input:             req.Input,
	}
	return g.attempts(ctx, inv, tool, dispatch, cred.Value, key)
}

func (g *Gateway) attempts(ctx context.Context, inv Invocation, tool ToolRecord, dispatch Dispatch, secret, key string) (Invocation, error) {
	max := tool.Retry.MaxAttempts
	if tool.SideEffect == SideEffectWrite || max < 1 {
		max = 1
	}
	if max > 5 {
		max = 5
	}
	var last error
	for n := 1; n <= max; n++ {
		if err := ctx.Err(); err != nil {
			last = classifyTransport(tool.Class, tool.SideEffect, err)
			inv.History = append(inv.History, attemptFrom(n, g.nowUTC(), g.nowUTC(), last))
			break
		}
		started := g.nowUTC()
		attemptCtx, cancel := context.WithTimeout(ctx, tool.timeout())
		out, err := g.dispatch(attemptCtx, dispatch)
		cancel()
		finished := g.nowUTC()
		if err == nil && secret != "" && bytes.Contains(out, []byte(secret)) {
			out = nil
			err = fail(CodeCredentialDenied, "tool output contained a credential and was discarded")
		}
		if err == nil {
			if schemaErr := ValidateValue(tool.OutputSchema, out); schemaErr != nil {
				err = fail(CodeOutputSchemaInvalid, "tool output does not match the schema")
			}
		}
		if err == nil {
			inv.History = append(inv.History, Attempt{Number: n, StartedAt: started, FinishedAt: finished})
			inv.Attempt = n
			inv.Attempts = len(inv.History)
			inv.Output = out
			done, retErr := g.finish(inv, secret, nil)
			if tool.SideEffect == SideEffectWrite {
				g.remember(key, done, nil)
			}
			return done, retErr
		}
		flags := failureFlags(err)
		inv.History = append(inv.History, attemptFrom(n, started, finished, err))
		inv.Crash = flags.crash
		inv.Uncertain = flags.uncertain
		last = err
		if !flags.retryable || n == max {
			break
		}
	}
	inv.Attempt = len(inv.History)
	inv.Attempts = len(inv.History)
	done, retErr := g.finish(inv, secret, last)
	if tool.SideEffect == SideEffectWrite && last != nil && failureFlags(last).terminal {
		var stored *Error
		if retErr != nil {
			stored = asError(retErr)
		}
		g.remember(key, done, stored)
	}
	return done, retErr
}

func attemptFrom(number int, started, finished time.Time, err error) Attempt {
	flags := failureFlags(err)
	return Attempt{
		Number:     number,
		Code:       flags.errorCode,
		Crash:      flags.crash,
		Uncertain:  flags.uncertain,
		StartedAt:  started,
		FinishedAt: finished,
	}
}

func (g *Gateway) dispatch(ctx context.Context, d Dispatch) (out json.RawMessage, err error) {
	defer func() {
		if recover() != nil {
			out = nil
			err = fail(CodeInternal, "tool adapter panicked")
		}
	}()
	switch d.Tool.Class {
	case ClassMCP:
		return g.mcp.Invoke(ctx, d)
	case ClassHTTP:
		return g.http.Invoke(ctx, d)
	case ClassWorker:
		return g.workers.Invoke(ctx, d)
	default:
		return nil, fail(CodeInvalidInput, "tool class is invalid")
	}
}

func (g *Gateway) finish(inv Invocation, secret string, err error) (Invocation, error) {
	if err != nil {
		typed := asError(err)
		typed.Message = redact(typed.Message, secret)
		inv.Error = typed
		err = typed
	}
	if secret != "" && (bytes.Contains(inv.Output, []byte(secret)) || bytes.Contains(inv.Input, []byte(secret))) {
		inv.Output = nil
		typed := &Error{Code: CodeCredentialDenied, Message: "tool output contained a credential and was discarded"}
		inv.Error = typed
		err = typed
	}
	inv.FinishedAt = g.nowUTC()
	if inv.History == nil {
		inv.History = []Attempt{}
	}
	return inv, err
}

func checkApproval(now time.Time, tool ToolRecord, approval *Approval) error {
	if approval == nil || !approval.Approved || approval.IdempotencyKey == "" || !validToken(approval.IdempotencyKey) {
		return fail(CodeApprovalRequired, "write requires an approved idempotent decision")
	}
	if approval.TenantID != tool.TenantID || approval.ToolID != tool.ID || approval.ToolVersion != tool.Version {
		return fail(CodeApprovalRequired, "approval does not match the pinned tool")
	}
	if approval.ExpiresAt.IsZero() || !approval.ExpiresAt.After(now) {
		return fail(CodeApprovalRequired, "approval is expired")
	}
	return nil
}

func writeKey(tool ToolRecord, idempotencyKey string) string {
	return tool.TenantID + "\x00" + tool.ID + "\x00" + tool.Version + "\x00" + idempotencyKey
}

func (g *Gateway) replay(key string) (Invocation, error, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	prev, ok := g.writes[key]
	if !ok {
		return Invocation{}, nil, false
	}
	return cloneInv(prev.inv), cloneStoredErr(prev.err), true
}

func cloneStoredErr(err *Error) error {
	if err == nil {
		return nil
	}
	cp := *err
	return &cp
}

func (g *Gateway) begin(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.writes[key]; ok {
		return false
	}
	if _, ok := g.inflight[key]; ok {
		return false
	}
	g.inflight[key] = struct{}{}
	return true
}

func (g *Gateway) end(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.inflight, key)
}

func (g *Gateway) remember(key string, inv Invocation, err *Error) {
	var stored *Error
	if err != nil {
		cp := *err
		stored = &cp
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.writes[key] = writeOutcome{inv: cloneInv(inv), err: stored}
}

func failureFlags(err error) *callFailure {
	var flags *callFailure
	if errors.As(err, &flags) {
		return flags
	}
	typed := asError(err)
	flags = &callFailure{errorCode: typed.Code, message: typed.Message}
	switch typed.Code {
	case CodeOutputSchemaInvalid, CodeResourceLimit, CodeCredentialDenied:
		flags.terminal = true
	}
	return flags
}

func redact(message, secret string) string {
	if secret == "" || message == "" {
		return message
	}
	return strings.ReplaceAll(message, secret, "[redacted]")
}

func newID(prefix string) string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(buf[:])
}
