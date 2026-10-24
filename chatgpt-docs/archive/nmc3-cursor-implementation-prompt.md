> **HISTORICAL / SUPERSEDED PLAN:** Read [current decisions](../decisions.md) and [current handoff](../prompts/01-current-implementation-handoff.md). The dashboard freeze and OpenCode/scripted-adapter direction override conflicting instructions below. Findings remain pinned to their stated snapshot.

# Cursor implementation prompt: NMC3 internal platform, Go backend, deterministic agents

Implement this revised direction in `khangdoan514/hacktx-2026`, continuing on `cursor/dev-0d85`. This is the current product and implementation brief. It supersedes earlier instructions that required FastAPI, Convex, external partner access, generic company/workspace onboarding, or no dummy agents.

Do the work in ordered, reviewable phases. Build functioning interactions and persisted backend behavior; creating routes that only print JSON does not satisfy this brief. Preserve the current visual language. This task concerns product behavior, information architecture, backend implementation, and security—not a landing-page or visual redesign.

## 1. Context you must carry forward

The product is called **NMC3**. It is an internal tool for **NMC2**, which offers GPU as a service. NMC3 is the hackathon project's name; do not invent a second customer company called NMC3, or present this as a generic SaaS platform selling workspaces to multiple companies.

NMC2 has two main divisions: **Hardware** and **Software**. The **AI team is within Software**. Teams must coordinate across those divisions. People can perform multiple functions: for example, a product manager may also perform business analysis. A job title is not, by itself, an authorization grant.

The core scenario is:

> A team needs an agent for a particular use case. It submits a structured request to Software's AI team. The teams clarify requirements, tools, context, resource needs, evaluation criteria, budget, and guardrails. The responsible people review the exact proposed configuration. The capability is tested, approved for a defined scope, handed over, and managed over time.

The whiteboard's “PR agent” means a **pull-request/code-review agent**, not public relations. A Hardware firmware team requesting code-review assistance from Software/AI is a useful cross-division example; that particular team name is illustrative, not a claim about NMC2's real organization chart.

GPUaaS is business context and a reason to coordinate Hardware and Software. This release is not a customer GPU marketplace, cloud billing system, or GPU provisioning engine. Customers and partners may be referenced in internal records, but they do not get accounts or portal access in this scope.

The whiteboards explicitly emphasize:

| Area | Required product meaning |
| --- | --- |
| Use case / templates | Describe the job and reuse an approved starting specification |
| Connectors / tools | Record required MCP, CLI, knowledge-base, web-search, and internal-tool capabilities |
| Context / suggestions | Select relevant permitted sources; proposed context needs review |
| Hardware resources | Record resource requirements and limits; obtain Hardware review when needed |
| Evaluations | Define model comparisons, tool-call accuracy, success rate, and human intervention rate |
| Trigger / interfaces | Specify how work starts and how inputs/results are exchanged |
| Model | Record an allowed model policy and intended model choice |
| Budget | Define cost limits and the accountable budget owner |
| Guardrails | Define and enforce permitted and prohibited actions |
| Prompt | Version instructions as part of the agent configuration |
| Observability | Inspect state, errors, decisions, evidence, tool activity, and usage |

Keep management objectives, business-analysis context, engineering delivery, structured handoffs, and dashboard assistance where they support this lifecycle. Stop expanding unrelated project-management and profitability-analysis features as the primary product.

## 2. Source baseline and instructions to reconcile

At review time on 24 October 2026:

- `main` was `df5575f13f86b790c872406a24fe77174832ea58`.
- `cursor/dev-0d85` was `12beedc14d00053947d8bb9f1c6501c76c3e2f02`, published in PR #3.
- The branch already contained that main commit. Recheck the actual refs before editing; merge newer main changes if needed without resetting existing work.
- The agent Markdown documents on the reviewed branch and main had identical Git blob hashes.

Read the current `agents/README.md`, all `agents/docs/` documents, `agents/internal/contracts/`, and `agents/fixtures/api/v1/`. In particular: `product-spec.md`, `target-architecture.md`, `first-pass.md`, `contracts-v1.md`, `storage.md`, `pass-specs.md`, and `testing.md`.

Main supplies shared Go types, interfaces, JSON fixtures, and contract tests. It does not supply a completed runtime. Its first example is vendor comparison, with a `vendor-orchestrator`, `catalog-specialist`, and `analysis-specialist`. Its `ModelAdapter.Next` interface supports `FINISH`, `TOOL`, and `DELEGATE` actions. The first pass already anticipates a deterministic adapter, depth-0 orchestration, depth-1 specialists, read-only tools, and persisted run/event history.

Apply these explicit changes to that roadmap:

1. The entire custom application backend will be Go, including the business API and workers.
2. Implement deterministic dummy agents now using the documented contracts. A live model provider is not required for this delivery.
3. The main user journey is NMC2's internal capability request and handover process. Vendor comparison remains a contract fixture/example, not the primary website experience.
4. Authentication and current resource authorization are required for the integrated application now. Do not expose the documentation's anonymous first-pass API to the internal application.
5. Full durable execution, arbitrary workflow graphs, unrestricted MCP/CLI execution, and autonomous external writes remain deferred.
6. Update the roadmap and ownership documents to explain these deliberate changes. Do not claim the original provider-backed first-pass completion gate has been met by dummy agents.

