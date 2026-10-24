# Data, migration lineage, files and recovery

Status: target data requirements. Main's actual store/migration paths and Cursor's business schema are different branches of work; reconcile before moving them.

## Ownership

| Area | Records |
| --- | --- |
| Organization | NMC2, divisions, teams, profiles, memberships, scoped grants |
| Demand/delivery | Requests and revisions, clarification threads, requirements/tasks, handovers |
| Capability | Catalog metadata, approved specifications, executable-version references, assignments, release gates |
| Governance | Decisions, policy/evidence versions, budgets, resource requests and allocation evidence |
| Context | Packages, source versions, grants, classifications, expiry and retention |
| Evaluation | Suites/cases, execution references, judgments, outcomes and evidence |
| Runtime | Runs, session mappings, attempts, invocation/tool observations as available, ordered events, usage provenance |
| Operations | Documents/versions, notification preferences/inbox, outbox, audit, idempotency |

Business rows belong in `app`; runtime rows in `nmc3`. Logical schema separation does not automatically enforce permissions. Keep sensitive schemas non-exposed and restrict DB/RPC access deliberately. Do not duplicate canonical executable definitions in both schemas.

## Modeling rules

Use columns for IDs, scope, actor, state, version, timestamps and frequently queried attributes; validated JSONB for variable specifications/results. Add foreign keys, same-scope constraints, uniqueness and indexes appropriate to list/filter paths. Immutable published versions and audit events are append-only for ordinary runtime identities.

Record source and adapter/backend provenance explicitly. An intended provider/model differs from the backend that actually executed a scripted run. Missing usage cannot default to measured zero. Use appropriate precise numeric representation for money and document units/currency.

## Migration reconciliation

Cursor's reviewed business migrations live in `supabase/migrations`; newer main's agent migrations live under `agents/db/migrations`. Inventory both ledgers, deployed versions, function privileges and migration owners. Adopt one documented execution order without replaying applied changes or assuming moving a file changes its deployment history.

Use focused ordered migrations after the latest reconciled schema, not the older prompt's fixed “one new migration” instruction. Generate migration identifiers through the configured tooling. Never rewrite applied migrations or reset a live database. Keep reversible application cutover where possible and document non-reversible data transformations.

Existing workspace IDs can remain organization scope identifiers while the UI becomes one-company. Do not remove isolation columns or constraints just because there is one current organization. Map old data intentionally; remove old partner/workspace-creation endpoints before assuming their records are harmless.

## Transaction boundaries

Business command plus state/revision mutation, decision, audit and outbox is one transaction. Run admission plus dispatch/outbox record is one transaction. External OpenCode calls and Storage byte transfer are not inside a distributed database transaction; use explicit pending/finalizing/reconciliation states and idempotent steps.

Set request identity transaction-locally on a checked-out connection. Test connection reuse, rollback and concurrency. Admin/migration credentials must not be the normal request role. Workers should have only the privileges needed for their tasks.

## Private documents

Initiate an authorized upload → allocate a private object key → transfer bounded bytes → verify object metadata/checksum/type/size → finalize metadata → attach a version reference. Downloads recheck access before issuing a short-lived scoped URL or streaming content. Document the authorization window of already issued URLs.

Use quotas, streaming limits, allowed types and appropriate content checks. Clean abandoned uploads; handle object-written/database-commit-failed and the reverse without exposing incomplete artifacts. Never use a client-supplied arbitrary storage key as authority. Object references and filenames can themselves be sensitive.

## Backup and retention

Test database restoration and private object-byte restoration separately. Preserve enough audit/provenance to explain decisions within approved retention rules, without retaining every raw secret or prompt. Expired/revoked sources should block new access/dispatch while historical records remain appropriately redacted and accountable. Final retention periods are a company policy decision, not invented here.
