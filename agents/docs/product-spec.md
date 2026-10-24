# NMC3 agent system: product specification

**Status:** target product design, October 2026. This describes the agent portion of NMC3 end to end. It is based on the supplied platform PRD and is the intended direction, not a list of features already implemented. The [first pass](first-pass.md) and [contracts v1](contracts-v1.md) are the first delivery slice.

## 1. Purpose and boundaries

NMC3 lets an organization define reusable AI agents, grant them specific capabilities, run them alone or under one orchestrator, and inspect every consequential action. The Go agent service is general purpose: it owns agent definitions, model interactions, bounded delegation, tool calls, context resolution, execution records, and agent-specific policy enforcement behind versioned APIs. The existing Next.js website in `frontend/` is the first integrated client. Other applications and the NMC3 workflow engine can use the same contracts; the workflow engine owns DAG scheduling, joins, conditional edges, and workflow-wide recovery.

The central product promise is that an agent can reason within a **runtime-enforced envelope**. Instructions guide behavior; identity, permissions, budgets, approvals, and data access are checked in code at the point of action.

## 2. Users and jobs

| User | What they need |
| --- | --- |
| Agent developer | Define an agent, test it with fixtures, publish a version, call it from an API or workflow, and debug a failed run. |
| Platform administrator | Control models, tools, credentials, limits, permissions, retention, and allowed deployments. |
| Business operator | Start an approved agent or workflow, provide task inputs, respond to approval requests, and inspect the outcome. |
| Auditor or supervisor | Reconstruct who started a run, what definitions and policies were used, what tools were called, and why a protected action was allowed or denied. |

### Core journeys

1. **Create and publish:** A developer drafts an agent with a role, instructions, schemas, model policy, tool allowlist, context grants, limits, and allowed delegate targets. Validation and a test run pass. Publishing creates an immutable version. A later edit creates a new version; active runs retain their pinned version.
2. **Invoke and inspect:** An authorized caller supplies a definition version and schema-valid input. The runtime creates a run, resolves scoped context, processes model actions, and returns validated output or a typed failure. The caller can inspect current state and an ordered event trail.
3. **Coordinate specialists:** A depth-0 orchestrator delegates to allowed specialists at depth 1. Specialists can use permitted tools but cannot delegate. The orchestrator receives only explicitly forwarded outputs and produces a final result. Concurrent specialists share run-level budgets.
4. **Use a protected tool:** Before a tool call, the gateway checks identity, agent permissions, input schema, resource scope, side-effect class, policy, and approval requirement. An approved write uses an idempotency key and records its outcome. A denied or ambiguous operation is visible in the run.
5. **Recover and operate:** If a worker stops, durable state allows safe resumption. Completed operations are not repeated simply because a process restarted. Operators can see retries, outstanding approvals, cancellation, and resource use.

## 3. Capabilities and requirements

### Agent definition and lifecycle

An agent definition has an ID and immutable published version; name and description; role and instructions; input/output schemas; model/provider policy; allowed tools and delegate targets; context grants; execution limits; and governance policy references. Draft definitions can be edited and tested. Publishing validates references and schema compatibility, records the publisher and timestamp, and freezes the version. A run pins the exact agent, tool, and policy versions needed to explain its behavior. Retirement blocks new runs without rewriting history.

The website's definition editor, declarative JSON/YAML files, and other API clients must produce the same canonical definition. Import/export and migration rules need explicit versioning. Secrets are referenced by identifier, never embedded in a definition or event. A distributable client SDK can make API use easier for external developers, but the API remains complete without one.

### Model execution loop

The runtime exposes a provider-neutral adapter. Each turn receives the agent definition, scoped task input, allowed tool/delegate descriptions, prior action results, and remaining limits. It returns a structured action: finish, call a tool, or request delegation. The coordinator validates the action shape and policy before dispatch. The runtime enforces model-turn count, token/cost limits, deadlines, cancellation, and output schema validation. Provider errors, invalid structured output, context-window overflow, and exhausted budgets produce distinct outcomes.

Model adapters must not be able to execute tools or agents directly. Provider-specific prompt formatting, streaming, and usage accounting remain behind the adapter boundary. The deterministic adapter remains useful for tests and repeatable first-pass runs.

### Orchestration and delegation

Only the depth-0 orchestration agent may delegate. It may invoke multiple depth-1 specialists concurrently within a shared limit. A specialist may call allowed tools but cannot invoke an agent, even if its instructions or tool output request it. Each delegation specifies target version, schema-valid input, selected context, parent invocation, and remaining budget. The runtime records the decision and result. A specialist sees only its own instructions, forwarded task data, and granted context; it does not inherit the orchestrator's complete transcript or tool permissions.

The initial depth rule is fixed at **1**. Changing it would require a new product decision, policy model, and tests. The orchestration agent can make decisions inside a deterministic workflow node, but cannot change a published workflow graph or bypass its dependencies and approval steps.

### Tools and extensions

The tool registry stores ID/version, description, schemas, side-effect class, required permissions, credential reference, timeout, retry policy, and execution endpoint. The gateway performs authorization and schema checks, records invocation metadata, and applies timeout/cancellation. Supported target integration classes are MCP servers, HTTP services, internal services, and custom Go tools. Custom code should run in separately deployed workers with resource and network restrictions, outside the central orchestration process.

Read-only calls can use bounded retries. Write calls require policy approval where configured, stable idempotency keys where supported, and reconciliation when the outcome is uncertain. The runtime must never infer that a timed-out write failed and blindly repeat it.