Existing frontend ownership notes may no longer match the work. Preserve concurrent work, identify shared files, and update the ownership map to the new boundaries. Do not silently replace someone else's newer agent implementation with a dummy.

## 3. Target architecture: all custom backend code in Go

Keep Next.js/React/TypeScript for the frontend. Use **Supabase Auth**, **Supabase Postgres**, and **private Supabase Storage**. SQL migrations and policies remain SQL. Managed Supabase services are not something to rewrite in Go.

Use a Go modular application with an API binary and a worker binary. One repository/module is sufficient; do not invent a microservice fleet. Domain packages should remain separable so the agent runtime can be extracted later without changing product contracts.

Suggested final layout:

```text
go.mod / go.sum
cmd/api/                    Go HTTP server
cmd/worker/                 outbox and bounded execution worker
internal/platform/          configuration, database, identity, errors, logging
internal/organization/      NMC2 organization, divisions, teams, access
internal/requests/          capability requests and revisions
internal/approvals/         business decisions and policy checks
internal/capabilities/      catalog, specifications, releases, team assignments
internal/context/           context packages, source references, grants
internal/resources/         resource requests, budgets, allocation records
internal/evaluations/       suites, results, release gates
internal/documents/         private Supabase Storage lifecycle
internal/dashboards/        versioned dashboard proposals and publication
internal/notifications/     inbox, outbox, subscription authorization
internal/audit/             attributable event history
internal/agents/contracts/ canonical agent types and API shapes
internal/agents/runtime/   coordinator, gateway, policy, persistence
internal/agents/dummy/     explicitly named deterministic adapters
agents/docs/               updated agent design and ownership documents
agents/fixtures/           versioned compatibility fixtures
supabase/migrations/       one ordered migration history
frontend/                  existing Next.js application
```

Use the repository root module `github.com/khangdoan514/hacktx-2026`. The current nested module is `github.com/khangdoan514/hacktx-2026/agents`, and `agents/internal/contracts` cannot simply be imported from a separate sibling backend. Move the canonical contracts once into the root module, update imports, fixture paths, tests, and documentation atomically, then remove the obsolete nested module. Do not copy the contracts into a second independent package. Reconcile any upstream module changes before performing this move.

Use ordinary Go HTTP handlers/middleware and a maintained PostgreSQL driver with explicit transactions. Use a reviewed JWT/JWKS library rather than implementing cryptography. Pin dependencies in `go.mod`/`go.sum`. Keep transport DTOs separate from persistence and policy code. Document API schemas so the TypeScript client is generated or checked against the Go server.

Retain the business `/api/v1` HTTP shapes where they still represent useful internal operations. Preserve the documented `/v1/agent-runs`, run detail/events, and `/v1/agents` contracts through explicit routing. Configure a specific Next.js rewrite for the agent API; do not assume the business `/api` rewrite maps to `/v1` automatically. Authentication and authorization must apply to canonical routes and any aliases equally.

Next.js can render pages and proxy requests. Business mutations, authorization decisions, file-signing logic, run orchestration, workers, and event delivery belong in Go. Do not reintroduce them as Next.js server actions, Python helpers, Convex functions, or Supabase Edge Functions.

Go owns live updates using authorized SSE with polling fallback. Persist authoritative data in Postgres. Remove Convex rather than leaving a second TypeScript backend. Do not add Supabase Realtime as a parallel delivery mechanism during this migration.

## 4. Explicit removal, retention, and replacement list

### Remove from the final application

| Existing direction or implementation | Required replacement |
| --- | --- |
| FastAPI, Uvicorn, Pydantic business API, Python worker | Go API, validated Go DTOs, Go worker |
| Python backend dependency/build/startup path | Go build/test/run path; retain unrelated ML experiments if present |
| Convex directory, backend functions, auth config, deployment settings, dependencies, hint secret/URLs | Go outbox/inbox and SSE/polling |
| Public create-account and “create your company” flows | Authorized NMC2 employee onboarding |
| User-created workspaces and workspace picker | One provisioned NMC2 organization with division/team navigation |
| External partner invitations, guest/partner access roles, partner portal | Internal customer/partner reference fields only where necessary |
| General partner onboarding, partner project sales workflows | Internal capability requests and cross-team delivery |
| Profitability/capacity-margin analysis as the primary journey | Request throughput, capability adoption, evaluation evidence, budgets, and resource needs |
| Hard-coded fake agent arrays, random KPI generators, pretend run timers | Persisted definitions, deterministic backend execution, real event timestamps |
| Detail pages that just render raw JSON or fetch an entire collection | Selected-record views and real domain actions |
| Production fallback JWT secret and anonymous application endpoints | Fail-closed Supabase identity verification |
| Local filesystem as the production document store | Private Supabase Storage |
| Old claims that agents are never implemented in this workstream | Explicit deterministic agent implementation and future provider adapter boundary |

