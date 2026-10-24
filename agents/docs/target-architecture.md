# Target agent architecture and execution lifecycle

**Status:** intended design for the full agent product. [architecture.md](architecture.md) describes the smaller first pass. This document names future boundaries so first pass code can grow without being mistaken for a production runtime.

```text
Next.js website / workflow engine / API consumers
                |
       authenticated public API
                |
   definition registry + policy check
                |
       run coordinator (Go) <------> durable execution adapter
          |        |                        |
          |        |                   run/attempt store
          |        +--> context resolver ---> scoped knowledge/memory
          +--> model adapter -----------> provider(s)
          +--> tool gateway ------------> isolated workers / MCP / HTTP
          +--> delegation scheduler ----> depth-1 specialist invocations
                |
             event/audit stream ---> traces, metrics, run inspector
```

## Ownership and trust boundaries

**Public API** authenticates the caller, authorizes read/write access, validates request shape, and provides run status. **Registry** resolves an immutable agent version and policy references. **Coordinator** alone creates invocations and interprets model actions. **Context resolver** supplies only permitted data. **Tool gateway** alone authorizes and dispatches tools. **Durable adapter** records checkpoints and retries without changing agent semantics. **Audit/event stream** records decisions and outcomes with redaction. **Workers** run custom code outside the coordinator's process. The application is a client; it does not enforce the final permission decision.

The trust boundary is crossed whenever model output, tool output, retrieved knowledge, or user input proposes an action. Those values are untrusted data. The coordinator and gateway validate the proposed action against pinned definitions, current identity, policy, limits, and schemas.

## End-to-end run lifecycle

1. **Definition resolution:** The caller selects an authorized agent ID/version. Registry validation pins its model policy, schemas, tools, delegate targets, and limits. A missing or retired version cannot start a new run.
2. **Admission:** The API checks caller identity, tenant, input schema, policy, quotas, and an idempotency key for run creation. It creates a run ID and returns a status handle. Future workflow callers use the same execution contract.
3. **Context assembly:** The resolver constructs the initial step/run context from authorized sources. It records references and grants without copying all enterprise knowledge into every prompt.
4. **Model turn:** The adapter formats the authorized prompt and returns one structured action. Usage and duration are recorded. Invalid actions fail or use a bounded correction policy; they never execute first.
5. **Action dispatch:** `FINISH` validates output; `TOOL` goes through the gateway; `DELEGATE` checks target, depth, scope, and shared budgets. A result is added to the agent's action history for a later turn.
6. **Delegation:** A depth-1 child receives selected input/context and its own pinned definition. Independent children can run concurrently. The parent receives their structured results and errors, not their unrestricted private state.
7. **Completion:** The run stores validated output or a typed error, terminal state, usage, and an ordered event. API readers see a consistent snapshot. A workflow caller receives the result through its node interface.
8. **Recovery:** If work stops mid-run, durable checkpoints and attempt records identify completed and uncertain actions. Safe operations may retry; ambiguous writes reconcile or wait for an operator. The run remains inspectable throughout.

## Critical invariants

- Definition versions and execution policy references are pinned for a run; a later edit does not silently alter in-flight behavior.
- At most one terminal outcome exists for a logical invocation. Multiple attempts may exist under that invocation.
- The parent/child relation determines delegation depth. Depth 1 cannot delegate.
- All tool calls use the gateway, including calls initiated inside a specialist. No model or extension can grant itself access.
- An approval decision comes from an authorized human or service identity, not from an agent response.
- A write with an uncertain outcome is not repeated without idempotency or reconciliation.
- Events are append-only, ordered per run, and filtered/redacted for each viewer.

## Data and event model

The Supabase Postgres store separates **definitions**, **runs**, **agent invocations**, **model turns**, **tool invocations**, **attempts**, **approval requests/decisions**, **context/artifact references**, and **events** across delivery passes. Large payloads live in private Supabase Storage with references in the database. Events support live inspection and audit; a run snapshot supports efficient current-state reads. The first pass persists run history; Pass 3 adds durable execution and recovery.

Each event needs run ID, sequence, type, timestamp, actor/invocation ID, correlation ID, and safe metadata. Sensitive payloads use references plus access checks and retention rules. Model prompt text, secrets, and full private context are not emitted by default.

## Failure behavior

| Failure | Expected product behavior |
| --- | --- |
| Invalid user input or model action | Reject with typed error before dispatch; record safe diagnostic. |
| Provider outage or malformed output | Bounded retry only if safe; otherwise fail or pause with visible reason and usage. |
| Tool timeout | Cancel if possible, record attempt; a read may retry, a write needs idempotency/reconciliation. |
| Specialist failure | Return structured failure to orchestrator; its policy decides whether to continue or fail. |
| Budget exhaustion | Stop new actions; terminate with explicit `BUDGET_EXCEEDED` and consumed usage. |
| Worker/process restart | Resume from durable checkpoint; do not repeat completed logical work. |
| Approval timeout | Follow a configured escalation or cancellation policy, recorded as an event. |
| User cancellation | Stop new work, propagate cancellation, retain completed side-effect records. |

## Interfaces with the rest of NMC3

The workflow engine calls a pinned agent definition and observes its state/output as one node; it owns graph readiness, joins, and workflow-level approvals. The existing Next.js website consumes APIs/events to create, test, publish, start, approve, and inspect; it does not directly access provider credentials. Infrastructure supplies PostgreSQL or equivalent metadata storage, a durable executor such as Temporal if validated, secret management, object storage, tracing, and isolated workers. Each is an adapter choice to validate rather than a dependency of the deterministic first pass.
