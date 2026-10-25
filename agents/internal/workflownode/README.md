# Workflow node contract

This package is the agent-service side of a workflow node. An external workflow uses it to invoke one pinned agent version and read the result. It checks schema-valid input and output, records correlation and policy identity, carries artifact references, and stops new actions when the workflow cancels the invocation.

It does not schedule a graph. DAG validation, readiness, conditions, parallel dispatch, join policy, workflow versioning, workflow-wide durability, and approval placement stay with the workflow engine. `Join` and `JoinValidated` only combine invocations the caller already selected, and they return nothing until every participant is a schema-valid success.

## Call pattern

1. Resolve a published `agent_id` and `agent_version`. The version on the request is the version that runs. A later publish does not change this call. A missing, retired, or mismatched version fails with `NOT_FOUND` and does not open a session.
2. Send `InvokeRequest` with workflow correlation, policy identity, JSON input, artifact references, and positive limits. The workflow supplies every correlation id except `agent_run_id`. The agent service assigns that id when the session opens.
3. The service validates the pinned input schema, then opens an `Executor` session. Each proposed `TOOL` or `DELEGATE` is committed only if this package authorizes it. Depth limits and tool allowlists are enforced inside the agent service on `Commit`, not here.
4. A `FINISH` is a success only after the output matches the pinned output schema and its artifact references are well formed. Invalid output is kept on the invocation for inspection, marked failed, and is not committed.
5. The same workflow run id and node invocation id replay the original invocation. A different request body for that id returns `CONFLICT` and does not open a second run.

## Correlation, identity, and artifacts

`Correlation` carries `workflow_id`, `workflow_version`, `workflow_run_id`, `node_id`, `node_invocation_id`, and `trace_id`. Those values are copied onto the invocation and onto every node event. `agent_run_id` is empty in the executor request and filled from `Session.RunID` before `NODE_STARTED`.

`PolicyIdentity` is the tenant, principal, and policy the workflow presents. The invocation keeps that identity. A `POLICY_OVERRIDE` proposal is rejected and is not committed.

`ArtifactRef` is an id plus an object key. Bytes and secrets stay in the artifact store. `UnmarshalArtifactRef` rejects unknown JSON fields, so an inline payload cannot ride along. Input references stay on `Invocation.Artifacts`. References produced by a validated finish are `Invocation.OutputArtifacts`.

## Schemas

`ValidateSchema` implements the v1 subset: `type`, `properties`, `required`, `items`, `enum`, and `additionalProperties` as a boolean. Any other keyword fails the invocation before a session opens. An empty schema still requires one JSON value.

## Cancellation

`Cancel(workflowRunID, nodeInvocationID)` is the workflow signal. It marks a non-terminal invocation `CANCELLED`, cancels the session context, and records `NODE_CANCELLED`. The action loop does not commit another action after that. An action already inside `Commit` is the executor's responsibility; this package does not start the next one. Completed side effects remain the executor's records.

A caller context that is already cancelled never opens a session. Cancelling a terminal invocation returns it unchanged and does not append another event.

## Graph and approvals

There is no method that edits a workflow. A `GRAPH_MUTATION` proposal fails with `GRAPH_IMMUTABLE` and is not committed. The stored `workflow_version` stays the version the workflow sent.

There is no method that approves, denies, or skips a gate. An `APPROVAL_DECISION` proposal fails with `APPROVAL_BYPASS`. Text inside a validated output does not change `Invocation.Approval`.

A protected tool is committed only when the workflow passes an `ApprovalGate` it has already set to `APPROVED` with a decision principal. Pending, denied, expired, and missing gates fail with `APPROVAL_REQUIRED` before `Commit`. This package does not wait or resume; the workflow decides when a protected node may run.

## State and events

Node state uses the agent run states:

| State | Meaning |
| --- | --- |
| `PENDING` | Admitted. No agent session yet. |
| `RUNNING` | Session is open and `agent_run_id` is set. |
| `SUCCEEDED` | Output passed the pinned schema. |
| `FAILED` | Input, output, graph, approval, policy, or executor rejection. |
| `CANCELLED` | Workflow cancellation observed. No further actions are committed. |

`WAITING` is not a node state. A pause for approval is a workflow state.

Node events are owned by this package and sequenced per workflow run, starting at 1. The sequence on an appended event is ignored. `List` returns sequence order. Tool input, model instructions, and output bodies are not copied into event data. Tool and model events stay on the agent run identified by `agent_run_id`. `NODE_ACTION_STARTED` means this package authorized a tool or delegate and called `Commit`.

`MemoryLog` is the in-process log. A durable log implements the same `EventLog` interface. Append still records the cancellation row when the caller context is already cancelled.

## Join

`Service.Join` loads the named invocations. `JoinValidated` is the pure check:

- any pending or running participant returns `JOIN_NOT_READY` and an empty result
- any failed or cancelled participant returns `JOIN_FAILED` and an empty result
- a success whose output was not validated, or no longer matches the pinned schema, returns `OUTPUT_SCHEMA_INVALID`
- otherwise the result lists the validated outputs, sorted by node id, with the shared workflow version and trace id

The function does not launch nodes, poll, or inspect edges. The workflow chooses the parallel set and calls `Join` when it wants the combined result.

## Integration

Wire a `Catalog` of immutable published definitions and an `Executor` whose `Session` surfaces agent proposals and applies only actions passed to `Commit`. Map coordinator failures such as `DELEGATION_DEPTH_EXCEEDED` through `Commit`; do not reimplement them here.

HTTP routes for workflow and UI clients are not in this package. `Get`, `Events`, and `Join` are the in-process reads those routes would call. A durable event log and the real coordinator adapter are integrator work.

## Tests

Package tests use a fake catalog and a fake session. They cover two parallel invocations whose outputs join only after both validate, a join that stays empty when one output fails schema checks, workflow cancellation that commits no further action, graph and approval rejection, version pinning, and correlated event order. No test calls a live model or a database.