Remove the obsolete external/workspace-creation API operations as well as their buttons. Hiding navigation does not remove access. Unknown or retired routes should have intentional responses and links should be updated. Internal employee invitations must not accept arbitrary external recipients or client-selected privileged roles.

Delete `backend/src/app`, Python-only backend dependencies and startup instructions only after retained behaviors and security regression tests have been ported to Go. Do not keep Python as an undocumented fallback after cutover. Remove Convex env values from examples, configuration, CI, and architecture docs; do not print existing secrets while cleaning configuration.

Do not blindly drop legacy database tables, delete real records, reset databases, or rewrite applied migrations. First inventory data and references. Disable old behavior, map relevant data to the new model, and use additive migrations. Retire obsolete tables in a later reviewed migration when no reader/writer remains. Existing workspace IDs can remain the organization-scope key during migration; single-company UX does not require stripping all scope columns.

### Retain and adapt

- Postgres transaction boundaries, row-level policies, version checks, audit/outbox concepts, and useful test scenarios—with the defects below fixed.
- Projects/tasks as delivery containers subordinate to a capability request, not a competing top-level product.
- Requirements, revision history, comments, documents, approvals, and structured handoffs.
- Dashboard definition/version/provenance concepts for operational reporting and future dashboard assistance.
- Agent action/state contracts, fixtures, depth rules, and read-only gateway design from main.
- Existing frontend components and design system wherever they fit. No new landing page, brand exploration, or gratuitous redesign.

## 5. Fix the reviewed security defects during the Go migration

The reviewed Python branch's 16 backend tests passed on an isolated PostgreSQL database. Additional API checks reproduced these failures; port them as negative regression tests rather than reproducing the old behavior:

1. **Cross-workspace approval mutation.** A reviewer unable to read a requirement in workspace B could approve an approval created in project A that referenced B's requirement; the target changed to `accepted`. `create_approval` did not bind target ownership, and the privileged SQL function updated by ID/revision only. Bind target organization, project, kind, version, and review state at creation and at decision. Enforce it inside the transaction and any privileged SQL function.
2. **Membership revocation failure.** Removing workspace membership returned 200, but the same person could still read their project with 200. Active project membership alone is insufficient. Every access path must require current employee/organization status plus the relevant project/team/resource grant.
3. **Unsafe production token configuration.** With `ALLOW_LOCAL_AUTH=false`, production verification still accepted a token signed with the shipped `dev-only-change-me` fallback for an active account. Fail startup on missing/unsafe production Auth configuration. Keep the development issuer separate and reject it in production.

Also finish the currently incomplete Supabase integration: proper signing-key verification, employee/profile provisioning, refreshed-session synchronization, sign-out, private object operations, and their authorization tests. Do not treat the presence of a Supabase dependency or bucket configuration as completed integration.

Approval authority must be server-derived. Being an internal viewer, a request author, or a person assigned to a task must not automatically make someone a security reviewer or release approver. A task's generic “complete” endpoint must not bypass a required decision. Test the alternative mutation paths too.

## 6. Organization and authorization model

Provision one NMC2 organization. Seed Hardware and Software divisions and the Software/AI team. Other team names should be configurable; do not invent an official NMC2 org chart. Employees may belong to multiple teams and hold multiple functional roles.

Separate these concepts:

- **Organization placement:** division, team, membership, reporting/support ownership.
- **Business functions:** PM, BA, engineer, AI engineer, Hardware resource owner, manager.
- **Permissions:** request creation, team triage, specification editing, budget approval, security review, release publication, execution, audit access.
- **Resource scope:** request, project, capability, repository/context package, budget, artifact, run.

Use current database-backed grants. Defaults should permit useful collaboration without making all internal information globally readable. A catalog entry may be discoverable while its prompt, private repository context, credentials, evaluation examples, and run outputs remain restricted.

Default roles:

| Role | Scope and actions |
| --- | --- |
| Requester / PM / BA | Draft and track own/team requests, clarify needs, review outcomes |
| Team lead | Prioritize own team's work, authorize demand within delegated limits |
| AI intake owner | Triage requests assigned to AI, request information, assign engineering owner |
| AI engineer | Create specifications and test versions within assigned scope |
| Hardware resource owner | Review resource feasibility and record authorized allocation evidence |
| Security/data reviewer | Decide permitted tool/data boundaries within assigned responsibility |
| Budget owner | Approve a versioned budget envelope within delegated authority |
| Release approver | Publish an eligible capability version after required gates |
| Operator | Start permitted runs, inspect permitted results, request cancellation |
| Administrator | Employee/team/access administration; explicitly scoped emergency powers |
| Auditor | Read appropriate history without mutation rights or automatic secret access |

Keep separation of duties for consequential approvals. A person may have multiple business functions, but cannot self-approve a restricted submission merely by switching a UI role. Seed a small documented permissions matrix and test it.

## 7. Primary request-to-handover workflow

Implement this journey end to end:

