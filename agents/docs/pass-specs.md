# Agent product delivery passes

**Planning answer:** eight passes cover the agent capabilities in [product-spec.md](product-spec.md). A pass is a vertical product increment with a working end-to-end acceptance check, not a fixed sprint or time estimate. Passes 5 and 6 can proceed partly in parallel after Pass 4 policy contracts are stable. The full NMC3 DAG workflow engine remains a separate product workstream; Pass 7 specifies its integration with agents rather than assigning its entire implementation to `agents/`.

The Go service is a general-purpose product with a complete public API. The existing Next.js website in `frontend/` is the first integrated client, and each relevant pass adds a website flow for its new capability. The service must also work through the API without the website. A client SDK is optional; the Go extension SDK in Pass 6 serves a different purpose by helping developers build custom tools. Each pass keeps existing published definitions and completed runs readable. A contract change must specify migration, compatibility, and fixture updates. “Complete” means code, tests, API behavior, operator notes, and the required website flow.

## Pass 1 — functional agent core

**Outcome:** A user can start and inspect a model-backed run in which one orchestrator coordinates two specialists and a read-only tool.

**Build:** File-loaded versioned definitions; provider-neutral model loop with one real provider and deterministic test adapter; structured finish/tool/delegate actions; input/output schema checks; depth-1 delegation, allowlists, shared turn/tool/delegation/time/token/cost budgets and concurrency; provider usage capture; tool gateway; Supabase Postgres run/event store and migrations; run API and minimal start/inspect UI. Start from the existing [v1 contracts](contracts-v1.md) and [storage spec](storage.md).

**Acceptance:** All scenarios in [first-pass.md](first-pass.md) pass from a clean checkout, including denied delegation/tool, invalid model output, provider failure, deadline, browser inspection, and a completed run readable after API restart. The deterministic model path needs no provider credentials; Supabase connection is required for storage acceptance. The provider path works when configured.

**Explicit limit:** Completed run history survives API restart, but in-flight work does not automatically resume. There is no production identity, write, approval, or organizational-data support.

**Compatibility work:** The current v1 types cover turn/tool/delegation/time limits but not token/cost usage fields. Extend the shared contract and fixtures through the integrator before the provider-backed acceptance check; keep API clients compatible with the existing fixture shape.

## Pass 2 — definition lifecycle and developer experience

**Outcome:** Developers can draft, test, publish, discover, and retire agents as reusable versioned resources.

**Build:** Persistent definition registry; canonical JSON/YAML representation; validation of schemas, model policy, tool/delegate references, and limits; draft/test/publish/retire lifecycle; immutable published versions; import/export; definition and schema versioning; website definition editor and test playground; API documentation and examples. Record publisher and timestamps.

**Acceptance:** Publishing an invalid definition fails with field-level errors. Editing a published version creates a new version. A run started on version A remains pinned to A after B is published or A is retired. Declarative files and website edits produce the same canonical definition through the API. A user can draft, test, and publish on the website without hand editing files.

**Compatibility work:** Extend v1 definitions with lifecycle metadata and model policy. Add versioned definition APIs and a migration for existing file-loaded definitions. Keep the v1 run API working.

## Pass 3 — durable runs and recovery

**Outcome:** Agent work survives API/worker restarts and remains inspectable without duplicating logical completion.

**Build:** Extend existing Supabase run history with durable attempt and checkpoint records; add a durable execution adapter, worker leases or equivalent ownership, checkpoint/resume, cancellation propagation, bounded retries for safe reads, explicit uncertain-operation state, run creation idempotency, replay, and retention basics. Validate the chosen backend with a restart proof of concept before committing to it.

**Acceptance:** Stop a worker during a two-specialist run, restart it, and finish from recorded progress. A completed invocation is not completed twice. Concurrent workers cannot claim the same active attempt. A lost response from a read-only tool is retried within policy and recorded as a second attempt. Cancellation is durable and prevents new actions after restart. Ordered events remain consistent.

