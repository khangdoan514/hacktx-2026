package dummy

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
)

type scriptFile map[string][]struct {
	Action contracts.ModelAction `json:"action"`
}

type Script struct {
	mu      sync.Mutex
	steps   map[string][]contracts.ModelAction
	cursor  map[string]int
	onTurn  func(agentID string)
	release <-chan struct{}
}

func LoadScript() (*Script, error) {
	raw, err := ReadRepo("agents/examples/deterministic-actions.json")
	if err != nil {
		return nil, err
	}
	var parsed scriptFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	steps := map[string][]contracts.ModelAction{}
	for id, actions := range parsed {
		for _, action := range actions {
			action.Action = stripUsage(action.Action)
			steps[id] = append(steps[id], action.Action)
		}
	}
	steps["analysis-specialist"] = []contracts.ModelAction{{
		Type:   contracts.ActionFinish,
		Output: json.RawMessage(`{"summary":"Vendor A remains inside the fictional comparison."}`),
	}}
	diff, err := ReadRepo("agents/fixtures/pr-review/sample.diff")
	if err != nil {
		return nil, err
	}
	encodedDiff, _ := json.Marshal(string(diff))
	steps["pr-review-orchestrator"] = []contracts.ModelAction{
		{Type: contracts.ActionDelegate, Delegation: &contracts.DelegateRequest{AgentID: "code-quality-specialist", Input: json.RawMessage(`{"diff":` + string(encodedDiff) + `}`)}},
		{Type: contracts.ActionDelegate, Delegation: &contracts.DelegateRequest{AgentID: "test-security-specialist", Input: json.RawMessage(`{"diff":` + string(encodedDiff) + `}`)}},
		{Type: contracts.ActionFinish, Output: json.RawMessage(`{"summary":"The fictional change adds amounts without a sign check.","finding_ids":["quality-sign","security-total"]}`)},
	}
	steps["code-quality-specialist"] = []contracts.ModelAction{
		{Type: contracts.ActionTool, ToolCall: &contracts.ToolRequest{ToolID: "diff.read", Input: json.RawMessage(`{"diff":` + string(encodedDiff) + `}`)}},
		{Type: contracts.ActionFinish, Output: json.RawMessage(`{"findings":[{"line":6,"message":"Amount is added without a sign check."}]}`)},
	}
	steps["test-security-specialist"] = []contracts.ModelAction{
		{Type: contracts.ActionTool, ToolCall: &contracts.ToolRequest{ToolID: "diff.read", Input: json.RawMessage(`{"diff":` + string(encodedDiff) + `}`)}},
		{Type: contracts.ActionFinish, Output: json.RawMessage(`{"findings":[{"line":4,"message":"The total starts at zero with no overflow guard."}]}`)},
	}
	return &Script{steps: steps, cursor: map[string]int{}}, nil
}

func (s *Script) ObserveSpecialists(started chan<- string, release <-chan struct{}) {
	s.onTurn = func(agentID string) { started <- agentID }
	s.release = release
}

func stripUsage(action contracts.ModelAction) contracts.ModelAction {
	return action
}

func (s *Script) Next(_ context.Context, input contracts.ModelInput) (contracts.ModelResult, error) {
	s.mu.Lock()
	index := s.cursor[input.Invocation.ID]
	s.cursor[input.Invocation.ID] = index + 1
	actions := s.steps[input.Definition.ID]
	var action contracts.ModelAction
	if index < len(actions) {
		action = actions[index]
	} else {
		action = contracts.ModelAction{Type: contracts.ActionFinish, Output: json.RawMessage(`{}`)}
	}
	hook := s.onTurn
	s.mu.Unlock()
	if input.Invocation.Depth == 1 && index == 0 {
		if hook != nil {
			hook(input.Definition.ID)
		}
		if s.release != nil {
			<-s.release
		}
	}
	return contracts.ModelResult{Action: action, Usage: contracts.Usage{}}, nil
}