### Context and memory

The context resolver assembles four logical scopes: step context (one invocation), run context (shared artifacts and results), agent memory (authorized persisted information), and organization context (enterprise knowledge and policy). Access is explicit by identity, agent grant, purpose, and run. Data passed to a specialist is selected and recorded. Retrieval results are treated as untrusted data; they cannot grant new permissions. The first pass uses step and run context. Persisted memory and organization retrieval are later capabilities with their own retention and authorization rules.

### Security and governance

Every run carries a tenant, requesting principal, effective policy identity, and trace ID in the target design. Authorization is checked on starting runs, resolving context, delegating, invoking tools, viewing run data, and making approval decisions. Policies can limit models, tools, target agents, budgets, and data classifications. Credentials are fetched just in time by the gateway or isolated worker and are never returned to a model or stored in an event. Tenant boundaries apply to definitions, runs, context, credentials, and logs. Prompt injection is handled as untrusted input at model-facing boundaries; the authorization layer remains authoritative.

### Runs, recovery, and approvals

A run records its pinned definitions, input, output/error, invocation tree, model turns, tool calls, approvals, usage, and ordered events. Logical states include pending, running, waiting, succeeded, failed, cancelled, and skipped where a workflow owns the enclosing node. Attempts are separate records so a retry does not erase a prior failure. Durable storage must preserve enough information to resume after worker failure. Approval requests have a policy-defined approver, expiry, decision, and audit record. Cancellation propagates through active model and tool calls; completed external side effects remain recorded.

### Observability and operations

The product exposes live status, invocation parentage, tool and model events, retries, approvals, errors, duration, token usage, estimated cost, and trace/correlation IDs. Logs and traces must be linkable to a run without exposing secrets or unauthorized context. Operators need filtering by tenant, agent/version, state, time, and error; retry and failure rates; budget consumption; and a way to distinguish model quality failures from platform failures. Retention and redaction policies govern all stored inputs, outputs, and traces.

## 4. External surfaces

- **Go service API:** manage definitions and versions, start/cancel runs, retrieve run snapshots and events, submit approvals, and list authorized tools/agents. The first slice implements only the four endpoints in [contracts v1](contracts-v1.md).
- **Existing website:** the Next.js application is the first user-facing client. It calls the same documented Go API available to other clients, directly or via a thin server-side proxy. The service enforces permissions; the website presents capabilities and state.
- **Optional client SDK:** a convenience wrapper around the public API for external developers, if demand justifies it. It does not define separate agent behavior.
- **Workflow engine:** invokes a pinned agent version as a node and receives state, validated output, events, and cancellation hooks. The workflow engine remains responsible for graph scheduling.
- **Website features:** definition editor, test playground, run inspector, approval inbox, tool management, and administration arrive across the passes. The first pass needs start and inspect.
- **Worker protocol:** dispatches isolated tool execution and reports typed outcomes. Transport and orchestration are still open decisions.

## 5. Quality bar

- **Correctness:** permissions, depth, schemas, budgets, and state transitions are enforced by runtime tests, including denied and malformed actions.
- **Reliability:** a restart does not lose durable progress or silently duplicate a protected write; ambiguous writes require reconciliation.
- **Security:** an agent cannot gain a tool, context scope, or delegate target through prompt text or retrieved content; tenant and viewer authorization are checked at access time.
- **Operability:** every terminal run has a comprehensible status, error or output, and ordered audit trail; failures can be correlated across services.
- **Performance:** independent specialists can run concurrently within configured limits; status is available while a run is active.
- **Portability:** the Go orchestration service is independently deployable from the application and uses replaceable model, store, and durable-execution adapters.

Quantitative latency, throughput, concurrency, cost, and availability targets will be set after representative load and deployment constraints are known. Until then, acceptance should use explicit behavior and failure tests rather than invented numbers.

## 6. Scope by delivery pass

The full feature-to-pass map and acceptance checks are in [pass-specs.md](pass-specs.md).

| Stage | Product outcome | Key limits |
| --- | --- | --- |
| Pass 1: functional core | Model-backed orchestration, two specialists, read-only tool, Supabase run history, API/events, minimal UI. | No automatic in-flight recovery or production auth. |
| Pass 2: definition lifecycle | Draft/test/publish/retire, immutable versions, website editor. | Definition storage and schema evolution required. |
| Pass 3: durable runs | Attempts, checkpoints, restart recovery, cancellation atop existing run history. | Safe write support waits for Pass 4. |
| Pass 4: governance | Identity, tenant policy, secrets, approvals, safe writes. | Policy and isolation validation required. |
| Pass 5: context | Scoped memory, knowledge, retrieval, provenance. | Access and retention policy required. |
| Pass 6: integrations | MCP/HTTP/internal tools, Go extension SDK, isolated workers. | Worker isolation must be validated. |
| Pass 7: platform integration | Agent nodes in workflows and full application flows. | Depends on adjacent workflow and UI workstreams. |
| Pass 8: operations | Self-hosted deployment, scaling, observability, measured targets. | Requires security and operations review. |

## 7. Decisions still open

The team must choose a durable execution backend; schema dialect and evolution policy; model providers; credential and secret system; approval expiry/escalation policy; worker protocol and isolation level; tenant isolation model; retention/redaction policy; run version migration behavior; and measurable service targets. Record each decision with a rationale and tests before treating it as implemented behavior.
