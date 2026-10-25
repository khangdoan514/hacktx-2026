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

## HTTP

`NewHandler` is the HTTP surface for one in-process `Service`. Workflow editing, run visualization, and the approval inbox stay in the application workstream. This handler checks one node invocation at a time.

| Method | Path | Call |
| --- | --- | --- |
| `POST` | `/v1/workflow-node/invocations` | `Invoke` |
| `POST` | `/v1/workflow-node/runs/{workflowRunID}/invocations/{nodeInvocationID}/cancel` | `Cancel` |
| `GET` | `/v1/workflow-node/runs/{workflowRunID}/invocations/{nodeInvocationID}` | `Get` |
| `GET` | `/v1/workflow-node/runs/{workflowRunID}/events?after=` | `Events` |
| `POST` | `/v1/workflow-node/runs/{workflowRunID}/join` | `Join` |

`POST /v1/workflow-node/invocations` takes one `InvokeRequest` object and blocks until that invocation is terminal. Parallel nodes are concurrent requests. The response is HTTP 200 and the invocation, including failed and cancelled outcomes. Read `state` and `error`.

The same `workflow_run_id` and `node_invocation_id` returns the original invocation and does not open another session. A different body for that pair is HTTP 409:

```json
{"error":{"code":"CONFLICT","message":"invocation id already exists with a different request"}}
```

`Cancel` uses an empty body. It marks a non-terminal invocation `CANCELLED`, cancels the session context, and does not commit another action. The response is HTTP 200 and that invocation. A terminal invocation is returned unchanged, and no further event is appended.

`GET .../events` takes an optional `after` sequence. The body is `{"workflow_run_id","events"}`. A workflow run with no events returns an empty list. Tool input, model instructions, and output bodies stay off the event records.

`POST .../join` takes `{"node_invocation_ids":["..."]}`. HTTP 200 and a `JoinedOutput` are returned only when every named invocation is a schema-valid success. A pending or running participant is HTTP 409 `JOIN_NOT_READY`. A failed or cancelled participant is HTTP 409 `JOIN_FAILED`. The response then has no `nodes` array. Unknown JSON fields are HTTP 400 `INVALID_INPUT`. An unknown invocation is HTTP 404 `NOT_FOUND`.

The request context is the `Invoke` context. Closing the connection cancels that call the same way a cancelled context does. Workflow cancellation is the cancel route, which can run while another request is still inside `Invoke`.

Any other method on a listed path is HTTP 405 `INVALID_INPUT`. Authentication stays at the service edge.

## Mount

`agents/cmd/nmc3-agent/main.go` still serves `httpapi.New` alone. The block below is the integrator mount. It is not applied in this tree.

`catalog` implements `Catalog`. `executor` implements `Executor`. `run()` already has an OpenCode `coordinator` (`contracts.Coordinator`); that value is a separate type from `Executor`. `NewMemoryLog` keeps node events in the process. A durable `EventLog` is integrator work. This package has no Postgres migration.

Add this import next to the other `agents/internal` imports:

```go
"github.com/khangdoan514/hacktx-2026/agents/internal/workflownode"
```

Replace this line in `run()`:

```go
server := http.Server{Addr: addr, Handler: httpapi.New(coordinator, runs, events, definitions), ReadHeaderTimeout: 10 * time.Second}
```

with:

```go
nodeLog := workflownode.NewMemoryLog()
nodeService, err := workflownode.New(catalog, executor, nodeLog, nil)
if err != nil {
	return err
}
mux := http.NewServeMux()
mux.Handle("/v1/workflow-node/", workflownode.NewHandler(nodeService))
mux.Handle("/", httpapi.New(coordinator, runs, events, definitions))
server := http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
```

Mount the workflow prefix without `http.StripPrefix`. `NewHandler` matches full paths. `/` remains the agent API.

## Integration

Wire a `Catalog` of immutable published definitions and an `Executor` whose `Session` surfaces agent proposals and applies only actions passed to `Commit`. Map coordinator failures such as `DELEGATION_DEPTH_EXCEEDED` through `Commit`; do not reimplement them here.

A durable event log and the executor adapter that satisfies `Executor` are integrator work.

## Tests

Package tests use a fake catalog and an Executor double (`fakeExec`) whose sessions complete independently. They cover two parallel invocations whose outputs join only after both validate, a join that stays empty when one output fails schema checks, workflow cancellation that commits no further action and leaves the other invocation running, an unapproved protected tool that does not commit, graph and approval rejection, version pinning, and correlated event order. HTTP tests drive Invoke, Cancel, Get, Events, and Join through that double, including replay and `CONFLICT`. No test calls a live model or a database.