1. A requester chooses a use-case template or starts a blank capability request.
2. They supply a desired outcome, requesting team, accountable business owner, target users, urgency/due date, acceptance criteria, tools, context, resource needs, model preferences, budget proposal, trigger, and prohibited actions. Unknown fields can be explicitly marked unresolved in a draft.
3. The system offers compatible existing capabilities for reuse using category, ownership, approved scope, and requirements. Start with deterministic filtering; do not imply semantic AI matching.
4. The requester submits an immutable request revision to Software/AI's intake queue. Record requester and receiving team separately.
5. Intake accepts, rejects with a reason, asks targeted questions, or proposes reuse. Someone must own the next action. Notify only authorized participants.
6. The AI engineer prepares a versioned specification and links needed Hardware, data, security, and budget reviews. A Hardware review is required when actual hardware resources are requested, not for every hosted-model use case.
7. Reviewers approve or request changes against that exact specification revision. A material change invalidates affected approvals and test evidence.
8. The deterministic runtime executes an eligible test version against permitted fixtures through the actual coordinator/gateway. Persist results, events, and evaluation evidence.
9. Required reviewers accept the evidence and release scope. Publication creates an immutable catalog version. It does not itself provision a GPU or grant a GitHub write credential.
10. The requesting team receives a handover containing the version, allowed uses, limits, approved context, operating instructions, known limitations, support owner, and revocation/escalation route.
11. The recipient acknowledges the handover. Operation, change requests, suspension, and retirement remain linked to the original request.

Keep separate state machines rather than one status that mixes delivery, execution, and approval:

- Request: `draft → submitted → triage → needs_information / accepted / rejected`; accepted requests proceed through `in_delivery → ready_for_acceptance → fulfilled`. Explicit cancellation and controlled reopening require a reason.
- Specification: `draft → in_review → changes_requested / approved → superseded`.
- Catalog version: `draft → testing → eligible → published → suspended / retired`.
- Handover: `draft → sent → acknowledged → completed`, with return-for-information and cancellation.
- Approval: `pending → approved / rejected / changes_requested / expired / superseded / cancelled`.
- Agent run: retain documented `PENDING → RUNNING → SUCCEEDED / FAILED / CANCELLED` for this bounded implementation.

Server-side transition functions own the rules. Revision checks, state change, decision record, audit event, and outbox event must commit together. Concurrent decisions must yield one effective outcome. Retries must return the original result for an identical idempotency key/body and reject a mismatched body.

## 8. Specification fields and changes that invalidate approval

An immutable specification revision needs:

| Group | Fields |
| --- | --- |
| Identity | capability/request IDs, version, requesting and owning teams, accountable owners, category |
| Purpose | user problem, expected output, non-goals, acceptance criteria |
| Interface | input/output schemas, trigger, manual interface and intended future integrations |
| Instructions | versioned prompt/instructions, template and template version |
| Model policy | requested provider/model, approved allowlist, data handling restriction; current execution adapter recorded separately |
| Tools | tool/version references, side-effect class, resource scope, credential reference only, timeout |
| Context | authorized immutable source references, classification, purpose, retention and expiry |
| Resources | hosted API or self-hosted execution intent; optional GPU type/count/VRAM, concurrency and duration requirements |
| Budget | currency, period, per-run and aggregate proposed limits, budget owner, approved amount |
| Guardrails | allowed/prohibited actions, network targets, write approvals, max turns/calls/delegations, timeout |
| Evaluations | suite/version, expected outcomes, thresholds, reviewers and result references |
| Operations | support owner, lifecycle state, rollback target, incident/escalation path |

Changes to tools, context scope, model/data policy, instructions, output action types, budget, hardware requirements, or evaluation suite create a new revision and invalidate the relevant prior decision. Do not copy an “approved” flag into a materially different configuration. Low-risk metadata edits can have a documented narrower rule.

## 9. Dummy agents: implement the documented interfaces properly

The user now explicitly wants dummy agents. Implement deterministic adapters in Go and clearly identify them **in code, internal metadata, developer logs, and developer documentation**. Do not add “dummy,” “mock,” or “demo mode” badges, banners, or prefixes to the website.

Use a package such as `internal/agents/dummy`, types such as `DummyModelAdapter`, and an explicit setting such as `AGENT_EXECUTION_MODE=dummy`. Every persisted run must record its server-assigned execution mode and adapter version. Do not let clients forge that provenance. Future provider mode must require explicit configuration; never silently fall back to dummy results when a real provider fails.

The website uses ordinary names such as “PR Review Assistant,” “Run history,” and “Completed.” A completed run means the actual deterministic execution finished. Do not display fabricated provider token counts, invented cost savings, successful external writes, or GPU allocations. Display actual observed duration/status; use unavailable/empty values when no measurement exists. Do not represent an intended model choice as the provider that executed a dummy run.

Keep real application state real: authentication, database writes, requests, approvals, grants, file access, event ordering, and handovers are not mocked. Only model reasoning and not-yet-connected external tool sources are deterministic implementations behind contracts.

### Required runtime mechanics

