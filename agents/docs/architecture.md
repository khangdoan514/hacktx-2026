# Agent runtime architecture

Status: proposed design for the first pass, derived from the supplied NMC3 PRD. The full product boundaries and lifecycle are in [target-architecture.md](target-architecture.md). The concrete first pass type and wire contract is in [contracts-v1.md](contracts-v1.md).

```text
caller / future Next.js UI
          |
          v
      Go HTTP API  ---> Supabase Postgres run store + event log
          |
          v
   agent run coordinator
      |          |
      |          +--> tool gateway --> registered read-only tools
      +--> model adapter --> real provider / deterministic test adapter
      |
      +--> authorized specialist invocations (depth 1 only)
```

The Go service owns agent definitions, invocation state, delegation, tool authorization, and events. The caller sees an API and never directly instructs a specialist or invokes a privileged tool. The current FastAPI backend may later proxy or consume this API; no such integration exists yet.

## Core contracts for the first pass

| Contract | Minimum fields | Invariant |
| --- | --- | --- |
| `AgentDefinition` | ID, version, role/instructions, input schema, output schema, allowed tools, allowed delegates, limits | Published versions are immutable. |
| `AgentInvocation` | ID, run ID, parent ID, definition ID/version, depth, input, state, output/error | Only the coordinator creates invocations. |
| `ToolDefinition` | ID/version, input/output schema, side-effect class, timeout | The gateway validates and authorizes each call. |
| `ToolInvocation` | ID, agent invocation ID, tool ID/version, input, result/error, timing | All calls produce a run event. |
| `RunContext` | Run ID, user request, scoped artifacts, remaining budgets | Specialists receive only explicitly selected data. |
| `RunEvent` | Sequence, run ID, invocation ID, type, timestamp, metadata | Events permit an ordered run inspection. |

The shared Go types and fixtures now exist in `agents/internal/contracts/` and `agents/fixtures/api/v1/`. See [contracts-v1.md](contracts-v1.md) for the supported first pass JSON Schema subset and exact rules. Keep internal Go types as the canonical runtime representation, with explicit serialization at the API boundary. The first version pins definition and tool versions at run start.

## Invocation rules

1. The coordinator validates the requested agent version and input.
2. It assigns run and invocation IDs, a deadline, and remaining budgets.
3. It resolves only the context and tools allowed by that definition.
4. The adapter produces a result or a request for an authorized tool/delegate action.
5. The coordinator validates every action, records it, dispatches it, and returns its result to the caller agent.
6. It validates the final output and emits a terminal event.

Delegation is allowed only when the caller has depth 0, the target is on the caller's allowlist, and run-level concurrency and call budgets remain. Depth is checked in code using the parent invocation; it is never accepted from model output. A specialist's attempted delegation fails visibly. Agent and tool execution obey cancellation and deadlines.

## API sketch

These paths are draft contracts, not implemented endpoints:

- `POST /v1/agent-runs` — submit a task and return `{run_id, state}`.
- `GET /v1/agent-runs/{run_id}` — current state, final output or error, invocation summary.
- `GET /v1/agent-runs/{run_id}/events` — ordered events for inspection or polling.
- `GET /v1/agents` — available agent IDs and versions for the first pass.

Use typed errors such as `INVALID_INPUT`, `AGENT_NOT_ALLOWED`, `DELEGATION_DEPTH_EXCEEDED`, `TOOL_NOT_ALLOWED`, `BUDGET_EXCEEDED`, `TIMEOUT`, and `OUTPUT_SCHEMA_INVALID`. Never put credentials or full hidden prompts in run events.

## Persistence and security boundary

The first pass stores runs and events in Supabase Postgres so completed work remains inspectable after an API restart. Pass 3 adds automatic recovery of interrupted executions. The eventual platform also needs authorization, tenant scoping, audit retention, and protected write approval. This design centralizes all tool and delegation calls so those checks can be added at one boundary.
