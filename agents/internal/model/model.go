package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// Deterministic replays one action per completed action in an invocation's history.
// It is safe to share across concurrent invocations and makes no network calls.
type Deterministic struct {
	Steps map[string][]contracts.ModelResult
}

func NewDeterministic(steps map[string][]contracts.ModelResult) *Deterministic {
	return &Deterministic{Steps: steps}
}

func (d *Deterministic) Next(ctx context.Context, input contracts.ModelInput) (contracts.ModelResult, error) {
	if err := ctx.Err(); err != nil {
		return contracts.ModelResult{}, contextError(err)
	}
	key := input.Definition.ID
	steps := d.Steps[key]
	index := len(input.History)
	if index >= len(steps) {
		return contracts.ModelResult{}, &contracts.RunError{Code: contracts.ErrInternal, Message: "No deterministic model action is configured for this turn"}
	}
	result := steps[index]
	if err := validateUsage(result.Usage); err != nil {
		return contracts.ModelResult{}, err
	}
	return result, nil
}

// OpenAIConfig configures the Chat Completions JSON mode transport. Explicit
// token rates are required so recorded cost reflects the selected model's rates.
type OpenAIConfig struct {
	APIKey              string
	Model               string
	Endpoint            string
	InputUSDPerMillion  float64
	OutputUSDPerMillion float64
	Client              *http.Client
}

type OpenAI struct{ config OpenAIConfig }

func NewOpenAI(config OpenAIConfig) (*OpenAI, error) {
	if config.APIKey == "" || config.Model == "" {
		return nil, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Model key and model name are required"}
	}
	if config.Endpoint == "" {
		config.Endpoint = "https://api.openai.com/v1/chat/completions"
	}
	if config.InputUSDPerMillion <= 0 || config.OutputUSDPerMillion <= 0 || math.IsNaN(config.InputUSDPerMillion) || math.IsNaN(config.OutputUSDPerMillion) || math.IsInf(config.InputUSDPerMillion, 0) || math.IsInf(config.OutputUSDPerMillion, 0) {
		return nil, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Positive, finite model token rates are required"}
	}
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 90 * time.Second}
	}
	return &OpenAI{config: config}, nil
}

type completionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (a *OpenAI) Next(ctx context.Context, input contracts.ModelInput) (contracts.ModelResult, error) {
	if err := ctx.Err(); err != nil {
		return contracts.ModelResult{}, contextError(err)
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return contracts.ModelResult{}, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Model input cannot be encoded"}
	}
	requestBody, err := json.Marshal(map[string]any{
		"model":           a.config.Model,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": "You are an agent. Return only a JSON object with exactly one action. Valid forms: {\"type\":\"FINISH\",\"output\":...}, {\"type\":\"TOOL\",\"tool_call\":{\"tool_id\":\"...\",\"input\":...}}, or {\"type\":\"DELEGATE\",\"delegation\":{\"agent_id\":\"...\",\"input\":...}}. Use the supplied definition, history, and allowed capabilities. Never claim to have called a tool or specialist yourself. The coordinator decides whether a requested action is allowed."},
			{"role": "user", "content": string(payload)},
		},
	})
	if err != nil {
		return contracts.ModelResult{}, &contracts.RunError{Code: contracts.ErrInternal, Message: "Model request cannot be encoded"}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.Endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return contracts.ModelResult{}, &contracts.RunError{Code: contracts.ErrInternal, Message: "Model request cannot be created"}
	}
	request.Header.Set("Authorization", "Bearer "+a.config.APIKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := a.config.Client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return contracts.ModelResult{}, contextError(ctx.Err())
		}
		return contracts.ModelResult{}, &contracts.RunError{Code: contracts.ErrInternal, Message: "Model provider request failed"}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return contracts.ModelResult{}, &contracts.RunError{Code: contracts.ErrInternal, Message: fmt.Sprintf("Model provider returned HTTP %d", response.StatusCode)}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return contracts.ModelResult{}, &contracts.RunError{Code: contracts.ErrInternal, Message: "Model provider response could not be read"}
	}
	var completion completionResponse
	if err := json.Unmarshal(body, &completion); err != nil || len(completion.Choices) != 1 || completion.Usage == nil {
		return contracts.ModelResult{}, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Model provider returned an invalid completion"}
	}
	action, err := parseAction(completion.Choices[0].Message.Content)
	if err != nil {
		return contracts.ModelResult{}, err
	}
	usage := contracts.Usage{InputTokens: completion.Usage.PromptTokens, OutputTokens: completion.Usage.CompletionTokens}
	usage.CostUSD = (float64(usage.InputTokens)*a.config.InputUSDPerMillion + float64(usage.OutputTokens)*a.config.OutputUSDPerMillion) / 1_000_000
	if err := validateUsage(usage); err != nil {
		return contracts.ModelResult{}, err
	}
	return contracts.ModelResult{Action: action, Usage: usage}, nil
}

func parseAction(content string) (contracts.ModelAction, error) {
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	var action contracts.ModelAction
	if err := decoder.Decode(&action); err != nil {
		return contracts.ModelAction{}, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Model returned invalid action JSON"}
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return contracts.ModelAction{}, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Model returned extra action content"}
	}
	return action, nil
}

func validateUsage(usage contracts.Usage) error {
	if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CostUSD < 0 || math.IsNaN(usage.CostUSD) || math.IsInf(usage.CostUSD, 0) {
		return &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Model returned invalid usage"}
	}
	return nil
}

func contextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &contracts.RunError{Code: contracts.ErrTimeout, Message: "Model deadline exceeded"}
	}
	return &contracts.RunError{Code: contracts.ErrTimeout, Message: "Model request cancelled"}
}