**Compatibility work:** Extend the Supabase-backed `RunStore`/`EventSink` with attempt and checkpoint records, then add a durable executor. Do not introduce protected writes until Pass 4 policy and reconciliation are ready.

## Pass 4 — identity, policy, approvals, and protected writes

**Outcome:** An organization can safely grant specific capabilities and require a human decision before a sensitive action.

**Build:** User/service identity; tenant-aware authorization; policy checks on run start, definition access, context resolution, delegation, tool invocation, run inspection, and approvals; model/tool allowlists and budget policy; secret references and just-in-time resolution; write-tool classification; durable approval requests/decisions with expiry and escalation; stable idempotency keys and reconciliation for uncertain writes; audit records and redaction. Add an approval inbox and administrator controls needed to configure the policy.

**Acceptance:** A user from tenant A cannot read tenant B's definition, run, events, context, or secret. A model or retrieved document cannot grant a disallowed tool. A protected write waits for an authorized approver; a denied, expired, or unauthorized decision cannot dispatch it. Restart during approval preserves the pending request. A timeout after a write dispatch never causes a blind duplicate; reconciliation or explicit intervention resolves it. Every decision is auditable.

**Compatibility work:** Add identity and policy references to run/definition contracts and a `WAITING` state. Specify authenticated API errors, authorization-filtered events, approval endpoints, and write-attempt states. Treat Pass 1's anonymous local mode as development-only.

## Pass 5 — scoped context, knowledge, and memory

**Outcome:** Agents use useful prior and organizational information without receiving unrestricted data.

**Build:** Context resolver for step, run, agent memory, and organization scopes; context grants and purpose checks; selected forwarding from orchestrator to specialist; retrieval adapters; memory write/read policy; artifact references and large-payload storage; provenance and retention; redaction and deletion paths. Retrieved text is untrusted input and cannot change permissions.

**Acceptance:** A specialist sees only selected run artifacts and its granted memory. A run denied access to an organization collection receives no retrieved content or metadata from it. Revoking a grant blocks new reads while historical runs remain explainable under retention policy. A retrieval result containing instructions to call a disallowed tool is treated as data, and the tool remains denied. Context provenance is visible to an authorized inspector.

**Compatibility work:** Version the `RunContext` and definition grants, add artifact/reference APIs, and preserve older run snapshots that lack memory references.

## Pass 6 — integrations and isolated extensions

**Outcome:** Developers can add governed tools and operate them outside the central coordinator process.

**Build:** Versioned tool registry and lifecycle; MCP client integration; HTTP/internal-service adapters; Go extension SDK for custom tools; separately deployed extension workers; worker registration/protocol; schema validation and typed errors; credential isolation, resource limits, network/filesystem restrictions, timeouts, crash reporting, retries by side-effect class, and tenant boundaries. Add website tool management and test surfaces.

**Acceptance:** Register and version one MCP, one HTTP, and one custom Go tool; invoke each through the same gateway and permission checks. A worker crash or timeout is recorded without crashing the coordinator. A worker cannot read another tenant's credential or reach a forbidden network target in the supported deployment. Updating a tool version does not alter a pinned in-flight run. A write-capable extension follows Pass 4 approval/idempotency rules.

**Compatibility work:** Extend `ToolDefinition` and `ToolInvocation` with endpoint, auth reference, side-effect, attempt, and worker metadata. Agree on a worker protocol and deployment contract before implementing worker code.

## Pass 7 — workflow and full application integration

**Outcome:** Agents are reusable operations inside NMC3 workflows, and users can manage and inspect them through the application.

**Build in `agents/`:** A workflow-node invocation contract for pinned agent version, schema-valid input/output, cancellation, events, policy identity, and artifact references. Keep agent depth and tool policy enforcement within the agent service. Add API/event support needed by workflow and UI clients.

**Build in adjacent workstreams:** The workflow engine owns DAG validation, readiness, conditions, parallel nodes, join policies, workflow versioning, workflow-wide durability, and approval placement. The application owns visual workflow editing, run visualization, agent/tool management, approval inbox, and administrative surfaces. Integrate via published contracts and mocks before the entire workflow engine is ready.

