# Shared contracts v1

The source of truth is `agents/internal/contracts/`. `agents/fixtures/api/v1/` fixes the HTTP JSON shape for the first parallel implementation pass. Change either only through the integrator, with all fixtures and contract tests updated together. This is a first pass contract, not a claim that the wider NMC3 platform is complete.

## Package and ownership

The Go module is `github.com/khangdoan514/hacktx-2026/agents`; implementations inside this module import `internal/contracts`. The integrator owns that package and the API fixtures. Package owners own only the paths listed in [parallel-work.md](parallel-work.md). A package may add private types but must not replace a shared type at a boundary.

## Identity, versions, and data

- IDs are opaque, nonempty strings. They are never parsed for meaning. Agent/tool references use ID plus immutable version. The first pass uses `1.0.0`; publishing/version storage is future work.
- `StartRunRequest.model_id`, `Run.model_id`, and `RunContext.model_id` are optional OpenRouter model selectors. Empty means the service default applies. A supplied value must pass `contracts.ValidModelID` (1–128 chars; letters, digits, `/._:-`); syntax violations are `INVALID_INPUT` before any provider call. Unknown provider-side IDs fail as provider errors, not validation.
- JSON values at boundaries use `json.RawMessage`. Validate nonempty, well-formed JSON, then validate against the definition schema before use. No schema means no additional shape restriction; it does not skip JSON validity.
- For the first pass schema validator, support JSON Schema `type`, `properties`, `required`, `items`, `enum`, and `additionalProperties`. Reject unsupported keywords explicitly rather than silently ignoring them. A later validator can expand coverage.
- `Limits` are positive numbers on run submission. An agent definition may set stricter limits; effective limits are the minimum of run and definition values. Zero is invalid in submitted limits. Counters are shared across the entire run, including specialists.
- `Limits.MaxTokens` caps cumulative input plus output tokens; `Limits.MaxCostUSD` caps cumulative estimated USD cost. `ModelAdapter.Next` returns `ModelResult` with an action and nonnegative per-turn `Usage`. The coordinator records and accumulates usage before dispatching each action, including an action that crosses a budget, and exposes cumulative usage on `Run`. Missing, negative, NaN, or infinite provider usage is invalid.
- `RunContext.Artifacts` and `RunEvent.Data` must not contain secrets or hidden instructions. An absent optional value is omitted from JSON. All timestamps are UTC RFC 3339; event sequence, not timestamp, establishes order.
- `Run.ToolInvocations` holds validated tool outcomes for inspection. The store persists these with the run snapshot; events contain only safe tool metadata, not raw tool input or output.

## State and action rules

- A run or invocation starts `PENDING`, moves to `RUNNING`, then exactly one of `SUCCEEDED`, `FAILED`, or `CANCELLED`. Terminal states do not change. Start response may be `PENDING` or `RUNNING` depending on scheduling.
- The root invocation has `ParentID == nil` and depth 0. A child has a parent ID and depth 1. The coordinator derives depth from the parent and never trusts an adapter-supplied depth.
- `ModelAction` must have exactly one payload matching its `Type`: `FINISH` requires valid `Output`, `TOOL` requires `ToolCall`, and `DELEGATE` requires `Delegation`. Reject mismatches as `INVALID_INPUT` before any side effect.
- `ModelInput` carries the agent's allowlisted `delegates` and `tools` definitions so a provider can shape inputs to their schemas. `ToolGateway.Definitions` exposes the registry; the coordinator filters it per agent.
- The coordinator checks delegate allowlists, depth, budgets, and concurrency before dispatch. A depth-1 `DELEGATE` produces `DELEGATION_DEPTH_EXCEEDED`. Only depth 0 can delegate, and only to an allowlisted definition.
- A `TOOL` action goes through `ToolGateway`. The gateway checks the agent's tool allowlist and the input schema before calling the tool. For v1, registered tools must declare `READ_ONLY`.
- Each model turn, tool call, and delegate invocation consumes the corresponding run budget once. On timeout or cancellation, propagate `context.Context` and record a terminal outcome.
- `ActionResult` returns the result of an action to the model adapter on its next turn. A `FINISH` output is validated against the agent's output schema. Never let a model adapter directly execute a tool or agent.

## Store and event rules

- `RunStore.Create` rejects duplicate run IDs. `Get` returns `NOT_FOUND` for missing IDs. `Update` is atomic for one run and rejects state regression. The first pass implements these contracts against Supabase Postgres. Completed runs survive API restarts; automatic recovery of interrupted work is a Pass 3 requirement.
- `EventSink.Append` assigns the next sequence number atomically per run, starting at 1. The supplied `Sequence` is ignored. `List(runID, after)` returns events with `Sequence > after` in increasing order. Empty results are `[]`, not `null`.
- Emit `RUN_STARTED`, `AGENT_STARTED`, `MODEL_STARTED`, `MODEL_SUCCEEDED` or `MODEL_FAILED`, `TOOL_STARTED`, `TOOL_SUCCEEDED` or `TOOL_FAILED`, `DELEGATION_REJECTED`, `AGENT_SUCCEEDED` or `AGENT_FAILED`, and `RUN_SUCCEEDED` or `RUN_FAILED` as applicable. Cancellation uses `AGENT_CANCELLED` and `RUN_CANCELLED`. The event's `InvocationID` identifies the actor when applicable.
- An error is `{code,message}`. Messages are safe to show to users and must not include secrets, hidden prompts, or tool credentials.

## HTTP v1

All bodies are JSON. Successful GET responses use the wrappers in `api.go`; errors use `ErrorResponse`. The API agent owns routing and transport status; the runtime owns execution decisions.

| Method/path | Success | Error cases |
| --- | --- | --- |
| `POST /v1/agent-runs` | `202 Accepted`, `StartRunResponse` | `400 INVALID_INPUT`; `404 NOT_FOUND` for unknown agent version; `500 INTERNAL` |
| `GET /v1/agent-runs/{run_id}` | `200 OK`, `RunResponse` | `404 NOT_FOUND` |
| `GET /v1/agent-runs/{run_id}/events?after=N` | `200 OK`, `EventsResponse` | `400 INVALID_INPUT` for malformed cursor; `404 NOT_FOUND` |
| `GET /v1/agents` | `200 OK`, `AgentsResponse` | `500 INTERNAL` |

The POST endpoint accepts `StartRunRequest`, starts work asynchronously, and returns the run ID immediately. `GET` supplies the current snapshot. A run can be observed before it completes. The `after` cursor defaults to 0. The first pass has no authentication; deployment must add it before handling real organizational data.

## How to change v1

Propose the specific field or signature change to the integrator, including affected packages. The integrator updates `internal/contracts`, all affected `fixtures/api/v1` files, this document, and the contract test in one change. Package owners then rebase on that revision. Do not make incompatible copies of the contract inside implementation packages.
