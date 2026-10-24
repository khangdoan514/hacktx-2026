package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// Gateway is a registry of read-only tools and the sole authorization boundary.
type Gateway struct{ handlers map[string]contracts.Tool }

func (g *Gateway) Definitions() []contracts.ToolDefinition {
	definitions := make([]contracts.ToolDefinition, 0, len(g.handlers))
	for _, handler := range g.handlers {
		definitions = append(definitions, handler.Definition())
	}
	return definitions
}

var _ contracts.ToolGateway = (*Gateway)(nil)

func NewGateway(handlers ...contracts.Tool) (*Gateway, error) {
	g := &Gateway{handlers: make(map[string]contracts.Tool)}
	for _, handler := range handlers {
		if handler == nil {
			return nil, fmt.Errorf("nil tool")
		}
		def := handler.Definition()
		if def.ID == "" || def.Version == "" || def.SideEffect != "READ_ONLY" || def.TimeoutSeconds <= 0 {
			return nil, fmt.Errorf("invalid read-only tool definition")
		}
		if err := ValidateSchema(def.InputSchema); err != nil {
			return nil, fmt.Errorf("invalid tool input schema: %w", err)
		}
		if err := ValidateSchema(def.OutputSchema); err != nil {
			return nil, fmt.Errorf("invalid tool output schema: %w", err)
		}
		key := def.ID
		if _, exists := g.handlers[key]; exists {
			return nil, fmt.Errorf("duplicate tool %s", key)
		}
		g.handlers[key] = handler
	}
	return g, nil
}

func (g *Gateway) Invoke(ctx context.Context, agent contracts.AgentDefinition, invocation contracts.AgentInvocation, request contracts.ToolRequest) (contracts.ToolInvocation, error) {
	now := time.Now().UTC()
	result := contracts.ToolInvocation{ID: newID(), RunID: invocation.RunID, AgentInvocationID: invocation.ID, ToolID: request.ToolID, Input: request.Input, StartedAt: now}
	fail := func(code contracts.ErrorCode, message string) (contracts.ToolInvocation, error) {
		err := &contracts.RunError{Code: code, Message: message}
		finished := time.Now().UTC()
		result.FinishedAt = &finished
		result.Error = err
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return fail(contextCode(err), "tool context ended")
	}
	allowed := false
	for _, name := range agent.AllowedTools {
		if name == request.ToolID {
			allowed = true
			break
		}
	}
	if !allowed {
		return fail(contracts.ErrToolNotAllowed, "tool is not allowed for this agent")
	}
	handler := g.handlers[request.ToolID]
	if handler == nil {
		return fail(contracts.ErrNotFound, "tool not found")
	}
	def := handler.Definition()
	result.ToolVersion = def.Version
	if err := ValidateJSON(def.InputSchema, request.Input); err != nil {
		return fail(contracts.ErrInvalidInput, "invalid tool input")
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(def.TimeoutSeconds)*time.Second)
	defer cancel()
	type outcome struct {
		data json.RawMessage
		err  error
	}
	ch := make(chan outcome, 1)
	go func() { data, err := handler.Invoke(callCtx, request.Input); ch <- outcome{data, err} }()
	select {
	case <-callCtx.Done():
		return fail(contextCode(callCtx.Err()), "tool context ended")
	case out := <-ch:
		if out.err != nil {
			if errors.Is(out.err, context.DeadlineExceeded) || errors.Is(out.err, context.Canceled) {
				return fail(contextCode(out.err), "tool context ended")
			}
			var runErr *contracts.RunError
			if errors.As(out.err, &runErr) {
				return fail(runErr.Code, "tool failed")
			}
			return fail(contracts.ErrInternal, "tool failed")
		}
		if err := ValidateJSON(def.OutputSchema, out.data); err != nil {
			return fail(contracts.ErrOutputSchemaInvalid, "invalid tool output")
		}
		result.Output = out.data
		finished := time.Now().UTC()
		result.FinishedAt = &finished
		return result, nil
	}
}

func contextCode(err error) contracts.ErrorCode {
	if errors.Is(err, context.DeadlineExceeded) {
		return contracts.ErrTimeout
	}
	return contracts.ErrTimeout
}

func newID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return "tool-" + hex.EncodeToString(raw[:])
}
