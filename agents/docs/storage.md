# Agent storage decision and data specification

**Status:** selected platform direction, October 2026. No Supabase project or database is connected to the agent service yet. The `db/migrations/001_init.sql` file is a generic starter table, not the agent schema.

## Decision

Use **Supabase** for the agent system's data platform: its PostgreSQL database for definitions, runs, and events; Supabase Storage for files and artifacts; and Postgres `pgvector` for later semantic retrieval if RAG is needed. The Go service owns agent behavior and exposes a general-purpose API. The existing website uses that API rather than treating database tables as the agent API. A durable execution engine is selected separately in Pass 3; persisting run rows does not by itself provide safe worker replay.

Supabase's database is full PostgreSQL, and its Storage supports private buckets with access policies. Its vector features use the `pgvector` Postgres extension. These are complementary parts of the selected platform, not substitutes for model inference or durable orchestration. [Supabase database overview](https://supabase.com/docs/guides/database/overview), [Storage buckets](https://supabase.com/docs/guides/storage/buckets/fundamentals), [vector columns](https://supabase.com/docs/guides/ai/vector-columns)

The deployment model remains to be chosen. Supabase documents self-hosting for private infrastructure, but the self-hosted edition differs from managed Supabase and makes backups, high availability, maintenance, and monitoring the operator's responsibility. Record whether each environment uses managed or self-hosted Supabase before deployment. [Supabase self-hosting guide](https://supabase.com/docs/guides/self-hosting)

## What goes where

| Data | Store | Reason |
| --- | --- | --- |
| Agent/tool definitions and immutable versions | Supabase Postgres | Transactions, references, version queries |
| Runs, invocations, turns, tool attempts, approvals | Supabase Postgres | Consistent state and indexed inspection |
| Ordered run events and audit metadata | Supabase Postgres, append-only | Per-run sequencing and reconstruction |
| Large inputs/outputs, documents, generated files | Private Supabase Storage buckets with Postgres references | Avoid large row payloads; controlled access and retention |
| Provider credentials and tool secrets | Dedicated secret manager | Never store plaintext secrets in definitions, events, or artifacts |
| Long-term memory and organization knowledge | Supabase Postgres metadata, Storage objects, and `pgvector` embeddings as needed | Added in Pass 5 after access rules are defined |
| Durable scheduling/checkpoints | Execution engine selected in Pass 3 | Worker restart/replay semantics are separate from the product database |