- Use the canonical `ModelAdapter.Next`, `ModelAction`, `Coordinator`, `ToolGateway`, `RunStore`, and `EventSink` contracts from main.
- Validate exactly one `FINISH`, `TOOL`, or `DELEGATE` payload before dispatch.
- The root at depth 0 may invoke allowlisted specialists at depth 1. A specialist cannot delegate. Derive depth server-side from parentage.
- Execute two independent specialists concurrently within the shared limit, then combine validated outputs.
- All tools go through the same gateway and schema/scope checks, including dummy tools. No adapter may call a tool or another agent directly.
- Use only read-only tools in this delivery. Do not grant arbitrary network URLs, shell commands, GitHub writes, or GPU control.
- Enforce positive effective limits, shared atomic counters, timeouts, cancellation, and output schema validation. Do not implement fake limits that only change a label.
- Persist real runs, invocation trees, tool calls, bounded attempts, safe errors, and ordered events in Supabase Postgres. Commit initial run/event together.
- Use a bounded worker queue with database ownership/leases; do not launch unlimited goroutines from HTTP handlers. A restart must preserve completed history and put abandoned in-flight work in a truthful terminal error state. Automatic mid-run recovery is deferred.
- Add deterministic failure fixtures for invalid output, disallowed tool, forbidden delegation, timeout, and exhausted budget. Select scenarios explicitly in tests; do not fail randomly.

### Required agent examples

1. **Documentation compatibility example:** retain the vendor orchestrator, catalog specialist, analysis specialist, and read-only catalog fixture from main as a regression example. Preserve the semantics of its existing fixtures.
2. **Primary product example:** add a PR review orchestrator and two depth-1 specialists—for code-quality checks and test/security checklist checks. Use a checked-in fictional diff and authorized coding standards. Return structured findings with file/line references that exist in the supplied input, severity, rationale, checklist coverage, and limitations. Deterministic rules must not invent repository evidence. They are not a complete security scanner.
3. **Dashboard-assistance example:** a deterministic adapter emits a schema-valid dashboard proposal referencing authorized operational data definitions. Save it as a proposal; a human reviews and applies it through the same validated dashboard command as manual edits. It cannot publish itself or inject executable HTML/JavaScript/SQL.

These are reusable configured capabilities using the same runtime, not separate implementations for every job title. Keep request routing/intake as ordinary application logic. Do not pretend the AI team is an autonomous agent that approves its own requests.

The initial trigger is a real manual Run action. Store future pull-request/webhook/schedule trigger requirements, but keep unimplemented trigger activation unavailable. No fake “connected” integration status. Do not ship a public inbound webhook unless signature verification, replay handling, repository authorization, and delivery idempotency are implemented and tested.

## 10. Pages and interactions to complete

Use one NMC3 navigation model organized around work and ownership. Existing route groups may be retained internally, but users should not have to understand a split between a “business app” and an “agent app.” Avoid conflicting Next.js routes. Keep visual changes limited to supporting the workflows below.

| Page | Required interactions and persisted behavior |
| --- | --- |
| Sign-in / access pending | Company access flow, invitation or approved identity, expired-session handling, real sign-out; no public organization creation |
| My work | Requests awaiting me, assigned clarifications, approvals, handovers, failures; permission-filtered links and counts |
| Teams | Hardware/Software hierarchy, Software/AI intake ownership, team members and capability assignments |
| Request list | Filter by requesting/receiving team, state, owner, due date and category; paginate |
| New request | Template/reuse selection, save draft, validation, submission; meaningful unresolved-field handling |
| Request detail | Current revision, history/diff, owners, comments, clarifications, dependencies, decisions and next action |
| AI intake | Claim/assign work, triage, request information, accept/reject with reason, propose reuse, priority ordering |
| Specification | Edit structured fields, choose authorized sources/tools, compare revisions, submit review, invalidate stale approvals |
| Approval inbox/detail | Read exact reviewed version and evidence; approve/reject/request changes with reason; expiry and conflict handling |
| Capability catalog | Discover approved capability metadata, filter category/owner, request access/reuse, see support ownership |
| Capability detail | Published version, allowed scope, limits, linked request, handover, permitted Run action, history |
| Agent version / test | Authorized draft configuration, fixture input selection, actual test execution and result comparison |
| Runs / run detail | Start, inspect invocation tree and ordered events, view evidence, cancel, retry as a new linked run |
| Evaluations | Versioned suite, cases and expected results; pass/fail evidence and human review; release-gate failures |
| Context and documents | Upload/finalize, attach permitted versions, classification, provenance, revoke/remove; controlled download |
| Tools/connectors | Capability metadata, intended vs configured connection state, approved scope, read/write classification; never raw secrets |
| Resource requests | Hardware feasibility review, requested vs approved limits, allocation evidence and owner; no invented live inventory |
| Budgets | Proposed/approved envelope, accountable owner, actual available usage, alerts; missing telemetry is not zero spend |
| Handover | Send exact release/context package, recipient acknowledgment, return questions, completion evidence |
| Operational dashboards | Manual create/edit/revise/publish and review/apply assistance proposals; source access checked at read and publish |
| Notifications | Read/unread, link to authorized object, preferences; no titles/previews from inaccessible resources |
| Audit / administration | Authorized employee/team/grant management, decision history, suspension/retirement, protected audit search |

List pages must support meaningful empty/loading/error states and stable pagination. Detail routes must load the selected record, not the collection. Buttons must call a real command and display permission, validation, stale revision, and unavailable integration outcomes accurately. Persist draft forms where losing work would be material. Do not fabricate completion evidence such as “checked against acceptance criteria” when a person has not supplied it.

