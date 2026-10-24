package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
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

type OpenAI struct {
	config       OpenAIConfig
	providerCost bool
}

// OpenRouterConfig uses OpenRouter's measured USD cost from response usage.
// No local token-rate table is needed because model routing and cache rates vary.
type OpenRouterConfig struct {
	APIKey   string
	Model    string
	Endpoint string
	Client   *http.Client
}

func NewOpenRouter(config OpenRouterConfig) (*OpenAI, error) {
	if config.APIKey == "" || config.Model == "" {
		return nil, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Model key and model name are required"}
	}
	if !contracts.ValidModelID(config.Model) {
		return nil, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Invalid model ID"}
	}
	if config.Endpoint == "" {
		config.Endpoint = "https://openrouter.ai/api/v1/chat/completions"
	}
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 90 * time.Second}
	}
	return &OpenAI{config: OpenAIConfig{APIKey: config.APIKey, Model: config.Model, Endpoint: config.Endpoint, Client: config.Client}, providerCost: true}, nil
}

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
		PromptTokens     int      `json:"prompt_tokens"`
		CompletionTokens int      `json:"completion_tokens"`
		Cost             *float64 `json:"cost"`
	} `json:"usage"`
}

func (a *OpenAI) Next(ctx context.Context, input contracts.ModelInput) (contracts.ModelResult, error) {
	if err := ctx.Err(); err != nil {
		return contracts.ModelResult{}, contextError(err)
	}
	modelID := a.config.Model
	if a.providerCost && input.Context.ModelID != "" {
		modelID = input.Context.ModelID
	}
	if a.providerCost && !contracts.ValidModelID(modelID) {
		return contracts.ModelResult{}, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Invalid model ID"}
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return contracts.ModelResult{}, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Model input cannot be encoded"}
	}
	requestPayload := map[string]any{
		"model":           modelID,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": "You are an agent. Return a single JSON object with exactly one action: {\"type\":\"FINISH\",\"output\":...}, {\"type\":\"TOOL\",\"tool_call\":{\"tool_id\":\"...\",\"input\":...}}, or {\"type\":\"DELEGATE\",\"delegation\":{\"agent_id\":\"...\",\"input\":...}}. Emit exactly one action per turn; to use two specialists, send one DELEGATE per turn across successive turns. The input object must contain only the fields in the target's input_schema. Available delegates and tools, with their schemas, are listed under delegates and tools in the user message. Never claim to have called a tool or specialist yourself. The coordinator decides whether a requested action is allowed."},
			{"role": "user", "content": string(payload)},
		},
	}
	if a.providerCost {
		requestPayload["usage"] = map[string]bool{"include": true}
	}
	requestBody, err := json.Marshal(requestPayload)
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
		log.Printf("NMC3 model action unparseable (model=%s, bytes=%d): %.300q", modelID, len(completion.Choices[0].Message.Content), completion.Choices[0].Message.Content)
		return contracts.ModelResult{}, err
	}
	usage := contracts.Usage{InputTokens: completion.Usage.PromptTokens, OutputTokens: completion.Usage.CompletionTokens}
	if a.providerCost {
		if completion.Usage.Cost == nil {
			return contracts.ModelResult{}, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "Model provider did not report cost"}
		}
		usage.CostUSD = *completion.Usage.Cost
	} else {
		usage.CostUSD = (float64(usage.InputTokens)*a.config.InputUSDPerMillion + float64(usage.OutputTokens)*a.config.OutputUSDPerMillion) / 1_000_000
	}
	if err := validateUsage(usage); err != nil {
		return contracts.ModelResult{}, err
	}
	return contracts.ModelResult{Action: action, Usage: usage}, nil
}

func parseAction(content string) (contracts.ModelAction, error) {
	decoder := json.NewDecoder(strings.NewReader(extractFirstObject(content)))
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

// extractFirstObject returns the first balanced {...} span so surrounding
// chatter or code fences from chatty providers do not fail an otherwise
// valid action. The extracted object is still strictly validated.
func extractFirstObject(content string) string {
	start := strings.Index(content, "{")
	if start < 0 {
		return content
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(content); i++ {
		c := content[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
		} else if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return content[start : i+1]
			}
		}
	}
	return content
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