Use private buckets for agent artifacts and knowledge documents. Store object keys, checksums, content types, tenant ownership, and retention state in Postgres; avoid public URLs for sensitive content. Supabase Storage uses RLS policies for access to private buckets. Its database backups contain Storage metadata, **not the object bytes**, so object backup and restore must be designed separately. Self-hosted Storage can use an S3-compatible backend. [Storage access control](https://supabase.com/docs/guides/storage/security/access-control), [backup guide](https://supabase.com/docs/guides/platform/backups), [self-hosted S3 guide](https://supabase.com/docs/guides/self-hosting/self-hosted-s3)

### Threads implementation

Threads uses `nmc3.threads` and `nmc3.thread_files` in the agent Postgres database. The thread index is scoped by tenant and principal; transcript snapshots are refreshed during SSE updates and at turn completion. Attachment metadata stays in Postgres while attachment bytes live in a private S3-compatible bucket. Local Compose uses an S3-compatible Silo service. Selected attachments are fetched from the bucket for a message and sent to OpenCode as native prompt attachments; they are not copied into the workspace. Text, PDF, PNG, JPEG, GIF, WebP, and common Office ZIP/XML documents can be inspected by the agent. PDFs and images require a model that accepts those inputs. Unsupported binary formats stay downloadable and the agent receives an explicit note rather than invented contents. Each uploaded file is limited to 20 MiB by the current OpenCode attachment API. Production can point the same adapter at Supabase Storage's S3 endpoint or another compatible service.

## RAG and embeddings, when added in Pass 5

Supabase/`pgvector` stores and searches embedding vectors; an embedding model or job generates them. The ingestion path is: authorized document upload to private Storage → extraction and chunking → embedding generation → chunk metadata and vector rows in Postgres → tenant- and grant-filtered retrieval → cited chunks passed into an agent's scoped context. Embedding generation and re-embedding on document updates are asynchronous jobs with retry and deletion handling. Supabase documents separate embedding generation and storage/query steps. [Vector columns](https://supabase.com/docs/guides/ai/vector-columns), [automatic embeddings](https://supabase.com/docs/guides/ai/automatic-embeddings)

Record `tenant_id`, source document/object key, chunk ordinal, text or protected text reference, embedding model/version, vector dimension, content hash, and timestamps. Use the same embedding model for indexed chunks and query vectors; model changes require a new index or re-embedding plan. Retrieval applies authorization **before** returning chunks, and each returned chunk carries source provenance. Start with measured queries; add vector indexes and hybrid keyword/vector search when corpus size and relevance testing justify them. [Supabase semantic search](https://supabase.com/docs/guides/ai/semantic-search), [hybrid search](https://supabase.com/docs/guides/ai/hybrid-search)

## Logical schema

The table list is a design contract, not a migration. Names and exact types are finalized when the owning pass is implemented. Use opaque IDs, UTC timestamps, foreign keys, and JSONB only for validated variable payloads. Store searchable state, version, identity, and timing fields as columns. Pass 2 may assign all records to one internal local tenant; Pass 4 makes tenant identity and enforcement mandatory before multi-organization use.

| Table | Minimum columns and constraints | First required |
| --- | --- | --- |
| `agent_definitions` | `id`, `tenant_id`, `name`, `lifecycle_state`, `created_by`, timestamps; unique tenant/name | Pass 2 |
| `agent_versions` | `agent_id`, `version`, canonical spec JSONB, schema version, content hash, published/retired timestamps; unique `(agent_id, version)`; published rows immutable | Pass 2 |
| `tool_definitions`, `tool_versions` | Same ID/version pattern, side-effect class, schemas, permission and credential references | Pass 2/6 |
| `runs` | `id`, tenant/principal, root agent ID/version, state, input/output references, error code, effective limits, usage, timestamps, request idempotency key | Pass 1 |
| `agent_invocations` | `id`, `run_id`, `parent_id`, agent ID/version, depth, state, input/output references, timestamps; parent belongs to same run | Pass 1 |
| `model_turns` | `id`, invocation ID, ordinal, provider/model, action kind, usage, cost estimate, state/error, timestamps; unique ordinal per invocation | Pass 1 |
| `tool_invocations` | `id`, invocation ID, tool ID/version, side-effect class, idempotency key, state/result reference, timestamps | Pass 1/4 |
| `attempts` | logical operation ID, attempt number, worker, state, error, started/finished timestamps; unique `(operation_id, attempt_number)` | Pass 3 |
| `run_events` | `run_id`, sequence, event type, actor/invocation, safe metadata JSONB, timestamp; primary key `(run_id, sequence)` | Pass 1 |
| `approval_requests`, `approval_decisions` | run/action, required policy, approver identity, state, expiry, decision and timestamps; one effective decision per request | Pass 4 |
| `artifacts` | tenant/run, object key, content type, byte size, checksum, classification, retention/deletion state | Pass 5 |
| `context_grants`, `memory_items` | tenant, owner/scope, purpose, policy, content reference, provenance, expiry | Pass 5 |
| `knowledge_documents`, `knowledge_chunks` | tenant, Storage object key, chunk/source metadata, embedding model/version and vector, indexing state | Pass 5 if RAG is built |

Do not store full prompts, secrets, or raw private context in `run_events`. Keep sensitive payloads in access-controlled records or objects and emit references plus safe summaries. Record definition/tool versions and policy revision on each run so historical decisions can be explained after a later publish.

## Transaction and query rules

- Insert a run and its initial event atomically. Require a unique request idempotency key per tenant/caller where the API supports retries.
- Give each run a monotonic event sequence allocated inside a transaction. `GET /events?after=N` returns a stable ordered slice and never relies on timestamps for order.
- Transition run/invocation state with an expected current state or revision check; terminal states cannot be overwritten. Record attempts separately from logical operations.
- Use unique constraints for published versions, run creation keys, operation idempotency keys, and attempt ordinals. A database constraint is the final guard against duplicate logical writes.
- Index by `(tenant_id, created_at)`, `(tenant_id, state, updated_at)`, `(run_id, sequence)`, parent invocation, and operation state. Add specialized indexes only after inspecting actual queries and load.
- If Temporal is selected, its history owns workflow replay while Supabase Postgres owns product definitions and inspectable run projections. Specify an idempotent projection/outbox strategy; do not assume a transaction spans Temporal and Postgres. Temporal documents durable event history and worker replay. [Temporal event history](https://docs.temporal.io/encyclopedia/event-history)

## Security and tenancy

Put agent tables in a non-exposed schema such as `nmc3`. Give the Go service a dedicated least-privilege database role. The website does not receive a service-role/secret key or a direct connection string. The Go API authenticates callers and enforces tenant, definition, run, and event access. Apply private-bucket policies to Storage objects. If any table is deliberately exposed through Supabase's Data API, configure grants **and** row-level security together; exposed tables without RLS can be accessed by roles with grants. [Supabase API security guide](https://supabase.com/docs/guides/api/securing-your-api), [RLS guide](https://supabase.com/docs/guides/database/postgres/row-level-security), [Storage access control](https://supabase.com/docs/guides/storage/security/access-control)

Pass 4 must test cross-tenant reads/writes at both API and database boundaries. Authorization claims should come from trusted identity/policy data, not editable user profile fields. Secrets use references and just-in-time resolution, with rotation and access logging.

## Delivery and verification

| Pass | Storage requirement and proof |
| --- | --- |
| 1 | Connect to Supabase Postgres with migrations for runs, invocations, turns, tool calls, and events. Verify a completed run remains inspectable after API restart; an interrupted run may require manual resolution. |
| 2 | Add persistent definition/version registry; publish/retire and version-pinning tests. |
| 3 | Add durable attempts/checkpoints and executor after a kill/restart proof; test safe resumption and duplicate prevention. |
| 4 | Add tenant/policy/approval/write records and access checks; test cross-tenant isolation, approval restart, and ambiguous writes. |
| 5–6 | Use Supabase Storage for artifacts/knowledge and add memory, optional pgvector retrieval, tool metadata, and isolated worker references; test retention, scoped retrieval, and credential boundaries. |
| 8 | Backup/restore Postgres **and** object bytes, migration rollback, retention jobs, load indexes, and operational recovery drills. |

Before implementation, record the Supabase project/deployment model, region, migration tool, Storage bucket policies, embedding model if RAG is selected, durable executor, identity provider, retention periods, and database **plus object** backup targets. Do not repurpose the placeholder `users` table as the agent schema.