## 11. Data model and API responsibilities

Use `app` for organization/business records and `nmc3` for runtime records, retaining the separation in the agent docs. They can share one Supabase project. Establish one migration history and explicit table ownership. Use non-exposed schemas by default; browser requests go through Go.

Core business records:

- organization, divisions, teams, employee profiles, memberships, scoped grants;
- capability requests, immutable request/specification revisions, clarification threads;
- catalog capabilities, immutable released versions, consuming-team assignments;
- business approval requests/decisions, revision-bound evidence, handover records;
- context packages/source references/grants, documents and immutable file versions;
- tool/connector catalog metadata and restricted credential references;
- resource requests/allocation evidence, budget envelopes and usage references;
- evaluation suites/cases/results, release checks;
- dashboards/revisions/proposals and provenance;
- notifications, audit events, outbox events, idempotency records.

Core runtime records:

- canonical agent/tool definitions and immutable versions;
- runs, invocations, model-adapter turns, tool calls, attempts and event sequences;
- input/output artifact references, pinned policy/grant references, usage metadata;
- execution mode/adapter version, correlation IDs, initiating employee/service identity.

A business specification describes requested and approved behavior. It references a canonical executable agent definition/version; do not maintain two editable copies of the same executable definition. A catalog release binds the approved specification to its executable version and evaluation evidence.

Use columns for identity, scope, state, version, ownership, timestamps, and searchable attributes. Use validated JSONB for variable specifications and structured outputs. Add foreign keys and same-scope constraints where possible. Pin versions and hashes in decisions and handovers. Published content and audit events are immutable; edits create new revisions.

Provide explicit commands for submission, triage, review, publishing, assignment, acknowledgment, run start/cancel/retry, context grants, and suspension. Avoid a generic patch endpoint that can set any lifecycle state or actor field. Derive organization, actor, effective permissions, provenance, cost authority, and recipient access on the server.

Use machine-readable errors plus safe messages and request IDs. Validate content type, UUIDs, schema versions, body size, enum values, timestamps, numeric ranges, and pagination. Cross-scope lookup must not reveal whether the protected object exists. Rate-limit sensitive actions. Never log bearer tokens, secrets, or private source bodies by default.

## 12. Security requirements for the integrated product

**Identity and revocation.** Verify Supabase JWT signatures with the project's actual configured signing-key scheme and trusted issuer/audience. For asymmetric keys, use the trusted project's JWKS and bounded cache/rotation behavior. Reject missing required claims, invalid/expired tokens, arbitrary token-supplied key URLs, and development issuers in production. Use a shared frontend Supabase session integration so refreshed tokens reach requests; logout must invoke Supabase sign-out and clear application state. Provision a profile safely from verified identity, but grant NMC2 membership only from an approved employee/invitation source. Email-domain matching alone does not authorize all employees or privilege assignment.

**Current permissions.** Check active employee and organization status plus resource/action scope at reads and writes. Removing a person or grant blocks subsequent project/file/run access and new approvals. Cancel/recheck execution permissions when access changes. Filter SSE, search, exports, notification previews, logs, and catalog detail fields. Management rank is not a universal data-access override.

**Database roles.** Use a non-owner, non-BYPASSRLS runtime role. Apply identity with transaction-local settings on a checked-out connection; commit/rollback releases it. Never use a pool-level/global user ID or session setting that leaks to another request. Separate migration/admin credentials from runtime/worker privileges. RLS is defense in depth, not a replacement for command authorization. Audit `SECURITY DEFINER` functions, restrict execute grants, fix search paths, and enforce target/current-actor checks inside them.

**Approval integrity.** Bind decisions to target organization/project, immutable revision, configuration hash, authorized reviewer, expiry and permissible state. Lock or compare-and-swap the target and decision together. Reject self-approval, stale evidence, removed reviewers, replays with a different body, and cross-scope targets. Distinguish approving a capability specification from approving a particular external write. A published agent is not blanket authorization for every future action.

**Tools and prompt injection.** Treat source documents, diffs, tool results, user prompts, and model outputs as untrusted data. They cannot grant tools, change budgets, select arbitrary credentials, remove review gates, or escalate access. Compute effective permissions as the intersection of the initiating identity, current grant, approved agent version, tool scope, and policy. The read-only gateway rejects external write tools in this delivery. Future MCP/CLI support requires endpoint/network/process isolation; recording a connector in the catalog does not execute it.

**Context and artifacts.** Grant access per purpose/resource/version. Do not forward a whole company's corpus or a parent's entire transcript to a specialist. Source references are not proof of permission. Recheck access when resolving context, downloading an artifact, applying a proposal, and displaying a run output. Record which sources were used. Redact sensitive output fields and avoid copying revoked source contents into unrestricted audit/notification records.

**Private Storage.** Implement upload initiation, controlled byte transfer, finalize/checksum/type/size verification, authorized download, deletion/retention, and cleanup of abandoned uploads. Keep buckets private. If Go uses privileged Storage credentials, it must enforce resource authorization before every storage operation; RLS will not rescue a bypassing key. Prefer short-lived scoped links where appropriate and document their revocation window. Limit streamed bytes before buffering and validate real content where needed. Do not assume a Postgres backup contains object bytes.

