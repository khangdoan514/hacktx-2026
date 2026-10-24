# Delivery plan and acceptance matrix

This is an implementation plan, not a report of completed features. Preserve the dashboard freeze and current OpenCode ownership throughout.

## Phase 0: reconcile current work

Fetch latest main and workstream branch. Inventory code, contracts, migrations, active frontend ownership and current runtime adapter work. Record retain/replace/remove decisions and merge main without resetting other changes. Do not recreate deleted engine packages. Confirm the canonical contract package and store/migration path before moving files.

Gate: the implementation plan matches the current tree; frozen paths and shared compatibility dependencies are explicit; the three historical security regression scenarios are captured.

## Phase 1: Go application and secure identity

Implement/configure Go API and workers, safe errors, database transactions, Supabase employee identity, current-grant checks, audit/outbox and private-file boundary. Port retained Python behavior and negative security tests. Fail closed on invalid production configuration. Establish supported endpoint/proxy compatibility without modifying frozen dashboard consumers.

Gate: valid employee access succeeds; invalid/default-secret identity, revoked membership and cross-scope target mutation fail. Database identity survives rollback/reuse tests without leaking between users.

## Phase 2: internal capability workflow

Provision NMC2/Hardware/Software/AI idempotently. Replace workspace/public-partner access with internal team navigation. Implement request drafts, submission, intake, clarification, spec revisions, scoped reviews and handover. Keep delivery tasks beneath the capability workflow.

Gate: multiple employee identities complete a request through acknowledgment; wrong reviewer, self-approval, stale revision and bypass completion commands fail; retired partner/workspace-creation endpoints are inaccessible.

## Phase 3: API-ready core and interim execution

Document stable contracts usable by all clients. Add the bounded scripted adapter at the execution seam, persisted runs/events, PR-review fixtures, cancellation/failure paths and evaluator evidence. Keep implementation provenance in code/developer records. Do not claim live runtime enforcement from scripted results.

Gate: API-only and website flows refer to the same records; permitted run history is inspectable after restart; denied scope, invalid output, deadline, budget scenario and interruption are accurately represented.

## Phase 4: OpenCode integration and release controls

Coordinate the OpenCode REST adapter with the agent owner. Validate session/permission/usage/event capabilities against the installed runtime. Bind actual sessions to NMC3 identity/version/policy, persist safe history, and reconcile uncertain submission. Complete evaluation gates, catalog release, assignments, budget/resource reviews and support ownership.

Gate: actual runtime controls meet the promised envelope; unsupported controls block live use; version changes invalidate affected evidence. Publishing does not allocate hardware or authorize writes implicitly. Dashboard implementation remains deferred.

## Phase 5: cutover and cleanup

Point retained product operations at Go. Remove Python backend/runtime configuration and Convex only after replacement checks. Update scripts, dependencies, README/env docs and eventual implementation PR description. Keep unrelated ML work. Disable obsolete API paths, retire data only after dependency review, and preserve frozen dashboard behavior.

Gate: clean documented setup, migrations and browser/API journeys work; no NMC3 runtime dependency on Python/Convex remains; known external checks are reported honestly. No incidental production deployment or implementation PR merge is authorized by this plan.

## Required tests

| Area | Proof |
| --- | --- |
| Go | Build/format/vet, meaningful unit/integration tests, race checks for shared counters/leases |
| Auth | Supabase tokens, expiry, refresh/logout, unknown keys, invalid claims, development issuer rejection |
| Grants | Revoked employee loses project/file/run/decision/event access despite otherwise valid token |
| Scope | Foreign IDs cannot cross organization/project/team/resource policy |
| Decisions | Exact revision, authorized reviewer, no self-approval, expiry, one concurrent result, no bypass endpoint |
| DB | Least-privilege role/RPC policy, pooled identity isolation, scoped privileged functions |
| Contracts | Existing fixture compatibility or deliberate versioning, schema rejection, safe typed errors |
| Scripted mode | No live calls/writes, deterministic scenarios, truthful provenance, no fabricated provider measurements |
| OpenCode | Real permission/depth/model/tool/cancellation/limit behavior, output validation, isolated session access |
| Events | Atomic creation, unique sequence, safe replay, deduplication, reconnect/gap handling and revocation |
| External ambiguity | Submission timeout does not create blind duplicate execution; reconcile/flag uncertainty |
| Files | Authorization, streaming limits, metadata/checksum validation, short-lived links and orphan cleanup |
| Budgets | Concurrent reservations/accounting, unknown telemetry, exhaustion before prohibited dispatch |
| Clients | Equivalent authorized API/browser records and equivalent denials; CLI/SDK contract smoke coverage |
| UI | Actual forms and selected-record views; protected dashboard remains unchanged and functional |
| Recovery | Completed history survives restart; abandoned execution is never marked successful automatically |

## Test environment discipline

Use fictional data and a dedicated disposable test database with explicit safeguards. The historical Python suite drops/rebuilds a named database; do not run it against an application cluster by accident. A plain Postgres run does not verify managed Supabase Auth/refresh/Storage. Record actual server version, environment, commands/results and checks not run. Do not broaden test claims to cover untested integrations.

## Deferred work

Dashboard editing and assistance; full arbitrary workflow graphs; unrestricted external writes; GPU provisioning; live inventory/billing integration; automatic durable recovery; broad long-term memory/RAG; external partner accounts; CLI/SDK implementations beyond assigned scope. Reopen these with explicit requirements rather than shipping working-looking controls.
