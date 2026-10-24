# Second Cursor prompt: CLI/SDK/website vision and current-runtime correction

Apply this as a follow-up to the current NMC3 implementation work. Continue existing authorized progress; do not restart from scratch. This prompt overrides conflicting older instructions about the runtime and dashboards.

## Full context

NMC3 is exclusively NMC2's internal platform. NMC2 offers GPU as a service, with Hardware and Software divisions and an AI team within Software. The core collaboration example is a team asking Software/AI for a pull-request/code-review agent. People can hold multiple functions, such as PM and BA. External customers/partners do not get portal access in this scope.

We are moving to a **CLI / SDK / website stack**. CLI and SDK serve developers. The website serves BAs, PMs and other less technical users. These are interfaces to one system, not three implementations. The central agent-building concerns are **evaluation, triggers, budget, guardrails, prompts and observation**.

Keep the internal request and handover workflow, but treat it as an entry point into this complete lifecycle:

**Request → configure → evaluate → approve → publish → trigger/run → observe → revise.**

## What changes in your work

1. Make the Go API independently usable without browser cookies/state or UI-only business decisions. Website sessions and developer credentials may differ in acquisition, but every object/action is governed by the same current employee/resource policy.
2. Define stable versioned schemas, typed safe errors, pagination, revision conflicts, idempotency and replayable event cursors. Keep one canonical executable definition and version model.
3. Implement evaluation suites/results/release gates; trigger intent vs activation; approved budgets and accurate usage; executable guardrails; immutable prompt versions; and persisted inspectable run/evidence history.
4. Ensure website commands and future SDK/CLI commands reach the same services. A developer cannot bypass review, self-approve, forge attribution or access a broader context set merely by using a terminal.
5. Provide API-only integration tests demonstrating the lifecycle and equivalent denied cases. Website tests prove usability; API tests prove the contract is client-independent.
6. Prepare a documented developer-auth design. Do not distribute database/service keys, assume copying a browser token is the final login flow, or invent unsupported identity-provider features.
7. Keep the SDK thin and plan safe retry/cancellation/event iteration. Do not duplicate policy logic in client libraries. CLI/SDK implementation remains separate unless explicitly assigned; this work must leave them a usable contract.

## Preserve the selected infrastructure and current main

All **NMC3-owned custom application/backend code** is Go. Next.js remains frontend. Supabase Auth, Postgres and private Storage remain selected. Remove Python business runtime and Convex after verified Go cutover.

Main has moved agent execution to **OpenCode** and removed the custom runtime/model/tool engine. The user explicitly chose to follow that direction. Do not rebuild the removed engine to satisfy an older prompt. Keep Go responsible for identity, policy admission, business rules, schemas, persistence, integration and observability; OpenCode supplies the execution harness.

Use a **scripted dummy execution adapter** at the backend integration seam in the meantime. Code, developer metadata and persisted provenance must identify its mode/version. Do not add dummy badges to the website. Do not invent real provider usage, live code-review posts, GPU allocation or successful external actions. Do not silently substitute dummy output after a live provider fails.

Preserve current contracts, store/history implementation, migrations and proxy consumers. Re-read current main; older exact signatures and source paths may be stale. Coordinate any module or contract move atomically rather than copying incompatible types into a second package.

## Dashboard freeze—explicit override

**No dashboard work in this phase**, including product dashboard routes, dashboard-assistance agents, proposals, apply/publish flows or dashboard wiring. Preserve existing dashboard files and behavior and the exact protected paths in `chatgpt-docs/scope-and-ownership.md`. Shared dependency/provider/API-helper/rewrite changes must not break the frozen dashboard. Observation can be implemented through the API and permitted non-dashboard product screens; the vision does not reopen dashboard scope.

## Acceptance changes

- A developer-compatible HTTP client can perform permitted configuration/test/invocation/inspection operations against the same records the website displays.
- Unauthorized, revoked, stale-version, self-approval and foreign-target attempts fail equally across clients.
- Evaluation evidence pins the definition, prompt, policy, sources and execution backend used. Scripted results are not claimed as real model-quality measurements.
- Manual/API triggers work; unimplemented webhooks/schedules remain inactive. Future activation needs signed/authorized/idempotent delivery.
- Budgets distinguish requested, approved, reserved and measured values. Unknown usage is not zero. Required hard limits must be enforced at the actual runtime boundary before claiming support.
- OpenCode integration tests verify permissions, isolation, cancellation, usage and events; scripted tests do not stand in for those guarantees.
- Revocation blocks new reads/runs/decisions and stops unauthorized event delivery.
- Completed run history survives restart; ambiguous or interrupted work is reconciled or reported accurately.
- The dashboard remains untouched and functional.

Update your active plan and architecture documentation to reflect this follow-up. Use `chatgpt-docs/` current documents as the detailed reference; archived prompts are historical. Continue independent work where external integrations are unavailable, but report unverified integration checks rather than inventing success.