**Secrets and network.** Store credential references, not plaintext credentials, in specs and events. Keep Supabase secret/service credentials and future provider tokens server-only. Disable secret-bearing debug output. Restrict outbound endpoints and block arbitrary URL fetching/SSRF. Use supported secret-management mechanisms when integrations are enabled; do not build a secret manager as part of the hackathon.

**Budgets and execution.** Enforce concurrency, turn, tool, delegation and deadline limits in the coordinator. Proposed budgets and measured usage are different data. Dummy mode has no provider token measurement; do not fabricate it. Test budget exhaustion with explicit synthetic counters in tests. Before real paid providers, implement atomic reservation/accounting across concurrent runs and failure reconciliation.

**Audit and recovery.** Record actor, action, target, before/after revision, decision reason, timestamp, request/run correlation, and execution mode. Keep history append-only for ordinary runtime roles. Run notification delivery in a Go worker with retry/backoff, idempotency, current-recipient checks and dead-letter visibility; a notification outage must not undo a committed business decision. Document database and object restoration, retention, and cleanup. Do not claim certification or production readiness without verification.

The main agent docs postpone some governance to later passes. For this integrated product, identity, scoped reads, safe read-only execution, and decision integrity move earlier. Dummy providers do not justify anonymous APIs or unrestricted organization data.

## 13. Evaluations and operational reporting

Implement versioned evaluation cases with expected outputs, permitted tools, prohibited actions, input/source fixtures, and required evidence. Run them through the actual deterministic coordinator. Persist per-case outcomes and which definition, policy, adapter, and suite versions were used.

Define metric denominators explicitly:

- Run success rate = successful eligible completed runs / eligible completed runs, with cancellations reported separately.
- Tool-call accuracy = judged-correct calls / judged calls; report unjudged cases rather than counting them as correct.
- Intervention rate = eligible runs requiring human intervention / eligible runs, with the intervention definition recorded.
- Model comparisons require comparable cases and actual provider-backed measurements later; dummy adapters are not model-quality evidence.
- Resource utilization requires measured capacity/usage from an integrated source; request counts are not GPU utilization.

Useful internal dashboards: intake age and blockers, request-to-handover duration, requests by division/category, reusable capability adoption, failed runs by reason, approval waiting time, evaluation regressions, proposed/approved budgets, and resource requests awaiting Hardware. Keep private source details out of high-level company summaries.

An evaluation pass certifies only that specific version and suite. It does not mean the agent is secure for every repository or tool. Require security-denial cases in release tests and preserve failure evidence.

## 14. Migration and delivery sequence

### Phase 0 — inventory and lock the direction

Refresh main/branch refs; preserve uncommitted work. Map existing endpoints, screens, tables, tests and agent contracts into retain/replace/remove. Record the decisions above in an architecture decision document. Identify shared frontend paths. Add failing regression tests for the three reproduced security issues. No live database reset or hosted project creation is required to start.

### Phase 1 — Go foundation and secure identity

Consolidate Go module/contracts, implement API/worker entrypoints, configuration, error handling, database transactions, trusted Supabase identity, organization membership, private-storage boundary, audit/outbox infrastructure, and health/readiness. Port retained backend tests to Go. Establish API compatibility for retained frontend calls. Use a separate test database with fail-safe configuration; never run destructive tests against a configured application database.

Gate: valid identity works, invalid/default-secret identity fails, current membership controls access, cross-scope approval tests fail safely, transaction identity does not leak across pooled connections.

### Phase 2 — NMC2 request workflow

Provision the internal organization/divisions/team. Replace workspace onboarding and partner access. Implement requester and AI intake journeys, immutable specifications, clarification, scoped approvals, delivery tasks and handover. Complete the actual forms and decision interactions.

Gate: two authorized employees in different teams can submit, clarify, review and acknowledge one request; unauthorized users and stale revisions are rejected. No generic JSON page stands in for these operations.

### Phase 3 — deterministic agent runtime

Implement the documented coordinator/gateway/adapters, bounded asynchronous execution, Supabase persistence, event inspection and cancellation. Preserve vendor fixture compatibility and add the PR review workflow. Bind runs to identity, request, catalog version and allowed context. Complete browser start/inspect flow.

Gate: successful, denied, invalid-output, timeout, budget and cancellation cases all produce real persisted outcomes; specialists cannot delegate; completed history survives restart; interrupted work does not silently become successful.

### Phase 4 — catalog, evaluations, resources and dashboards

Implement versioned publication, team assignment, evidence gates, resource/budget reviews and operational reporting. Add the dashboard-proposal dummy adapter and human application path. Release metadata must distinguish intended integrations/resources from recorded operational facts without developer-mode banners.

Gate: an approval or evaluation cannot be reused after a material spec change; proposal application rechecks data access; publishing does not silently grant write permissions or allocate hardware.

### Phase 5 — cutover, cleanup and verification

