> **HISTORICAL / SUPERSEDED PLAN:** Read [current decisions](../decisions.md) and [current handoff](../prompts/01-current-implementation-handoff.md). The dashboard freeze and OpenCode/scripted-adapter direction override conflicting instructions below. Findings remain pinned to their stated snapshot.

# NMC3: repository audit and implementation plan

**Superseded implementation direction:** Use [the current Cursor prompt](nmc3-cursor-implementation-prompt.md) for NMC2-only workflows, the all-Go backend, and explicitly implemented deterministic agents. This older document remains historical audit context; its FastAPI, external-partner, generic workspace, and no-dummy-agent directions no longer apply.

Prepared October 24, 2026. Repository: [khangdoan514/hacktx-2026](https://github.com/khangdoan514/hacktx-2026). Audited snapshot: [`f0f0b1fd6467914e5b2424c49093f02283183e8b`](https://github.com/khangdoan514/hacktx-2026/tree/f0f0b1fd6467914e5b2424c49093f02283183e8b).

**Revision 2 — Supabase and future agent integration.** Supabase is the selected database/authentication/storage platform. The application must expose clean contracts for a separate future agent system, including dashboard creation assistance. This revision covers the application side of that boundary. It does not add agent implementation, simulated assistants, generated activity, or fake results. The original repository findings remain tied to the audited commit; this revision changes the implementation plan, not the audit snapshot.

## 1. Finding and scope

**The main missing piece is the persistent, permission-aware business application.** The repository provides a Next.js shell, a FastAPI starter, development scripts, and prototype documentation. It does not yet implement the company/project/partner collaboration system described in the supplied notes.

This plan covers authenticated product pages, human interactions, business rules, Supabase persistence/auth/storage, APIs, access control, data analysis, testing, operations, and application contracts for later agent integration. Agent models, prompts, tools, memory, planning, orchestration and execution belong to a separate system and remain outside this work. The front page and visual design remain excluded. Page names describe functional destinations; they do not prescribe layouts, styling, or a separate screen for every operation. Create/edit operations can be embedded in their parent destination.

**Scope rule:** implement useful domain features now and design their integration contracts now. Connect assistance only when a real external implementation exists. Do not create placeholder agents, fake conversations, simulated progress, fabricated dashboard results, or agent-branded seed activity. Manual dashboard creation/editing, dataset selection, calculations, review and publication must work independently.

**Requirement precedence:** the supplied project notes define the target. The repository's `FRONTEND.md` describes a different, narrower consultant prototype and explicitly labels its data as simulated. It is evidence of prototype status, not the specification for this plan. In particular, budget boundaries and margin analysis belong in this plan because the supplied notes request them, despite different content constraints in that older prototype document.

**Evidence limits:** this is a source audit of the tracked default branch, not a live deployment test. The GitHub branch listing returned only `main`, and the open-PR listing returned no open PRs at inspection. Local uncommitted work and separately hosted services are unknown. The complete repository tree contained 53 tracked files and was not truncated. All substantive backend source, the schema, tests, configuration, scripts, shared application shell, relevant imports, and architecture documentation were inspected. Excluded feature internals and visual assets were not evaluated. No application source was changed and no build or runtime tests were run.

## 2. What actually exists

| Area | Verified implementation | Gap and consequence |
|---|---|---|
| Backend routing | Two application endpoints: `GET /api/health` and `GET /api/hello` | No business reads or writes exist. Framework documentation endpoints do not change this assessment. |
| Application startup | FastAPI app and configured CORS middleware | No authenticated principal, permission enforcement, database lifecycle, business services, or durable background work. |
| Persistence | One SQL file creates `users(id, email, created_at)` | No workspace, project, membership, partner, task, approval, document, or analysis schema. |
| Database integration | Database README explicitly says it is not wired into the backend | Creating a SQL file currently does not make the application persist anything. |
| Database execution | Setup installs dependencies and copies example environment files | No migration execution, migration history, database provisioning, or useful seed data. |
| Frontend/backend transport | Next.js rewrites `/api/*` to `BACKEND_URL` | Useful transport foundation; no business API contract or authenticated interaction layer. |
| Shared connectivity indicator | `BackendStatus.tsx` attempts an API health request | It imports `@/lib/api`, which is missing from the tracked tree. A health response would establish only API reachability. |
| Shared account/search controls | Search input and a fixed initial in an avatar | No search behavior, account/session menu, identity loading, workspace selection, or permissions. |
| Product destinations | Only four page files, all in the excluded overview/execution feature area | No in-scope company, project, partner, work-item, approval, document, data, or administration pages. |
| Tests | Two backend tests for health and hello | No persistence, authorization, domain behavior, multi-user, or browser journey coverage. |
| Delivery | Local setup and development scripts | No tracked CI workflows, deployment configuration, dependency readiness, or recovery procedure. |

Evidence: [API registration](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/backend/src/app/api/router.py), [application startup](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/backend/src/app/main.py), [database status](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/db/README.md), [initial schema](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/db/migrations/001_init.sql), [backend dependencies](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/backend/pyproject.toml), [tests](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/backend/tests/test_api.py), [setup script](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/script/setup.sh).

### Immediate repository blocker

`BackendStatus.tsx` imports `@/lib/api`. Other tracked components import `@/lib/mock-data`. Neither module exists in the complete tracked tree. The TypeScript configuration maps `@/*` to `./src/*`, so these imports resolve to absent source files. This is a source-level build blocker; a build was not executed to produce a runtime/compiler log.

The root `.gitignore` contains the unanchored rule `lib/`, which would ignore `frontend/src/lib/`. That is a plausible explanation for the missing files, not proof of how they disappeared. Scope the Python ignore rule to the intended generated directory, or explicitly permit the frontend library directory, then commit the real modules. Restore any missing existing files before replacing their behavior; do not invent application data just to hide unresolved imports.

Evidence: [health component](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/frontend/src/components/BackendStatus.tsx), [TypeScript paths](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/frontend/tsconfig.json), [ignore rules](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/.gitignore), [shared shell](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/frontend/src/components/AppShell.tsx), [complete snapshot](https://github.com/khangdoan514/hacktx-2026/tree/f0f0b1fd6467914e5b2424c49093f02283183e8b).

## 3. Product model to build

A workspace represents a company. A workspace contains departments, human memberships, projects, partner relationships, and governed data. Projects can be internal or partner-facing and can contain subprojects. Tasks and requirements belong to projects. People can hold multiple functional roles and participate in multiple projects.

Every material piece of work should answer: who owns it, what outcome is requested, which evidence and assumptions apply, who must decide, what is blocking it, and who can see it. The platform should preserve that information across a handoff instead of requiring each person to reconstruct it.

Separate these concepts:

- **Job function:** management tier, BA seniority, transformation lead, PM, PO, engineer, DBA, partner representative. Describes responsibility and default queues.
- **Workspace permission:** owner, administrator, member, guest. Governs organization administration.
- **Project permission:** project owner, contributor, reviewer, viewer, scoped partner participant.
- **Resource visibility:** internal, named shared audience, or explicitly published partner material.
- **Decision authority:** assignment to a particular review or approval, with any amount/scope threshold.

Holding a senior job title must not automatically reveal every confidential project. Being an administrator must not automatically make someone the business approver. A person can have several job functions without needing several accounts.

### Priorities

- **P0 — coherent first usable product:** reproducible repository, Supabase setup and migrations, Auth, database access policies, workspace/project permissions, project/task lifecycle, partner invitation and restricted access, requests/requirements, structured handoffs, versioned human approvals, private Storage, comments, notifications, audit history, an end-to-end test, and versioned domain/event contracts that preserve future integration options.
- **P1 — complete intended business workflow:** governed datasets, repeatable analysis, data questions, editable dashboards and saved reports, revision/proposal review, objectives/outcomes, transformation plans, richer collaboration, and reliable imports/exports. Dashboard definitions and context APIs are reusable by a future external assistant without requiring one now.
- **P2 — expansion:** selected external integrations, advanced reporting and capacity planning, enterprise identity features, and operational scaling. Connecting the separately developed agent system is a dependency-driven integration milestone, not a requirement to build that system here. These are recommendations for later, not claims that the notes require every feature immediately.

If the demonstration centers on the DBA-to-BA capacity/margin scenario, promote the minimal dataset import, metric definitions, analysis snapshot, and report functionality from P1 into the demo slice.

## 4. Missing pages and their interaction contracts

All destinations below are missing from the tracked application. The routes are proposed, not existing URLs. `w` means the active workspace ID; `p` means a project ID. Every nested route must validate actual membership and resource ownership rather than trusting the URL.

| Priority | Destination / proposed route | Essential interactions | Required backend behavior |
|---|---|---|---|
| P0 | Sign-in and return flow: `/sign-in`, `/auth/callback` | Authenticate, return to intended permitted destination, sign out, recover from an expired session | Supabase Auth and its supported Next.js session flow; verify identity at FastAPI; map Auth UUID to profile; reject untrusted return URLs; use Supabase verification/recovery. |
| P0 | Workspace selection and onboarding: `/workspaces`, `/onboarding` | Create initial company, select existing membership, set timezone and basic profile | Atomic workspace plus owner membership creation; unique membership; no caller-selected ownership of another workspace. |
| P0 | Invitation acceptance: `/invitations/accept` | Preview intended company/project, accept, sign in with intended identity; handle expired/revoked/already-used link | Hashed, expiring, single-use token; identity binding; membership and redemption in one transaction. |
| P0 | My work: `/w/:w/my-work` | See assigned tasks, requested reviews, unanswered questions, overdue handoffs; filter and open source records | Permission-filtered combined queue; server-derived counts; stable sorting and pagination. This is an operational queue, not a front-page redesign. |
| P0 | Project directory: `/w/:w/projects` | Create internal/partner project, filter by owner/status/partner, archive and restore | Project persistence, type and parent validation, membership bootstrap, scoped listing. |
| P0 | Project detail: `/w/:w/projects/:p` | Edit outcome, owner, dates, constraints, scope; see blockers, children, milestones, latest decisions | Read model assembled from authorized records; version-checked edits; archive restrictions. |
| P0 | Project members and sharing: `/w/:w/projects/:p/members` | Invite/add/remove members, change permissions, select partner-shared resources, preview access | Membership/grant service; last-owner protection; immediate access re-evaluation; auditable grants. |
| P0 | Project work: `/w/:w/projects/:p/tasks` | Create and assign tasks, prioritize, filter, add subtasks and dependencies, change status | Valid transitions, assignee membership checks, dependency-cycle prevention, acceptance rules. |
| P0 | Work-item detail: `/w/:w/projects/:p/tasks/:task` | Edit brief/criteria, add evidence, comment, resolve blocker, submit for review, accept or return work | Task revisions, ownership, review authority, state history, conflict handling. |
| P0 | Request intake: `/w/:w/requests`, `/w/:w/requests/:id` | Partner/internal draft, submit, ask for information, triage, reject with reason, link to project | Draft versus submitted state; requester/triager permissions; idempotent conversion; retained original request. |
| P0 | Requirements: `/w/:w/projects/:p/requirements` | Define requirement, evidence, priority and criteria; review/version; link implementation and acceptance | Requirement revisions and traceability links; accepted baseline; change impact on open reviews/tasks. |
| P0 | Handoffs: `/w/:w/projects/:p/handoffs`, `.../:id` | Prepare context, send to person/role, acknowledge, return for missing information, reassign, complete | Immutable sent snapshot; owned recipient assignment; lifecycle/history; due dates and notices. |
| P0 | Human decisions: `/w/:w/approvals`, `.../:id` | Review exact evidence/version; approve, reject, request changes, delegate when allowed | Approval authority and resource access; immutable target version; race-safe resolution; audit event. |
| P0 | Partners: `/w/:w/partners`, `.../:id` | Record partner organization and contacts, invite representatives, inspect explicitly shared projects, suspend access | Partner organization distinct from internal department; explicit memberships and grants; no implicit workspace-wide read access. |
| P0 | Partner workspace: `/w/:w/partner/projects`, `.../requests` | Read shared projects, submit own requests/evidence, answer questions, review shared deliverables | Dedicated response projections excluding internal fields; partner-specific object filtering across every endpoint. |
| P0 | Documents/evidence: `/w/:w/projects/:p/documents`, `.../:id` | Upload/download, attach to work, replace with new version, record source, share/revoke | Private storage, metadata/versioning, scoped download authorization, upload validation, orphan cleanup. |
| P0 | Project discussion: `/w/:w/projects/:p/discussions` | Comment on requests/tasks/documents, reply, mention, edit/delete own message according to policy | Persisted comments, visibility inherited from parent, edit history/tombstones as needed, safe mentions. Begin with object-linked discussion. |
| P0 | Inbox: `/w/:w/inbox` | Read actionable events, mark read, open target, configure notifications | Durable notifications and read state; event deduplication; permission-safe previews and target checks. |
| P0 | Workspace members/settings: `/w/:w/settings/members`, `/settings/workspace` | Manage members, business roles/departments, invitation states, owner transfer, timezone | Administrative authorization, self/last-owner safeguards, revocation, workspace configuration validation. |
| P0 | Account settings: `/account` | Change own name, timezone, notification preferences; sign out | Self-scoped profile updates; protected identity-provider attributes; no self-escalation. |
| P0 | Project activity and administrative audit: `.../activity`, `/w/:w/settings/audit` | Inspect who changed/shared/approved what; filter by actor/resource/date | Server-generated append-only audit records; restricted visibility and exports; redact secrets. |
| P1 | Search: `/w/:w/search` | Search projects/tasks/requirements/documents/decisions; filter and navigate | Enforce authorization before selecting snippets, counts, suggestions and results. Search must not reveal hidden objects. |
| P1 | Objectives and outcomes: `/w/:w/objectives`, `.../:id` | Set objective, sponsor, constraints, baseline, target; link projects and record outcomes | Versioned targets and measurement provenance; approvals for agreed changes; aggregate by permitted projects. |
| P1 | Data catalog: `/w/:w/data`, `.../:dataset` | Register/import dataset; inspect owner/schema/units/freshness; grant access; request clarification | Dataset metadata and immutable versions, import validation, permissions, data issue tracking. |
| P1 | Metric definitions: `/w/:w/metrics`, `.../:id` | Define numerator/denominator, units, period, exclusions, calculation owner; review version | Versioned definitions; validation; dependency links; invalidate dependent analysis when definitions change. |
| P1 | Data questions: `/w/:w/data/questions`, `.../:id` | Ask DBA a specific question, attach context, assign, answer with evidence, accept resolution | Question lifecycle; source-version linkage; relevant-owner notification; accepted answer feeds a new context revision. |
| P1 | Analysis workspace: `/w/:w/projects/:p/analyses`, `.../:id` | Set business question/assumptions, choose approved dataset versions, calculate, compare scenarios, review | Deterministic registered calculations; input/output snapshots; quality gates; reproducible revisions. |
| P1 | Dashboards and saved reports: `/w/:w/projects/:p/dashboards`, `.../:id`; report/export views | Manually create/edit widgets, choose metrics and approved data, set filters, save drafts, inspect provenance, publish revision, export | Typed dashboard definitions, authorized calculations, immutable revisions and sharing. Future assistance uses these same operations. Visualization styling is out of scope. |
| P1 | Dashboard revision/proposal review: `.../dashboards/:id/revisions/:revision` | Preview a proposed definition, compare with current draft, edit, accept/discard, submit for approval | Base-revision checks, schema/data-access validation, provenance, transactional application of changes; publication remains separately authorized. Works for human revisions now and real externally supplied proposals later. |
| Future connection | Assistance request status/detail within the dashboard or analysis | When a real provider exists: request help, see genuine status, answer clarification, cancel, inspect returned draft | Capability availability, scoped context, correlation IDs, authenticated result ingestion, deduplication and access rechecks. Without a provider, hide/disable assistance and explain unavailable; never pretend to run. No standalone agent-management pages. |
| P1 | Transformation plans: `/w/:w/projects/:p/transformation` | Record current/proposed process, impacts, rollout stages, owners, risks and measurable results | Versioned plan, approval linkage, milestone/task linkage, outcome records. No visual flow-builder requirement. |
| P1 | Risk/change register: `/w/:w/projects/:p/changes` | Raise scope change, risk, issue or blocker; assess impact; decide; apply accepted changes | Typed records and state history; authority checks; updates to relevant baseline/requirements after approval. |
| P1 | Project channels: `/w/:w/projects/:p/channels`, `.../:channel` | Persist messages, threads, unread state, permitted attachments and mentions | Channel memberships, ordered/paginated messages, replay/reconnect handling, retention. Expand from basic object discussions. |
| P2 | Integration administration: `/w/:w/settings/integrations` | Connect selected external source, map records, sync, inspect failures, disconnect | Credential storage, scoped mappings, webhook verification, deduplication, retry/reconciliation. |
| P2 | Import/export history: `/w/:w/settings/transfers` | Review bulk import validation, import progress, failed rows, export status, retry | Durable job state, checksums/idempotency, authorization at execution and download, expiry and cleanup. Basic document transfer remains P0. |

### Interaction rules that apply everywhere

1. Direct URLs, browser refresh, copied links, and back/forward navigation must work. Filters worth sharing belong in the URL or a saved view.
2. Mutations report success only after the backend commits. Failed submissions preserve entered content and show actionable errors.
3. Double clicks, network retries and reconnects must not create duplicate invitations, requests, approvals or imports.
4. Loading, empty, permission-denied, not-found, expired-session, service-failure and edit-conflict states are separate behaviors.
5. Concurrent edits use a record version or `ETag`. A stale save returns a conflict and allows reload/reapply; it must not silently erase another person's work.
6. Member removal, partner suspension and sharing changes apply to downloads, exports, search, notifications and live connections as well as ordinary detail pages.
7. Archive is generally reversible. Before deleting referenced records, show dependencies and apply a defined retention/deletion policy.
8. Lists need server filtering, stable pagination and deterministic ordering. Displayed totals must reflect actual authorized data.
9. Status changes have explicit causes, actors, timestamps and permitted next states. Do not make a status label an unrestricted text field.
10. A partner-facing response must omit internal costs, comments and other restricted fields at the server, not merely hide them in the browser.

## 5. Business journeys and state rules

### A. Set up a company and involve a partner

An owner creates NMC3, adds departments and internal participants, creates an internal or partner project, records the partner organization, and invites its representative to that project. The representative accepts with the intended identity and sees only the specifically shared project material.

Invitation state: `pending → accepted | revoked | expired`. Reissuing an invitation must revoke or supersede the previous token. If a membership already exists, acceptance is idempotent. A forwarded link cannot grant access to the wrong identity. A removed representative cannot retain access through old links or exports. Project sharing must support internal notes and partner-visible deliverables within the same project without copying confidential fields accidentally.

Acceptance test: use an owner, internal contributor, Partner A and Partner B. Each sees precisely their allowed projects; swapping project/document IDs never exposes another audience's material.

### B. Partner request becomes agreed delivery work

The partner writes a draft request with desired outcome, constraints, evidence and due date. On submission, a BA/PM triages it, requests missing details, links or creates the project, and turns the agreed request into versioned requirements. The PO creates prioritized tasks with acceptance criteria.

Request state: `draft → submitted → triaging → accepted | rejected`; `triaging → needs_information → submitted`. Conversion retains the requester, source request and rationale. Retrying conversion must return the already-created project/link rather than creating a second project.

Requirement state: `draft → in_review → accepted | changes_requested → draft`, with accepted revisions retained. New scope produces a new revision and, when appropriate, a change request. Task completion does not automatically establish requirement acceptance.

### C. Human-to-human handoff without losing context

A sender prepares a package with project/task IDs, objective, requested output, source references and versions, metric definitions, assumptions, constraints, findings/evidence, open questions, access restrictions, recipient and deadline. Sending captures a revision. Acknowledgment establishes receipt; completion establishes delivery. These must be different events.

Handoff state: `draft → sent → acknowledged → completed`; `sent/acknowledged → needs_information → sent`; unresolved handoffs can be reassigned or cancelled with a reason. Reassignment changes responsibility but preserves the original history. If addressed to a role/team, one person must claim or be assigned ownership; a queue cannot be the final accountable owner.

Changes to source material after sending mark the handoff as outdated and allow an explicit new revision. Existing packages remain inspectable. A returning recipient explains what is missing and who can answer. Display a specific blocker such as “Waiting for the DBA to define whether infrastructure cost includes electricity.”

### D. Management or designated reviewer decides

A contributor submits a requirement, analysis, plan, scope change or deliverable for review. The reviewer sees a fixed version, supporting evidence and consequences. They approve, reject, or request changes. Delegation is explicit and bounded by policy.

Approval state: `pending → approved | rejected | changes_requested | cancelled | superseded`. If the target changes materially, its pending approval is superseded; a new approval targets the new revision. Two reviewers acting concurrently cannot both resolve a single-decision request. Approval history is immutable; corrections create a new decision record. The author cannot self-approve where separation of duties is required.

Start with one named approver. Add quorum, ordered signoff or amount-based authority only when the product needs it. Financial bounds in the notes are constraints, not authorization to build billing/payments.

### E. Product owner and engineer deliver against requirements

The PO assigns a task linked to accepted requirements. The engineer starts work, records blockers, attaches implementation references and verification evidence, then submits for review. An authorized reviewer accepts the deliverable or requests changes. Partner acceptance is a separate step when part of the agreement.

Task state: `todo → in_progress → in_review → done`; `in_review → changes_requested → in_progress`; permitted nonterminal states can move to `cancelled` with reason. Represent blockers/dependencies separately so blocked work retains its underlying lifecycle state. Reopening completed work records why and which acceptance is being revisited.

Completion requires the relevant acceptance criteria and evidence, not simply an unchecked status mutation. Track a GitHub link as evidence first; bidirectional issue synchronization is a later integration.

### F. DBA establishes data; BA makes a repeatable analysis

Start with a controlled CSV upload or predefined read-only data view. The DBA describes ownership, field meanings, join keys, units, period, freshness and access restrictions. Import produces an immutable dataset version and a validation summary. Invalid rows are rejected or quarantined with an explicit policy; no silent omission.

The BA records the question, target margin, reporting/forecast period and eligible partner scope. The calculation uses named dataset and metric versions. Missing cost definitions open a targeted data question. A resolved question produces a new definition/context revision, then a fresh analysis. Reviews refer to specific analysis revisions.

Source changes mark dependent results stale. Recalculation creates a new revision and comparison; it does not rewrite historical approved findings. Restricted raw data need not be exposed merely because a person can see an approved aggregate report.

### G. Convert analysis into an operating change and measure it

A BA submits findings and expected benefits to the transformation lead. The lead records the current process, proposed change, affected departments, rollout stages, risks, owners and outcome measures. Management approves the appropriate plan revision. Rollout creates linked delivery work. Outcome measurements compare against the recorded baseline and target.

Avoid individual surveillance metrics. Useful operational signals are unanswered questions, blocked handoffs, decision turnaround and project outcomes. Do not add keystroke tracking, presence-based productivity ratings or per-person activity scores.

### H. Collaborate and notify without leaking information

People discuss specific work and mention only people allowed to access it. A mention creates an actionable notification after the comment commits. The notification opens the exact object/revision when appropriate. An inaccessible target is handled safely if permissions change later.

Store notification read state separately from business state. “Read” does not mean “accepted,” “approved,” or “done.” Prefer actionable notifications to every field edit. Channels can be added after object-linked discussion is reliable. Human voice communication is optional later work; if added, it requires its own session authorization, participant access, recording consent and retention decisions.

### I. Close, archive and revisit a project

Before closure, show unresolved tasks, partner acceptance, pending decisions and outstanding handoffs. Project owners either resolve them or record an explicit closure exception. Archive makes ordinary mutation unavailable while preserving permitted read access. Restoration is explicit. Reopening a project does not undo old approvals or resurrect expired invitations.

## 6. Authorization specification

Authorize every request by authenticated identity, active workspace membership, project membership, resource visibility, intended action and lifecycle state. Derive the actor from the verified session; never accept `actor_id`, `workspace_owner`, or arbitrary roles from mutation payloads.

| Actor / permission | Administrative ability | Business actions | Critical boundary |
|---|---|---|---|
| Workspace owner | Workspace configuration, administrator assignment, owner transfer | Only business authority explicitly granted | Prevent removal of the final owner; sensitive content access must follow explicit policy. |
| Workspace administrator | Manage allowed memberships, invitations and settings | May participate where granted | Cannot assign themselves business approval authority implicitly. |
| Project owner | Manage permitted project membership, scope and lifecycle | Assign work and request decisions | Cannot elevate users into global administrators or expand own workspace authority. |
| Contributor | No default administration | Edit permitted drafts, own/assigned work, comment, submit reviews | Cannot approve merely because they created a record. |
| Assigned reviewer | No default administration | Decide on assigned version within authority | Must also have access to the target and required evidence. |
| Viewer | None | Read and permitted downloads | No mutation through alternate endpoints. |
| Partner participant | No internal administration | Own requests, shared evidence/comments, assigned deliverable acceptance | No internal project enumeration, costs, private notes, other partners or raw data unless explicitly granted. |

Functional defaults: junior BAs prepare findings; senior BAs review analysis; PMs set scope/priorities; POs manage acceptance/backlog; engineers provide technical evidence; DBAs own data definitions; management sets objectives and decisions at the appropriate scope. These defaults can produce suggested assignments, but authorization comes from stored grants and decision policy.

Explicitly decide whether subprojects inherit membership. Recommended initial behavior: no implicit inheritance that broadens access; copying members is explicit and visible. For cross-project dependencies, return a safe “restricted dependency” reference when the viewer lacks access to its details. Internal subproject existence itself may need redaction.

Revocation applies to newly authorized business requests by checking current database membership. Supabase JWT validation alone does not guarantee immediate session revocation; use additional session checks where the action requires that guarantee. Realtime channel authorization and already-issued signed URLs also have lifetimes: keep sensitive data out of broadcast hints, recheck permissions on refetch, and use authenticated downloads for immediate-revocation requirements or short-lived signed URLs with an explicit expiry window. Previously downloaded files cannot be recalled.

## 7. Backend architecture

Keep the existing Next.js-to-FastAPI arrangement. Build one modular backend and one relational source of truth initially. The requirements do not justify starting with microservices, multiple independent databases, or a custom permission framework scattered across routes.

**Selected foundation:** Supabase Postgres is the application database; Supabase Auth handles human identity and sessions; Supabase Storage holds private files; Supabase Realtime provides authorized update hints. FastAPI owns domain validation, business commands, approval transitions and the future external-system boundary. Next.js owns the application routes and user interactions. Supabase CLI migrations are the schema source of truth. Supabase Queues is the preferred durable transport when background volume requires it; a server worker processes real imports, exports, notifications and integration delivery. No agent runtime is built in this stack. Supabase is a new target architecture, not an existing integration in the audited source.

**Responsibility boundary:** Supabase provides infrastructure; FastAPI defines what business actions mean. A separate agent system may later consume allowed context and submit typed draft proposals through FastAPI. It receives no database password, Supabase secret/service-role key, unrestricted Storage access, or blanket authority to mutate domain tables.

| Layer | Responsibility | Concrete work |
|---|---|---|
| API routes | Validate transport input and serialize allowed output | Separate request/response models; thin routes; consistent errors; pagination and version checks. |
| Authentication dependencies | Resolve the current human or explicitly registered integration identity | Validate Supabase access JWTs for humans, including issuer/audience/signature/expiry; map `sub` to profile. Future integration credentials use their own scoped principal, never an impersonated human session. |
| Authorization policies | Decide access for an action on a resource | Central workspace/project/resource checks plus tested Postgres RLS; explicit permitted-field projections for partners; future delegated access is restricted to both the user's and integration's allowed scope. |
| Domain services | Apply business rules | Project lifecycle, membership, invitation redemption, task transitions, handoffs, approvals, publishing. |
| Repositories/database | Persist and query Supabase Postgres | Transactions, constraints, Supabase migrations, indexes, RLS and consistent tenant scoping; restricted runtime role separate from migration credentials. |
| File service | Coordinate domain metadata and Supabase private Storage | Upload initiation/completion, validation, download authorization, version linking, cleanup; explicit Storage policies or narrowly authorized server operations. |
| Event/outbox service | Record effects of committed changes | Write business state, audit entry and outbox event atomically; dispatch later. |
| Worker | Perform retryable background work | Delivery attempts, imports, exports, stale-result propagation, scheduled reminders. |
| Query/report service | Produce scoped aggregates | Authorized counts, queues, search, reports and portfolio summaries. |
| Dashboard/artifact service | Store validated editable definitions and revisions | Shared manual/API operations; allowlisted widgets/calculations; source lineage; draft/review/publication lifecycle. |
| External integration boundary | Exchange versioned context and proposals with an eventual real provider | Specify contracts now; activate request delivery/callbacks only with a real authenticated integration; no reasoning, generation or simulated agent activity. |

Suggested repository locations, all new unless noted: `backend/src/app/core/{auth,permissions,errors,database}.py`; domain packages for `workspaces`, `projects`, `partners`, `requests`, `requirements`, `tasks`, `handoffs`, `approvals`, `documents`, `collaboration`, `data`, `dashboards`, `artifacts` and `audit`; `backend/src/app/jobs/`; `supabase/config.toml`, `supabase/migrations/`, `supabase/seed.sql` and database policy tests; `frontend/src/lib/api.ts` and Supabase browser/server clients. Keep integration schemas under a versioned contract directory; add provider-specific adapters only when needed. Reconcile the starter `db/migrations/001_init.sql` into the Supabase migration history once, rather than keeping two migration systems. The Python ORM, if used, must not independently auto-create production schema.

Continue using `/api` transport. Introduce `/api/v1` for business endpoints and retain health endpoints separately. Use Supabase's supported Next.js SSR session integration and forward the human access token as bearer authorization to FastAPI. FastAPI independently verifies the token and current application membership; never trust a decoded token, client-provided role, or the user object from an unverified session. Handle refresh without caching user-specific session responses across people. If a cookie-authenticated mutation endpoint is introduced, add the applicable CSRF protections. CORS is not authentication. [Supabase SSR guidance](https://supabase.com/docs/guides/auth/server-side/creating-a-client?queryGroups=framework&framework=nextjs), [verified JWT claims](https://supabase.com/docs/reference/javascript/auth-getclaims).

### Supabase access paths and security model

The initial product uses FastAPI for business reads and mutations. Browser Supabase access is limited to Auth, permitted Storage operations and private Realtime subscriptions. Keep core tables in a non-exposed application schema; do not expose business-state mutations through generated REST/GraphQL endpoints that bypass FastAPI transition rules. Any later direct Data API read projection requires explicit grants and tested RLS. Grants and RLS are distinct controls. [Data API security](https://supabase.com/docs/guides/api/securing-your-api).

FastAPI should use a dedicated least-privileged Postgres runtime role without `BYPASSRLS` and without ownership of protected tables. In a transaction, establish the verified principal through transaction-local database context used by application RLS predicates. Set it from verified identity only; never from a request body. A direct SQL connection does not automatically inherit Supabase JWT claims. Reset context by transaction completion and test pool reuse across users. Keep migration credentials separate. Workers use explicitly scoped service permissions and never treat a global service key as proof of user authorization.

Enable RLS on all application tables, with additional protection mandatory for any exposed schema. Define policies by workspace/project membership and resource grants rather than simple record ownership. Test `SELECT`, `INSERT`, `UPDATE` and `DELETE` independently; updates need both old-row access and new-row validation. Store permissions in controlled membership tables, never user-editable Auth metadata. Re-evaluate current membership rather than assuming JWT role claims stay fresh. Review views/functions for privilege escalation; prefer invoker security and explicitly restrict function execution. [Supabase RLS](https://supabase.com/docs/guides/database/postgres/row-level-security).

Keep partner-sensitive fields in separate protected relations or produce explicitly filtered server responses; RLS filters rows, not individual fields of an otherwise readable row. No direct broad table grant may expose costs or private notes merely because the project row is shared. Background and integration principals need their own access tests.

Use `auth.users` for managed identity and an application `profiles` table keyed by its UUID. Keep workspace and project roles in application tables. Supabase Auth invitations/sign-up establish identity; the application's single-use invitation record separately establishes the correct workspace/project access. Do not grant membership merely because a matching email address exists.

Use private Storage buckets, unique immutable object keys per document version and server-authorized upload completion. Metadata, tenant/project ownership and stored-object existence must agree before publication. Signed URLs are bearer access for their validity period; never describe them as immediately revocable by a membership-row update. For strict revocation use an authenticated, currently authorized download path. [Storage access models](https://supabase.com/docs/guides/storage/buckets/fundamentals).

Use private Realtime Broadcast channels with channel policies and minimal invalidation hints; fetch actual records through FastAPI again. Persist inbox messages and domain events in Postgres because a live notification is not the durable record. Rejoin/resync after reconnect or token refresh. Channel permission caching means membership changes must not rely on a socket disconnect alone; hints contain no confidential records. Client broadcasts cannot commit business state or claim that external work completed. [Realtime authorization](https://supabase.com/docs/guides/realtime/authorization).

Keep custom tables/functions outside the managed `auth`, `storage` and `realtime` schemas. Configure supported policies without altering managed Realtime objects; its schema is locked down, and `realtime.messages` RLS is already enabled. [Realtime migration restriction](https://supabase.com/changelog/realtime-schema-locked-down-against-modification).

Browser configuration contains only the project URL and publishable key. Supabase secret/service-role keys and database credentials remain server-only. Prefer the supported current client packages, pin versions and commit lockfiles. Update the repository's Node 18+ prerequisite to a supported Node release of at least 22 and verify it against the chosen frontend versions. [Supabase Node support change](https://supabase.com/changelog/45715-deprecation-notice-dropping-support-for-node-js-20).

### Transaction and job rules

- Resolve a human approval with a conditional update on pending state and expected version. Target transition, decision record, audit entry and outbox event commit together.
- Redeem invitations with a transaction and uniqueness constraints. Parallel acceptance cannot create duplicate memberships or redeem a revoked token.
- Send notifications after commit via an outbox; a failed email must not roll back an already-valid approval or cause it to repeat.
- Use event IDs/deduplication keys and bounded retry/backoff. Assume at-least-once delivery; make consumers idempotent.
- Durable jobs have `queued`, `running`, `succeeded`, `failed` and, where meaningful, `cancelled` states, with attempt count, last error and timestamps. Add leases/recovery so a process crash does not strand running jobs forever.
- Authorization must be checked when work executes, not only when it is enqueued. Cancellation semantics must distinguish “requested” from “completed.”
- Use Supabase Realtime for update hints and a normal API refetch/polling fallback for durable status. Reconnects need a full resync or persisted event cursor, not reliance on transient broadcast history.
- Start with the transactional outbox and one server worker. When adopting Supabase Queues, use durable queues kept inaccessible to browsers and external providers. It supplies transport, not a hosted application worker or an agent runtime; implement leases, retries and idempotent effects. Visibility windows do not guarantee exactly-once business side effects. [Supabase Queues](https://supabase.com/docs/guides/queues), [queue access and durability](https://supabase.com/docs/guides/queues/quickstart).

## 7A. Future agent integration: application responsibilities only

### The boundary to preserve

The platform owns projects, permissions, data definitions, dashboards, artifacts, revisions and human decisions. A separately developed agent system may later receive a specific authorized request and return a proposed change or a clarification question. The platform authenticates that exchange, validates the output, records provenance, and lets a permitted person decide what to apply. The external system owns how it reasons, coordinates agents or generates its proposal.

| Build in this application | Define now; activate with a real external system | Outside this application's work |
|---|---|---|
| Manual dashboard CRUD, typed definitions, safe calculations, revisions and publishing | Submit a scoped assistance request and track its actual delivery/status | Models, prompts and model/provider selection |
| Context retrieval by project/resource/version with current access checks | Allow a registered service to fetch a bounded context snapshot | Agent orchestration, planning, inter-agent communication and execution |
| Reusable draft/proposal validation and review operations | Receive authenticated status, clarification and artifact-result messages | Tool-running frameworks, autonomous database access, agent memory/vector retrieval |
| Provenance fields, human approval and append-only audit history | Correlate a real external request/result to its internal record | Fake agents, scripted assistants, timer-generated progress or pretend conversations |
| Versioned domain events and schemas, durable outbox, generic integration permissions | Retry delivery to a real configured provider and reconcile uncertain outcomes | Agent dashboards, agent builders, agent-run inspectors or agent evaluation systems |

Contract readiness is useful work now. A standalone agent registry, generic capability marketplace, multi-provider router or custom agent protocol is unnecessary. Agree the smallest HTTP/JSON exchange with the future system owner; extend it only for a concrete capability. A missing provider must not block ordinary project or dashboard functionality.

### First future capability: dashboard creation assistance

Implement manual dashboards first. Their definitions contain a schema version, title, description, supported widget types, metric IDs/versions, dataset or approved query references, dimensions, aggregation rules, filters, units, source lineage and bounded configuration. The renderer supports a fixed catalog of permitted widgets. It never evaluates returned JavaScript, Python, HTML or arbitrary SQL. Layout styling is still outside this plan; serializable widget order/configuration is a functional requirement.

An example later interaction is: “Create a dashboard comparing partner capacity and projected margin; exclude contracts expiring this quarter.”

1. The user opens a project dashboard or creates a draft and chooses the question plus relevant datasets/metrics. Available data is filtered by their existing permissions.
2. If no real assistance provider is configured and healthy enough to accept requests, the app shows assistance as unavailable and keeps manual editing available. It does not queue imaginary work or generate a dashboard automatically as a fallback.
3. With a real provider, FastAPI records the request with the actor, project, target/base revision, capability `dashboard.propose.v1`, authorized inputs and expiry. An outbox event delivers it after commit.
4. The provider receives scoped versioned context or an opaque, expiring grant to retrieve it. It receives no unrestricted schema dump, company-wide documents or admin credentials.
5. The real provider may return a targeted clarification question. The user supplies an answer tied to that request and context revision. It appears as an attributed external-service question, never a fabricated coworker message.
6. The provider submits a typed dashboard proposal with source references and its real external correlation ID. The app validates the schema, widget types, permitted dataset/metric references, source versions and current authorization. Calculated display values come from the platform's authorized query/calculation path; asserted numbers in a proposal are not trusted as verified results.
7. The user previews the draft, inspects provenance and differences, edits it, discards it, or applies it. Applying creates an ordinary dashboard revision. Publishing/sharing or approving it follows the same policy as a human-authored revision.
8. If the target or sources changed during generation, preserve the proposal and mark it stale/conflicted. Do not overwrite newer work. A user can rebase/revise or make a new request against current context.

The same application contracts can later support an analysis explanation, requirement draft or handoff summary. Each output must have its own validated schema and allowed effects. Do not implement those generators in this repository or create speculative schemas for dozens of capabilities.

### Context contract

The authoritative API exposes a bounded context package: workspace/project IDs; task or dashboard reference and revision; the business objective and requested output; permitted dataset, metric and document version references; approved assumptions and constraints; current findings; unresolved questions; output schema/version; and the effective sharing restrictions. Store a context manifest with the immutable references/digests needed for audit without duplicating all private data unnecessarily.

Access is the intersection of the requesting person's current permissions, the registered integration's grants and the request-specific allowed resources/actions. The backend derives workspace and actor from trusted records; a provider cannot widen them by changing fields in a callback. Retrieve only requested authorized slices and apply size/row limits. Prefer references and approved aggregates; raw data is transferred only under an explicit grant. Permission removal invalidates future context fetches and proposal application. Data already delivered to an external system cannot be recalled; record the disclosure and agree retention/deletion obligations when connecting that system.

### Request/result contract and lifecycle

Proposed integration-only API families, documented now and enabled only with a real configured integration:

- `GET /api/v1/projects/{p}/assistance-capabilities`: actual configured availability and supported schema versions. No provider means unavailable, not a fabricated capability list.
- `POST /api/v1/projects/{p}/assistance-requests`: human-authorized request with target/base revision, capability, instructions, approved input references and an idempotency key. Return `503 assistance_unavailable` if no usable integration exists; do not persist a pretend queued job.
- `GET /api/v1/assistance-requests/{id}` plus clarification reply and cancellation commands: current authorized status and attributable messages; no model/runtime internals.
- A scoped context endpoint keyed by request and context revision: provider access is authenticated and limited to that request. No arbitrary `workspace_id` or SQL endpoint.
- `POST /api/v1/integrations/{integration_id}/events`: authenticated provider status, clarification or proposal-result envelope; schema validation and deduplication before updating application records.
- Existing dashboard draft/revision and proposal apply/discard endpoints: human actions route through the same domain service regardless of proposal origin.

The request envelope includes `schema_version`, `request_id`, `capability`, `project_id`, target/base revision, context manifest ID, output schema/version, correlation/idempotency ID and expiry. Requester identity and the granted permissions are assigned by the server. Never let the caller choose an arbitrary callback URL or make the app fetch an arbitrary result URL; use configured destinations and scoped upload completion if a file result is needed.

The result envelope includes `schema_version`, `event_id`, `request_id`, integration identity derived from credentials, external request ID, event type, sequence/revision, timestamp, context/base revision, and either a typed proposal, bounded clarification question or safe error. Provenance records integration ID, original requester, source versions and received time. An optional external agent identifier may be stored as opaque provenance from the real system; do not invent agent identities.

Application lifecycle: `queued → submitted → in_progress → proposal_received`, with `needs_input` and terminal `failed`, `expired` or `cancelled` branches. Use `in_progress` only when acknowledged by the actual provider. Keep proposal validation and human review separate: `pending_validation → ready_for_review | invalid | stale → applied | discarded`. Receiving a result is not business approval. Delivery timeout produces an unknown/reconciliation state until queried or resolved, rather than blind resubmission of potentially running work.

Only `ready_for_review` proposals may transition to `applied`, after a fresh permission/base-revision check. Invalid or stale proposals must be corrected/rebased and successfully revalidated first; the state shorthand does not permit applying invalid content.

Callbacks require a registered service identity and verified signature/token; for webhook signatures verify timestamp, raw-body signature and replay window. Scope credentials per integration/environment, rotate them, and keep them server-side. Use `(integration_id, event_id)` for deduplication and validate event ordering/transitions so late messages cannot resurrect cancelled or terminal work. Concurrent callback retries create one result. Cancellation is initially a request; record whether the provider acknowledged it. Late results can be retained as quarantined evidence but cannot automatically apply changes.

Only authorized humans may accept/publish proposals in this scope. An integration can submit drafts and clarification, not grant itself access, impersonate a reviewer, approve its own work, silently alter membership, or publish to partners. Agent-provided text is untrusted content; it cannot redefine permissions, workflow rules or server instructions.

### Product behavior while the system is disconnected

- All human project, analysis and dashboard flows remain operational.
- No fake chatbot, fake agent avatars, fabricated activity counters or simulated response stream.
- Hide assistance entry points, or mark them unavailable with a brief explanation. Do not present a working “Generate” action until real execution is connected.
- If a real connection later becomes unavailable, show the last durable known state and allow appropriate cancel/retry/manual options without inventing results.
- Seed only clearly identified business demo data. Never seed fake agent proposals, conversations or completion events into the product.
- Unit/contract tests may submit synthetic payloads directly to isolated validators with credentials limited to tests; no mock provider, simulator or seeded agent is shipped or shown as a demonstration.

### Integration acceptance gate

Before enabling assistance, the external team and application team must agree on the first capability, schema versions, service authentication, delegated resource scope, callback/delivery semantics, cancellation/expiry, data retention and one real end-to-end test. Until then, implement the reusable domain artifacts and maintain the contract document. Do not claim agent integration is complete merely because an endpoint or test payload exists.

## 8. Proposed relational model

This is a logical schema, not a requirement to create every table on day one. Build the P0 groups first. Store core relations in columns and join tables; use structured JSON for validated snapshots/configuration rather than hiding the entire product in unvalidated blobs.

| Group | Entities and key fields | Required relationships / invariants |
|---|---|---|
| Identity | Supabase-managed `auth.users`; application `profiles`: Auth UUID, display name, status, timezone | Profile references verified Auth identity; no duplicate password/session tables; email alone cannot grant membership; historical audit identity survives the defined user-deletion policy. |
| Workspace | `workspaces`: name, timezone, status; `workspace_memberships`: user, access role, status | Unique workspace/user membership; preserve at least one active owner. |
| Organization | `departments`; `functional_roles`; `member_functional_roles` | Department belongs to one workspace; many human functions per member; no access implication from job title alone. |
| Partners | `partner_organizations`; `partner_contacts`; `partner_memberships` | Distinguish external organization records from internal teams; user identity can exist before/after contact invitation. |
| Invitations | `invitations`: token hash, intended identity, inviter, scope, role, expiry, consumed/revoked times | No raw tokens in logs/database; explicit target scope; redemption concurrency control. |
| Projects | `projects`: workspace, parent, type, partner, owner, objective, dates, state, version | Parent in same workspace; no cycles; owner is active eligible member; partner type validates relationship. |
| Project access | `project_memberships`; resource-specific sharing/grant records | Unique grants; membership is not universal document sharing; granted role cannot exceed granter authority. |
| Requests | `requests`; `request_revisions`: requester, outcome, constraints, state, linked project | Original submission retained; conversion link unique/idempotent; partner can edit permitted draft fields only. |
| Requirements | `requirements`; `requirement_revisions`: text, criteria, priority, state | Accepted revision immutable; link requests, tasks, decisions and evidence. |
| Tasks | `tasks`: project, parent, owner/assignee, priority, due date, lifecycle, version; `task_dependencies`; `milestones` | Subtasks/dependencies are valid and acyclic; completion validates evidence/criteria; blocked reason separate. |
| Handoffs | `handoffs`; `handoff_revisions`; `handoff_events` | Sent context revision immutable; recipient and accountable owner tracked; source references versioned. |
| Decisions | `approval_requests`; `approval_assignments`; `approval_decisions` | Refer to exact target revision; one final resolution for single-approver policy; decision append-only. |
| Evidence | `documents`; `document_versions`; `attachments`; `source_references` | Stable document identity, immutable stored version, checksum, storage key, size/MIME, visibility; validate every linked object. |
| Discussion | `comments`; optional `comment_revisions`; later `channels`, `channel_memberships`, `messages` | Inherit target visibility; replies remain in correct project; private replies cannot accidentally become public. |
| Notifications | `notifications`: recipient, event, safe payload, read time; `notification_preferences`; `delivery_attempts` | Deduplicate event/recipient/channel; generated from committed events; handle revoked access. |
| Audit/outbox | `audit_events`; `outbox_events` | Actual actor type/ID, initiating human when delegated, action, workspace/project/resource, revision, timestamp, request/correlation ID, schema version; immutable audit; delivery status separate. No synthetic service actors. |
| Data P1 | `datasets`; `dataset_versions`; `dataset_fields`; `data_access_grants`; `import_jobs`; `data_quality_issues` | Owner, source, freshness, schema and units; immutable import/version; row counts and validation summary. |
| Metrics P1 | `metric_definitions`; `metric_versions` | Units, period, formula/reference, exclusions, owner/reviewer; dependency tracking. |
| Questions P1 | `data_questions`; `question_responses` | Dataset/metric/analysis revision, requester, accountable responder, accepted answer, state. |
| Analysis P1 | `analyses`; `analysis_revisions`; input/dependency links; `analysis_results` | Persist question, assumptions, dataset/metric versions, calculation version, quality state and outputs. |
| Dashboards/reports P1 | `dashboards`; `dashboard_revisions`; `reports`; `report_revisions`; publication/share records; `export_jobs` | Typed definitions separate from calculated results; immutable published revision; human or verified external provenance; access checked for definition, aggregate, drilldown and export. |
| Proposals P1 | `artifact_proposals`: target/base revision, typed payload, source manifest, provenance, validation/review state | Same review path for human-submitted and later real external drafts; accepting produces a domain revision atomically; no fabricated provider/agent attribution. |
| Strategy P1 | `objectives`; `objective_measures`; `outcome_measurements`; `project_objectives` | Baseline/target/actual have units, period, source and owner; no fabricated progress percentage. |
| Transformation P1 | `transformation_plans`; `plan_revisions`; rollout stages; `change_requests`; `risks` | Link approval, impacted requirements, implementation tasks and outcome measures. |
| Integration P2 | `integrations`; `external_mappings`; `sync_jobs`; `webhook_receipts` | Scope credentials and mappings to workspace/project; unique external IDs; idempotent receipt processing. |
| Assistance boundary, deferred until connection | `assistance_requests`; scoped context manifests/grants; clarification records; provider event receipts linked to existing `integrations` and `artifact_proposals` | Store only actual authorized requests/events; reference real integration identity and external correlation; expiry, cancellation and current access enforced. These are application request records, not agent runtime, memory or execution tables. |

### Database conventions and integrity

- Use opaque IDs for externally addressed resources and Supabase Auth UUIDs for human identity. Reconcile the starter serial users table into Auth-linked profiles through an explicit migration; do not auto-link existing emails without verified ownership.
- Include `workspace_id` on tenant-owned data and `project_id` where applicable. Cross-table references must prove the same tenant, using composite foreign keys or equivalent database constraints where practical, plus application policy checks.
- Record created/updated times in UTC and display in user/workspace timezone. Business reporting periods have an explicit timezone.
- Use decimal types plus currency/unit metadata for financial and capacity values; avoid binary floats for money. Preserve original measurement precision.
- Add indexes for membership lookup, `(workspace_id, status, updated_at, id)`, project/assignee/due-date queues, pending reviewer queues, unread inbox, and event delivery state. Verify against actual query plans before adding speculative indexes.
- Enforce unique memberships, token hashes, external mappings and idempotency records. Record idempotency request hashes so reusing a key for different content fails clearly.
- Apply foreign keys, not-null/check constraints, and consistent tombstone/archive behavior. Avoid cascade deletion of historical decisions and evidence.
- Prefer typed association tables for critical references. If using a shared resource registry for approvals/attachments, it must enforce valid same-tenant targets; an arbitrary `target_type`/`target_id` pair is insufficient by itself.
- Define retention by record class. Archiving is not deletion, and soft deletion is not a complete privacy deletion workflow. Include storage cleanup and backup-retention implications in later deletion procedures.
- Use least-privileged database credentials and tested Supabase Postgres RLS as specified in section 7. RLS is required for application tables in this design and must not be treated as optional; it does not replace lifecycle validation or field-level response filtering.

## 9. API contract inventory

The following are proposed operation families under `/api/v1`. Use explicit commands for state changes with business meaning. Ordinary metadata editing can use `PATCH`. All list operations need bounded page size, stable cursor/order, validated filters and permission-scoped totals.

| Domain | Core endpoints / commands | Contract detail |
|---|---|---|
| Identity | `GET /me`, `PATCH /me`, Supabase Auth callback/session flows | Return verified profile and eligible workspace memberships; no sensitive provider tokens in profile responses. |
| Workspaces | `GET/POST /workspaces`, `GET/PATCH /workspaces/{w}` | Workspace creation atomically grants creator ownership. |
| Members | `GET /workspaces/{w}/members`, membership role/status updates | Prevent final-owner removal and unauthorized privilege escalation. |
| Invitations | `POST /workspaces/{w}/invitations`, list/revoke/resend; invitation preview/accept | Short-lived bearer token; identity binding; token errors do not disclose extra membership data. |
| Projects | `GET/POST /workspaces/{w}/projects`, `GET/PATCH /projects/{p}`, archive/restore/close | Check tenant through actual project record; lifecycle commands validate unresolved work policy. |
| Project access | List/add/update/remove `/projects/{p}/members`; grant/revoke resource sharing | Changes audited; server returns effective permissions for the current user. |
| Partners | Workspace partner list/create/detail/update; representatives and invitations | Separate organization/contact maintenance from project access grants. |
| Requests | Project/workspace request list/create; detail/edit; submit, triage, request-information, accept/reject, convert | Conversion supports idempotency and returns resulting links. |
| Requirements | Project list/create; detail/revisions; submit-review, accept/request-changes | Mutation targets a version; accepted text cannot be silently overwritten. |
| Tasks | Project list/create; detail/update; dependency operations; start, submit-review, complete, reopen, cancel | Assignee and dependency validation; transition authorization. |
| Handoffs | Project list/create; detail/revisions; send, acknowledge, return, reassign, complete/cancel | Send freezes the package; recipient access is validated before transmission. |
| Approvals | Scoped queue; create/detail; approve, reject, request-changes, delegate/cancel | Require pending state, target revision, expected version, authorized reviewer and rationale where needed. |
| Documents | Upload initiation/finalization; metadata/version list; authorized download; sharing | Finalization verifies stored object metadata; missing/invalid uploads never become downloadable evidence. |
| Discussion | Target comments list/create/update/delete; later channel/message operations | Parent target access and visibility determine permitted output and mentions. |
| Inbox | Notification list; mark one/all read; preferences | Read operations are idempotent; mark-all applies to a defined cutoff/filter. |
| Search | `GET /workspaces/{w}/search` | Search authorization covers snippets, facets and counts. |
| Audit | Project activity and admin audit list/export | Business history and admin-only metadata may require different projections. |
| Data | Dataset registration, import initiation, import status, versions/schema, grants | Raw source credentials never returned; imported data validated before publication. |
| Questions | Create/list/detail/respond/accept/reopen data questions | Link accepted answers to relevant definition/context revision. |
| Metrics/analysis | Metric version CRUD/review; analysis create/calculate/status/revisions/compare | Allow registered calculations and validated parameters; do not expose arbitrary privileged SQL execution. |
| Dashboards/reports | Create/update typed draft, list revisions, validate/preview proposal, apply/discard, publish revision, fetch results, export/status/download | Manual creation works now; later external drafts use the same domain contract. Reject unsafe widgets/queries, inaccessible references and stale base revisions. Export rechecks permission. |
| Future assistance boundary | Capability availability, scoped request/context, clarification/cancel/status, authenticated event intake | Contracts in section 7A; activation depends on a real external provider. No agent execution endpoints in this application. |
| Objectives/plans | Objective/measurement records; transformation revisions; rollout and risk/change operations | Baseline changes are explicit and traceable. |

Common error format should include a stable code, human-readable message, safe field errors and request ID. Suggested distinctions: `401` unauthenticated, `403` forbidden where disclosure is appropriate, `404` unknown or deliberately concealed, `409` stale version/invalid current state, `422` validation, `429` rate limit, and `503` unavailable dependency. Do not return raw database exceptions or credentials.

### Example: approval resolution

`POST /api/v1/approvals/{approval_id}/approve` accepts `expected_version`, the target revision being approved, and optional comment. An idempotency key identifies a retried submission. The server derives the actor, checks active access and reviewer authority, confirms target revision/pending state, then commits the decision and domain change with its audit/outbox entries. The response returns the committed approval and target versions. A stale or already-resolved request returns the current safe state; an exact retry returns the original result.

### Example: handoff package

The request body contains the project/work-item reference, objective, requested output, recipient, due date, source-version references, metric-version references, assumptions, constraints, findings/evidence links and unresolved questions. Access restrictions are validated against stored resource policy; a user cannot broaden sharing by declaring a more permissive field in the package. The server assigns package ID/revision, sender and timestamps.

## 10. Data and analysis requirements for the NMC3 example

For “Which partners can receive more compute capacity while maintaining target margin?”, the minimum data contract needs:

- Partner and contract identifiers, active dates, contract status and authorized sharing scope.
- Capacity by resource/site and period, with available versus committed versus consumed capacity clearly distinguished.
- Usage with explicit units and reporting period; allocated, consumed and billed GPU-hours must not be treated as interchangeable.
- Revenue/pricing and cost components with currency, unit, time basis and missing-value policy.
- Desired extra allocation or scenario volumes, forecast period, margin target and any contract-expiry exclusion.
- Dataset owner, source/refresh time, field definitions and validation status.

The analyst and DBA must agree whether the decision uses current gross margin, projected post-expansion gross margin, contribution margin or another definition. A possible agreed gross-margin definition is `(revenue - included_cost) / revenue`; it is a proposed metric, not a formula established by the repository. Define what happens when revenue is zero, cost data is missing, currencies differ, or reporting periods do not align. Projected eligibility also requires a documented revenue/cost model for the additional capacity; current margin alone does not prove profitable expansion.

Separate business eligibility from allocation feasibility. Several partners may individually qualify while their combined requested allocation exceeds available capacity. A selected allocation plan must check the aggregate resource constraint and require human prioritization or an explicit rule when capacity is insufficient.

Required calculation checks: duplicate join keys, missing partner/contract matches, incompatible periods, inconsistent units, negative/unexpected values, missing costs, duplicate imports, stale sources and expiring contracts. Record excluded records and why. Do not quietly convert missing costs to zero.

Each result revision stores input versions, parameters, metric/calculation version, author, timestamp, quality findings and output. A saved report should explain the definition and source behind a number, provide permitted supporting detail, and export the same scoped data. No 3D visualization or styling is required to make this functionality correct.

Dashboard assistance later proposes a definition against these governed metrics and data references. It must not introduce a second unverified calculation path or silently replace the agreed margin definition. Keep manual editing and registered calculations as the first usable implementation; the provider can request a missing definition through the same persisted question/context process.

## 11. External systems and integration scope

The notes describe uniting existing business tools. That is a product direction, not evidence that integrations already exist. Begin with links and controlled imports. Then choose one integration based on the concrete demo/use case rather than attempting every vendor at once.

The separately developed agent system is one future external integration with the stricter context/proposal boundary in section 7A. Define that contract early alongside dashboards and audit events. Do not defer basic revision/provenance design until the external system arrives, and do not build its orchestration or pretend its connection exists.

For any later issue tracker, document system, messaging system or BI/data source, specify: which system owns each field, allowed read/write direction, identity mapping, project mapping, authorization scope, conflict policy, sync frequency, deletion behavior, and disconnect effects. Store credentials server-side using an appropriate secret facility; never place provider secrets in public frontend environment variables.

Webhooks need signature validation, timestamp/replay handling, event deduplication and durable receipt storage. Polling needs cursors and backoff. Both need reconciliation to recover missed events. A provider outage must expose a stale/sync-failed state without corrupting the last valid local record. External writes are separate explicit business actions with idempotency and audit history.

Do not build payroll, complete HR administration, travel booking, billing, or full ERP functionality based on incidental examples in the notes. Those can become future project types after the common collaboration foundation exists.

## 12. Build sequence and work packages

Dependencies should determine order. Estimates below are relative complexity, not calendar promises: S = contained, M = a domain with several routes/interactions, L = several coupled domains. Actual timing depends on team size, deployment choices and demo deadline, none of which were supplied.

| Package | Scope | Depends on | Size | Exit criterion |
|---|---|---|---|---|
| B00 | Restore missing tracked modules; correct ignore scope; align Node prerequisite and setup docs; establish clean-checkout verification | None | S | Fresh checkout installs, typechecks/builds, and reaches backend with no untracked source dependency on a supported runtime. |
| B01 | Supabase local/project configuration, Postgres runtime roles, CLI migrations, profile/schema baseline and deterministic business-data seed | B00 | M | Local empty database migrates and seeds; restart preserves records; no second migration source or fake agent data. |
| B02 | Supabase Auth/Next.js session integration, FastAPI JWT verification, profile mapping, session expiry/sign-out, workspace membership | B01 | M | Two actual test identities receive distinct memberships; expired/invalid tokens and unauthorized API calls fail. |
| B03 | Domain authorization plus Postgres RLS, controlled API exposure, project/resource tests, partner response projections | B02 | L | Cross-tenant access is denied through FastAPI, exposed Supabase paths, storage, views and pooled connections; server runtime does not bypass RLS. |
| B04 | Workspace/member administration, departments/functions, invitations and owner protections | B02–03 | M | Invite/accept/revoke/resend and removal work, including concurrent redemption and final-owner cases. |
| B05 | Projects, subprojects, membership, partner organization records, archive/restore | B03–04 | M | Internal and partner projects persist with correct isolation and lifecycle. |
| B06 | Requests and requirements with revisions and traceability | B05 | M | Submitted request converts exactly once and links to an accepted requirement revision. |
| B07 | Tasks, subtasks, dependencies, acceptance evidence, My work queue | B05–06 | M | Assignment and review lifecycle survive refresh; cycles and invalid transitions are rejected. |
| B08 | Supabase private Storage, document/file versions, signed or authenticated transfers, attachments and sharing | B03, B05 | M | Authorized user uploads/downloads; unauthorized user cannot obtain metadata or file bytes; revocation behavior matches chosen URL expiry policy. |
| B09 | Structured handoffs and ownership/return/reassignment | B06–08 | M | A recipient receives complete context, returns a targeted question and completes a revised package. |
| B10 | Human approvals with version binding and concurrency control | B06–09 | M | Exact-version approval changes valid business state once; stale/conflicting approvals fail safely. |
| B11 | Comments, mentions, versioned audit/outbox events, notifications/read state, private Supabase Realtime update hints; Queues when needed | B03, B05; implement audit/outbox early alongside domain writes | M | Committed changes produce durable events; retries deduplicate; reconnect refetches current permitted data; live hints reveal no private payloads. |
| B12 | Partner-facing routes and full request-to-delivery journey | B04–11 | M | Separate partner session can submit, discuss and accept only explicitly shared work. |
| B13 | Data catalog, CSV import, field/metric definitions and DBA questions | B03, B08, B11 | L | Versioned validated dataset and accepted definition support repeatable analysis. |
| B14 | Registered calculations, immutable analysis revisions, comparison and stale flags | B10, B13 | L | Same inputs reproduce same outputs; source changes create visible stale status and new revision. |
| B15 | Manual dashboard creation/editing, typed widget/metric definitions, revision/proposal validation and review, saved reports, scoped drilldown/exports | B08, B14; align schema with B20 contract | M/L | A human creates/publishes a working dashboard without any agent provider; revisions and exports preserve input versions, access and provenance. |
| B16 | Objectives, outcome measurement, transformation plan and change/risk registers | B06–11; B14 where analytical | M | Approved plan produces linked rollout tasks and baseline/actual outcome evidence. |
| B17 | Search, saved filters, project channels and reconnect behavior | B03, B11–12 | M | Hidden records never leak through search/counts; discussion survives reconnect and pagination. |
| B18 | One external integration with mapping, webhook/poll recovery and reconciliation | Relevant domain, B11 | L | Duplicate/missed events and provider outages do not duplicate or lose business state. |
| B19 | Release hardening, deployment migrations, backups/restore, observability, security regression | Begins at B00; final gate after selected scope | M/L | Staging passes full journey and failure tests; backup restore and deployment procedure are exercised. |
| B20 | Application contract readiness: context manifest and dashboard proposal schemas, event versioning, provenance fields, domain API boundary, disconnected-state behavior and contract tests | Define after B03; evolve alongside B08, B11, B13–15 | M | Contract document and validators describe a real future exchange; manual functionality passes; no provider, simulator, fake agent or generated activity is shipped. |
| B21 — deferred connection | Wire the application to one real externally developed assistance provider: scoped service identity, delivery/context, genuine status/questions, callback ingestion and result review | B20, B15, real provider and agreed contract | M | Real end-to-end dashboard assistance passes; no admin credentials or direct domain-table access are granted; invalid/stale/unauthorized results cannot apply. External agent implementation is not part of this package. |

### Stage 0 — establish a trustworthy baseline

Complete B00–B01 and establish CI immediately. Do not postpone repository reproducibility until feature integration. Add Supabase URL/publishable-key, server database/JWT, private-bucket and delivery configuration examples with no secret values. Pin Supabase client/CLI and backend dependencies through an appropriate lock strategy. Use the local Supabase stack for repeatable Auth/database/Storage tests; select any hosted project separately without assuming one already exists.

### Stage 1 — prove identity and isolation

Complete B02–B05 and the audit/outbox base from B11. Deliver sign-in, workspace administration, project directory/detail, membership, invitation acceptance and partner organization records. Prove boundaries with at least two internal workspaces and two partners before sensitive business data is introduced.

Begin B20 by writing the versioned actor/provenance, context-reference and event contracts. This is application schema/API work. Do not create an agent runtime or an artificial provider to exercise the screens.

### Stage 2 — ship one complete human collaboration loop

Complete B06–B12. Deliver request intake, versioned requirements, tasks, evidence, handoffs, human decisions and partner acceptance. Add comments and actionable notifications. All records persist and all actions work from separate sessions.

### Stage 3 — prove the DBA-to-BA workflow

Complete B13–B15 and the dashboard portion of B20. Ship one controlled import format, one reproducible calculation and manual dashboard creation/editing against typed definitions. Add data definitions, questions, analysis review, revision/proposal review, saved report and export. Verify source changes mark dependent outputs stale and do not alter approved history. Contract tests validate prospective external payloads without shipping a fake provider.

### Stage 4 — complete business transformation

Complete B16 and selected B17 features. Add measurable objectives, rollout plans, risks/changes and cross-project summaries. Avoid building separate disconnected systems for each job title; reuse the same project, work-item, evidence and decision records.

### Stage 5 — expand integration and operating scale

Complete B18 where justified and finish B19 for the intended release audience. Add enterprise identity, advanced retention controls, richer capacity planning or further integrations only after the complete core journey is reliable.

B21 is gated by availability of the real external system, not by an arbitrary final phase. It can run as soon as B15/B20 and that provider are ready. Until then, the product ships its useful manual workflows with assistance unavailable. The application team owns connection/authentication/validation/review; the external team owns agent execution.

## 13. Smallest credible demonstration

Use one NMC3 workspace, one capacity-expansion project, one partner representative, one BA, one DBA and one authorized decision-maker. Use a second partner account as a negative-access check. If the demo also includes delivery implementation, add a PO and engineer; identities may hold multiple functional roles.

1. Partner signs in and submits an expansion request with constraints and an attachment.
2. BA accepts the request and links requirements to the capacity-expansion project.
3. DBA imports a small documented dataset and publishes field/metric definitions.
4. BA runs the registered capacity/margin analysis with a specific forecast period and manually creates a dashboard using approved metrics and supported widgets.
5. Missing cost information creates a data question addressed to the DBA; answering it produces a new definition and recalculation.
6. BA sends a structured handoff containing the findings and exact supporting revisions.
7. Decision-maker approves the reviewed revision. A second concurrent attempt cannot alter the resolved decision.
8. PO creates linked delivery work; the responsible contributor attaches evidence and submits it for acceptance.
9. Partner sees only the shared deliverable, provides feedback or accepts it, and cannot see internal cost details.
10. Reload the browser and restart the application; the state and history remain. Log in as the other partner and verify isolation.

This demonstration needs real authentication, persistence, authorization and deterministic calculations. It does not need every page in the full roadmap. Combine the first release into functional areas for projects, work/requests, decisions/handoffs, data/reports, partners/members, and inbox/account settings. Use detail routes and tabs to avoid creating dozens of disconnected experiences.

For this milestone, demonstrate Supabase-backed manual dashboard editing and saved revisions. Assistance is unavailable until a real provider is connected. After B21, the additional genuine demonstration is request assistance → authorized context → actual provider proposal → validated preview → human apply/publish. Do not simulate any of those external-provider steps to claim integration is working.

If time forces a smaller collaboration-only demo, omit the analytical steps and demonstrate request → requirement → handoff → approval → delivery acceptance using documented evidence. Clearly state that data calculation remains unimplemented rather than presenting fabricated analysis as live.

## 14. Verification plan

The existing two tests establish only starter-route behavior. The following are proposed tests to implement with the corresponding features; they have not been run against functionality that does not yet exist.

| Area | Must-pass scenarios |
|---|---|
| Reproducibility | Fresh checkout contains all imported modules; dependencies install from committed manifests/locks; frontend typecheck/build and backend test suite run; documented environment/bootstrap steps are sufficient. |
| Migrations | Empty database to latest schema; representative older schema upgraded safely; failed migration recovery is documented; data constraints reject invalid same-tenant relationships. |
| Authentication | Missing, malformed, expired and invalid tokens/sessions denied; logout/refresh behave as chosen; user-controlled identity/role fields cannot override authenticated actor. |
| Supabase RLS and exposure | Exposed schemas have explicit grants and RLS; unauthorized direct Data API writes fail; invoker views preserve restrictions; transaction-local actor context cannot leak across pooled connections; privileged worker/migration credentials are never used as ordinary user authorization. |
| Supabase Auth integration | Verify issuer/audience/signature/expiry; Auth UUID maps to correct profile; SSR refresh is not cached across users; editable metadata cannot grant roles; removal is enforced from current membership even while an old access JWT is still valid. |
| Workspace boundaries | User in workspace A cannot read/change workspace B records by ID, nested route, list filter, search, attachment URL or export job. |
| Project boundaries | Nonmember cannot read a restricted project; cross-project references validate access; subprojects do not accidentally inherit broader access. |
| Partner boundaries | Partner A cannot enumerate Partner B; internal-only fields/comments/raw costs remain absent from response bodies, exports and notification previews. |
| Invitations | Accept once; exact retry; simultaneous accept; expired/revoked token; wrong signed-in email/identity; already-member state; resend invalidates old invitation as specified. |
| Membership changes | Remove/deactivate user; revoke share; suspend partner; ensure queued exports, future file URLs and active subscriptions no longer expose new data; prevent last-owner removal. |
| Requests | Draft edit/submit; request information and resubmit; accept/reject with history; duplicate conversion produces one project/link. |
| Requirements | Revision retained; accepted criteria linked to delivery work; changed scope does not silently alter accepted baseline. |
| Tasks | Invalid assignee, cyclic parent/dependency, forbidden transition, stale edit, blocked dependency, review return, evidence-based completion, reopen with reason. |
| Handoffs | Missing required context rejected; inaccessible evidence flagged; role-addressed handoff gets an accountable person; acknowledge versus complete distinguished; revision/return/reassignment history retained. |
| Approvals | Unauthorized/self-approval where disallowed; target changed since request; two simultaneous decisions; repeated submission; cancellation/delegation; audit contains exact approved revision. |
| Files | Oversized/disallowed file; spoofed MIME; incomplete upload; invalid storage key; unauthorized metadata/download; replaced version retained; orphan cleanup and failed finalization. |
| Comments | Parent access required; mention cannot grant access; edit/delete policy; internal discussion never exposed through shared target projection; user content rendered safely. |
| Notifications | Duplicate outbox delivery yields one inbox event; failed external delivery retries; read state persists; revoked target gives safe outcome; no notification for rolled-back transaction. |
| Data import | Duplicate rows/keys, malformed values, incompatible schema/units, missing cost, unknown partner, timezone mismatch, partial failures and repeated upload handled predictably. |
| Analysis | Known small fixture produces exact expected values; zero denominator/missing data handled; combined allocation exceeds capacity detected; input/metric revisions reproduce output. |
| Staleness | New dataset or changed definition flags dependent results; approved historical output remains unchanged; new result can be compared and separately reviewed. |
| Reports/exports | Filters and input revision agree with export; restricted rows/fields absent; spreadsheet-formula injection mitigated for CSV output; revoked access checked before job execution and download. |
| Manual dashboards | Create/edit/validate/publish without an assistance provider; approved metrics calculate correctly; unsupported widgets and executable definitions rejected; conflicts preserve newer work and original proposal. |
| Future integration contracts, tested in isolation now | Validate schema versions, bounded context, grant intersection, attribution and stale base revisions with test-only payloads; no fake provider process or agent data is bundled into the application or demo. |
| Real integration, only at B21 | Real service authentication; forged/replayed/out-of-order callbacks; duplicate result; cross-tenant reference; revoked requester/grant; expired/cancelled request; untrusted code/SQL/URL in proposal; only a human can apply/publish; genuine end-to-end provider response. |
| Disconnected assistance | No configured provider yields unavailable behavior with no pretend queue/progress/result; manual features remain usable; feature flags do not fabricate capability health. |
| Supabase Realtime | Private channel access, token refresh, reconnect resync, late membership removal and sanitized hints; authoritative data is fetched with current access; a client broadcast cannot complete a job or alter a record. |
| Search | Hidden names never appear in snippets, autocomplete, counts or facets; stale search index entries cannot bypass current access. |
| Workers | Crash/restart, duplicate delivery, timeout, lease expiry, retry exhaustion and cancellation; no indefinitely stuck work or duplicate business decisions. |
| Performance | Measure permission-scoped project/task/inbox lists, audit pagination and representative reports using realistic seeded volumes; bound request/import/export sizes and concurrency. |
| End-to-end | Partner request through internal review, task delivery and partner acceptance using distinct sessions; reload and server restart retain state. |
| Operations | API liveness versus dependency readiness; loss of database/storage/mail provider shows correct failure; logs include request IDs and exclude secrets; restore a backup into an isolated environment. |

Use unit tests for state/permission rules, local Supabase/Postgres integration tests for transactions/RLS/constraints, API tests for contracts and access, and a small number of browser tests for complete multi-user journeys. Include database policy tests under the Supabase test setup and run security/performance advisors before release. Avoid relying only on mocked databases for concurrency and tenant-isolation assertions. Include negative tests as first-class release gates.

Define explicit performance targets during implementation using anticipated data size and hosting; do not fabricate throughput or latency guarantees for the current starter. At minimum, prevent unbounded list responses, upload size, SQL execution duration, export volume and retries.

## 15. Production and operating requirements

**Configuration and environments.** Separate local Supabase development/test from hosted staging and production. Fail clearly on missing required configuration. Preserve example files without secrets. Use distinct database/storage environments and actual test identities. Seed fictional business records reproducibly and label them as demo data; never seed fake agents, conversations or generated results. Make resets explicitly nonproduction. Assistance defaults to unavailable until a real integration is configured; do not auto-create service credentials or contact an external provider.

**Deployment.** Document Next.js-to-FastAPI connectivity, Supabase Auth redirect allowlists/session refresh, private Storage configuration, Realtime authorization and Postgres connection pooling. Use Supabase CLI migration history as the release source of truth and run migrations once through a controlled job. Deploy schema changes compatible with both old and new application versions during a rolling release. FastAPI and its worker still need hosting; using Supabase does not automatically host the existing Python server. Edge Functions are optional narrow adapters if a concrete need arises; do not rewrite the backend into them or use them to build an agent runtime. Do not assume rolling back application code rolls back data safely.

**Health.** Keep liveness cheap; add a readiness check that reflects required dependencies with bounded checks. Avoid reporting the whole product healthy merely because `/api/health` returns a constant. Separately expose worker/import/delivery health to authorized operators.

**Observability.** Structured logs should carry request ID, action and safe tenant/resource references. Track API error rate and latency, database connection pressure, outbox age, job failures/retries, storage errors and notification delivery failures. Keep operational logs distinct from user-visible business audit history. Do not log invitation/session tokens, credentials or unrestricted document content.

**Input and output safety.** Validate all payloads server-side. Limit uploaded sizes/types and quarantine/scan files as appropriate before sharing. Escape/sanitize user-authored rich content. Use parameterized database queries. If later importing from user-supplied URLs, restrict outbound destinations and redirects to prevent access to internal services. For direct dataset connections, use explicitly approved read-only credentials/views, execution timeouts and bounded results.

**Rate and resource limits.** Limit sign-in/invitation attempts, repeated search, uploads, report execution and exports by suitable user/workspace boundaries. Return actionable retry information. Background work must have timeout/concurrency controls so one workspace cannot monopolize the service.

**Recovery.** Back up both relational data and document objects with a recovery procedure that preserves their relationships. Define acceptable data loss and recovery time with the owner before production. Exercise restore in an isolated environment. Document stuck-job recovery, expired credentials, webhook failures, missing objects and migration incidents.

**Supabase configuration verification.** Verify the selected project's actual backup coverage and object recovery separately; do not assume a database restore recreates uploaded files. Check current plan/service limits before choosing capacity. Configure email delivery for real invited users and test Auth redirects/recovery end to end; new free-tier projects using default email have template-customization restrictions. Use current supported SDK/runtime versions and modern TLS clients. No hosted Supabase resources need to be created for this planning task. [Auth email configuration change](https://supabase.com/changelog/46599-changes-to-email-template-customisation-on-free-tier), [HTTP TLS change](https://supabase.com/changelog/cbc-tls-ciphers-deprecated).

**Retention.** Decide how long to retain files, versions, conversations, audit history, invitation metadata and export artifacts. Time-limit generated downloads. Define deletion/correction behavior and how retained business decisions reference deleted or redacted evidence. Do not promise regulatory certification or compliance merely because audit logging exists.

**CI.** Add clean installation, frontend lint/typecheck/build, backend tests, schema migration checks and the selected end-to-end journey. Add dependency/secret checks appropriate to the deployment. A failing permission or migration test blocks release.

## 16. Decisions to settle before their dependent work

These are explicit assumptions and decision points, not reasons to delay the entire plan.

| Decision | Working recommendation | Needed before |
|---|---|---|
| Product authority | Supplied enterprise collaboration notes take precedence over the older consultant prototype description | Initial backlog acceptance |
| Workspace scope | Model multiple company workspaces; demonstrate one NMC3 workspace | Identity/schema design |
| Partner model | External organization plus invited people with project/resource grants | Invitation and membership implementation |
| Identity platform — decided | Supabase Auth; application profiles and memberships remain separate business records | Implement B02; choose actual sign-in methods and redirect configuration |
| Database/storage/realtime — decided | Supabase Postgres, private Storage and Realtime; Supabase migrations are authoritative | Configure B01/B03/B08/B11 |
| Application hosting — open | Retain Next.js, FastAPI and a server worker; choose their hosting separately from Supabase | Provisioning and deployment work |
| Approval authority | Start with one explicitly assigned reviewer, revision binding and defined self-approval policy | Approval implementation |
| Sharing default | Internal by default; partner material shared deliberately; no automatic access from reporting hierarchy | Resource serialization and grants |
| Project inheritance | No automatic broadening from parent to child | Subproject implementation |
| Data source for demo | One controlled CSV schema or approved read-only view; fictional seeded data | Dataset implementation |
| Margin/capacity rules | Agree actual units, periods, cost inclusion, forecast assumptions and aggregate allocation constraint | Calculation implementation |
| Notifications | Persisted in-app inbox first; external delivery only when configured and needed | Notification delivery work |
| Human chat/voice | Object-linked comments first, channels later; voice only if separately prioritized | Collaboration expansion |
| Agent-system boundary — decided | External system owns execution; this app owns scoped context, validated dashboard/artifact proposals, revisions, human review and persistence | Define B20 early; no agent development in this scope |
| First assistance capability — proposed | `dashboard.propose.v1`, using the same typed definition as manual dashboards | Agree with real external system owner before B21 |
| Integration activation — open dependency | Agree real provider contract/authentication/retention; keep unavailable until connected; no simulations | B21 only |
| Other integration order | Choose a concrete external system after the relevant local workflow is reliable | B18 |
| Retention/recovery | Choose according to intended audience and actual data, then test | External production rollout |
| Team/deadline | Size the selected stages after confirming available contributors and demo time | Calendar scheduling |

## 17. Definition of done

An in-scope feature is complete when its page and direct URL work; it reads and writes real persistent data; server permissions and domain rules are enforced; success/error/conflict/retry behavior works; relevant records are linked; history and notifications reflect committed actions; and an independent authorized session can observe the result after refresh.

The first usable product is complete when a partner can submit a request, internal people can agree requirements and hand off evidence, an authorized person can decide on the exact reviewed version, delivery work can be accepted, and the partner sees only explicitly shared results. Every step must survive refresh/restart and pass cross-tenant/partner negative tests.

The full intended application adds governed data definitions, reproducible analysis, manually editable dashboards, stale-result handling, measurable objectives and rollout outcomes. Its context, artifact, revision and event contracts support a later external agent system. Manual use does not depend on that system and no fake agent behavior is shipped. A collection of pages without these underlying records and rules does not satisfy the target.

Integration readiness is complete when the app's versioned contracts and validators, scoped permission model, provenance, review lifecycle and unavailable behavior are documented and tested. Actual assistance integration is complete only after a real external service produces a valid proposal and the application verifies, reviews and applies it end to end. Agent execution development remains outside this plan in both cases.

## 18. Evidence index

All repository links are pinned to the audited commit so later changes do not silently alter the basis of this plan.

- [Complete tracked repository](https://github.com/khangdoan514/hacktx-2026/tree/f0f0b1fd6467914e5b2424c49093f02283183e8b)
- [README and quick start](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/README.md)
- [Architecture and transport description](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/docs/architecture.md)
- [Backend router](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/backend/src/app/api/router.py)
- [Health route](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/backend/src/app/api/routes/health.py)
- [Greeting route](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/backend/src/app/api/routes/hello.py)
- [Application startup](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/backend/src/app/main.py)
- [Backend configuration](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/backend/src/app/core/config.py)
- [Backend dependency manifest](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/backend/pyproject.toml)
- [Current backend tests](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/backend/tests/test_api.py)
- [Database status](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/db/README.md)
- [Initial SQL schema](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/db/migrations/001_init.sql)
- [Shared application shell](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/frontend/src/components/AppShell.tsx)
- [Health component and missing helper import](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/frontend/src/components/BackendStatus.tsx)
- [API proxy configuration](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/frontend/next.config.ts)
- [TypeScript import mapping](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/frontend/tsconfig.json)
- [Root ignore rules](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/.gitignore)
- [Setup script](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/script/setup.sh)
- [Development script](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/script/dev.sh)
- [Prototype status and older product specification](https://github.com/khangdoan514/hacktx-2026/blob/f0f0b1fd6467914e5b2424c49093f02283183e8b/FRONTEND.md) — consulted for status/scope distinction only; excluded feature and visual-design work is not part of this plan.
- User-supplied “Pipeline Idea” attachment — source for human roles, project hierarchy, partner participation, structured handoffs, company objectives and the DBA-to-BA business-analysis scenario.
- User revision request — Supabase selected; future integration with a separately developed agent system required; dashboard creation assistance is an example; no agent development or fake agents.

Supabase architecture references checked for this revision: [changelog](https://supabase.com/changelog), [Next.js SSR](https://supabase.com/docs/guides/auth/server-side/creating-a-client?queryGroups=framework&framework=nextjs), [JWT claims verification](https://supabase.com/docs/reference/javascript/auth-getclaims), [RLS](https://supabase.com/docs/guides/database/postgres/row-level-security), [Data API security](https://supabase.com/docs/guides/api/securing-your-api), [private Storage](https://supabase.com/docs/guides/storage/buckets/fundamentals), [Realtime authorization](https://supabase.com/docs/guides/realtime/authorization), [Queues](https://supabase.com/docs/guides/queues). The integration protocol and application architecture above are proposed product contracts, not claims that Supabase supplies an agent system.
