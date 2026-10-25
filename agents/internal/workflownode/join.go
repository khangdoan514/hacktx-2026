package workflownode

import (
	"sort"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

// JoinValidated combines node outputs only when every invocation is a
// schema-valid success for the same workflow run, version, and trace.
// It does not start nodes, poll, or read a graph. A running participant
// returns JOIN_NOT_READY and an empty result. A terminal failure or
// cancellation returns JOIN_FAILED and an empty result.
func JoinValidated(workflowRunID string, invocations []Invocation) (JoinedOutput, error) {
	if workflowRunID == "" {
		return JoinedOutput{}, &Error{Code: CodeInvalidInput, Message: "workflow run id is required"}
	}
	if len(invocations) == 0 {
		return JoinedOutput{}, &Error{Code: CodeInvalidInput, Message: "join requires the node invocations to combine"}
	}
	for _, inv := range invocations {
		if inv.Correlation.WorkflowRunID != workflowRunID {
			return JoinedOutput{}, &Error{Code: CodeInvalidInput, Message: "invocation is from a different workflow run"}
		}
		switch inv.State {
		case contracts.StatePending, contracts.StateRunning, "":
			return JoinedOutput{}, &Error{Code: CodeJoinNotReady, Message: "a node invocation is still running"}
		}
	}
	for _, inv := range invocations {
		if inv.State != contracts.StateSucceeded {
			return JoinedOutput{}, &Error{Code: CodeJoinFailed, Message: "a node invocation did not succeed"}
		}
	}

	nodes := make([]JoinedNode, 0, len(invocations))
	seen := make(map[string]struct{}, len(invocations))
	var trace, version string
	for _, inv := range invocations {
		if !inv.OutputValidated {
			return JoinedOutput{}, &Error{Code: CodeOutputSchemaInvalid, Message: "a node output has not been validated"}
		}
		if err := ValidateSchema(inv.OutputSchema, inv.Output); err != nil {
			return JoinedOutput{}, &Error{Code: CodeOutputSchemaInvalid, Message: "a node output does not match the pinned schema"}
		}
		if inv.Correlation.NodeID == "" {
			return JoinedOutput{}, &Error{Code: CodeInvalidInput, Message: "join requires a node id"}
		}
		if _, ok := seen[inv.Correlation.NodeID]; ok {
			return JoinedOutput{}, &Error{Code: CodeInvalidInput, Message: "join requires distinct node ids"}
		}
		seen[inv.Correlation.NodeID] = struct{}{}
		if trace == "" {
			trace = inv.Correlation.TraceID
		} else if inv.Correlation.TraceID != trace {
			return JoinedOutput{}, &Error{Code: CodeInvalidInput, Message: "invocations do not share a trace id"}
		}
		if version == "" {
			version = inv.Correlation.WorkflowVersion
		} else if inv.Correlation.WorkflowVersion != version {
			return JoinedOutput{}, &Error{Code: CodeInvalidInput, Message: "invocations do not share a published workflow version"}
		}
		nodes = append(nodes, JoinedNode{
			NodeID:           inv.Correlation.NodeID,
			NodeInvocationID: inv.Correlation.NodeInvocationID,
			AgentID:          inv.AgentID,
			AgentVersion:     inv.AgentVersion,
			AgentRunID:       inv.Correlation.AgentRunID,
			Output:           cloneRaw(inv.Output),
			Artifacts:        cloneArtifacts(inv.OutputArtifacts),
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].NodeID < nodes[j].NodeID })
	return JoinedOutput{
		WorkflowRunID:   workflowRunID,
		WorkflowVersion: version,
		TraceID:         trace,
		Nodes:           nodes,
	}, nil
}