Point the frontend only to Go. Delete the obsolete Python backend and Convex backend/config/dependencies. Remove unused routes/navigation, fake-data shims once their imports are replaced, and stale README commands. Update docs, environment examples, CI and PR description around the final scope. Validate clean setup, migrations, browser journeys, and recovery. Keep future features as documented backlog, not working-looking controls.

Do not stop at scaffolded packages or “API available” claims. Carry each phase through its acceptance checks. Report any unavailable external environment accurately rather than substituting an unreported mock.

## 15. Required verification matrix

| Area | Required proof |
| --- | --- |
| Go build | All Go packages compile; format/vet checks and meaningful unit/integration tests pass; race checks for shared counters and worker ownership |
| Authentication | Real Supabase access token, expiry, refresh, sign-out, key rotation/unknown key, missing claims, production rejection of dummy issuer |
| Employee access | Unauthorized signup cannot create an NMC2 membership; revoked member cannot read old project/run/file or decide an old approval |
| Scope | Cross-team/project/organization-object ID substitution cannot read or mutate unauthorized records; retained scope columns remain enforced |
| Approval | Foreign target, self-approval, wrong reviewer, old revision, expired/superseded decision, concurrent decisions, and bypass completion route rejected |
| RLS | Runtime role cannot bypass/own tables; pooled identity isolation; privileged functions cannot escape the target scope |
| Agent contracts | Existing fixtures remain valid or deliberately versioned; unsupported schema keywords rejected; exactly one action payload |
| Runtime | Shared limits under concurrent specialists, no depth-1 delegation, tool-denial before invocation, cancellation/deadline, restart behavior |
| Persistence | Run/event atomic creation, monotonic unique event sequence, immutable published versions, idempotent commands |
| Dummy boundary | No live model call in dummy mode, no external write, no random fabricated measurements; server-side execution provenance saved |
| Storage | Unauthorized upload/download rejected, oversized/type-mismatched upload rejected, finalize checks bytes, abandoned upload cleanup |
| Dashboard | Invalid/executable definition rejected, unauthorized source rejected, stale base revision rejected, provenance cannot be client-forged |
| Notifications | Outbox retry without duplicates, no unauthorized preview or SSE event, live subscriber loses access after revocation |
| Browser | Request → intake → clarification → approval → test run → release → handover works through forms with multiple actual test identities |
| Cleanup | No runtime Python/FastAPI/Convex dependency remains; no public workspace/partner access; documentation matches startup behavior |

Use fictional fixtures in a development/test Supabase environment. A plain PostgreSQL test run is useful for SQL and application logic but is not proof of Supabase Auth, refresh, or Storage. Run the latter against a functioning supported Supabase environment when available and report any unverified integration explicitly. Do not invent test results or silently skip them.

## 16. Demonstration and final handoff

The principal demonstration should use persisted records and multiple employee identities:

1. A Hardware requester submits a PR-review capability request to Software/AI.
2. AI intake asks a targeted repository/context question; the requester responds.
3. An engineer creates the exact versioned tool/context/limit specification.
4. Authorized reviewers approve it; a stale or unauthorized approval attempt is visibly rejected through normal error handling.
5. A PR-review run executes against the authorized fictional diff using the documented deterministic orchestrator and two specialists; the invocation tree and actual events are inspectable.
6. Evaluation evidence gates publication; the requesting team acknowledges the versioned handover.
7. A later instruction/context change produces a new version requiring the appropriate renewed review.
8. Removing access blocks further reads and runs. Restarting the API preserves completed history.

The website should have normal NMC3 product wording without dummy-agent badges. Code and developer documentation must make the deterministic implementation unmistakable. Do not claim a real GitHub review was posted, a model reasoned about live code, or hardware was allocated when only local deterministic processing occurred.

At completion, provide the final architecture, removal inventory, migration notes, environment/setup instructions, test evidence, remaining integration limits, and precise deferred work. Update the PR title/body to describe the final Go/internal-platform/deterministic-agent implementation. Do not deploy to production or merge the PR as an incidental part of implementation.

## Reference links

- [Reviewed main agent docs](https://github.com/khangdoan514/hacktx-2026/tree/df5575f13f86b790c872406a24fe77174832ea58/agents/docs): product scope, runtime boundaries, first-pass contracts and security requirements.
- [Reviewed first-pass specification](https://github.com/khangdoan514/hacktx-2026/blob/df5575f13f86b790c872406a24fe77174832ea58/agents/docs/first-pass.md): deterministic adapter, read-only tool gateway, bounded delegation and persistence.
- [Reviewed Cursor PR #3](https://github.com/khangdoan514/hacktx-2026/pull/3): implementation to migrate, not a completed secure production platform.
- [Supabase JWT documentation](https://supabase.com/docs/guides/auth/jwts): verify the project's actual signing scheme and claims.
- [Supabase Storage access control](https://supabase.com/docs/guides/storage/security/access-control): private-object authorization and policy requirements.
- [OWASP agent security guidance](https://cheatsheetseries.owasp.org/cheatsheets/AI_Agent_Security_Cheat_Sheet.html): additional review reference for tool permissions, untrusted context, human decisions, and adversarial tests.

This prompt's NMC2-specific workflow and architecture choices are product decisions from the user and this replan. They are not assertions that those features already exist in main.