**Acceptance:** A published workflow runs two independent agent nodes in parallel, joins their validated outputs, pauses for an approval before a protected action, and shows one correlated history across workflow and agent runs. An agent cannot modify the published graph or bypass the workflow's approval. Cancelling the workflow propagates to active agent invocations. Business users can create and run the supported workflow through the application without Go code.

**Compatibility work:** Establish workflow-to-agent IDs, version pinning, correlation IDs, state mapping, cancellation semantics, and event ownership. Do not duplicate DAG scheduling inside the agent coordinator.

## Pass 8 — self-hosted operations and release hardening

**Outcome:** The agent service can be deployed and operated reliably on the intended private infrastructure with measurable service targets.

**Build:** Deployment packaging and environment configuration; database/object-storage backup and restore; secret rotation; migrations/rollback; horizontal worker scaling; rate and concurrency controls by organization; OpenTelemetry traces, metrics, structured logs, token and cost accounting; operator search/filtering; audit/retention jobs; load, failure, security, and isolation tests; runbooks and incident procedures. Establish quantitative latency, throughput, recovery, availability, and cost targets from measured workloads.

**Acceptance:** A fresh environment installs from documented steps, restores from backup, and rolls back a failed deployment without losing committed runs. Load tests enforce organizational limits under parallel runs. Chaos/restart tests confirm recovery and no duplicate protected writes. Logs/traces link to run IDs while hiding secrets and unauthorized content. Operators can locate failed runs, explain cause, and follow a documented recovery path.

**Compatibility work:** Freeze supported API/schema versions, publish migration policy, and test upgrades from previous passes. Security and operations review gates apply before using real enterprise data.

## Coverage map

| Product capability | First complete pass |
| --- | --- |
| Model-backed agents, depth-1 orchestration, read-only tools, limits, run inspection | 1 |
| Draft/test/publish/retire, immutable versions, declarative definitions, website editor | 2 |
| Durable state, cancellation, retries, crash recovery, attempt history | 3 |
| Identity, tenant policy, secrets, approvals, protected writes, audit authorization | 4 |
| Step/run/agent/organization context, retrieval, memory, provenance | 5 |
| MCP/HTTP/internal tools, Go extension SDK, isolated workers | 6 |
| Workflow node integration, DAG/join behavior in workflow workstream, full application flows | 7 |
| Self-hosted deployment, scaling, observability, service targets, operational recovery | 8 |

## Cross-pass rules

- Preserve the depth-1 delegation rule unless the product specification changes explicitly.
- Carry the same principal, tenant, run, invocation, and trace identities across service boundaries.
- Keep action authorization in the runtime/gateway, independent of model text and UI controls.
- Pin versions at run start and retain enough history to explain old results.
- When adding a side effect, specify its retry, idempotency, reconciliation, approval, and audit behavior before dispatching it.
- Each pass adds tests for failure and denied paths, not only the successful path.

## Decision gates

| Before pass | Decision to record | Evidence needed |
| --- | --- | --- |
| 1 | First provider/model, API path from Next.js to Go, supported schema subset, usage-limit fields | One provider run and fixture compatibility test |
| 2 | Definition storage, version compatibility rules, canonical JSON/YAML form | Publish/retire and old-run pinning tests |
| 3 | Durable executor and persistence model | Worker-kill/restart and duplicate-prevention proof |
| 4 | Identity provider, tenant boundary, policy language, approval expiry/escalation, secret system | Denial, cross-tenant, approval-restart, and ambiguous-write tests |
| 5 | Memory retention, deletion, retrieval authorization, artifact storage | Scoped retrieval and revocation tests |
| 6 | Worker protocol, isolation mechanism, MCP/HTTP credential handling | Crash, network, resource, and credential-isolation tests |
| 7 | Workflow-to-agent contract and event/cancellation ownership | Cross-service parallel/join/approval run |
| 8 | Service-level targets, supported deployment topology, upgrade/rollback policy | Load, restore, upgrade, and incident exercises |
