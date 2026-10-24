# Architecture: Go application, Supabase, OpenCode

Status: target architecture, reconciled with main's OpenCode adoption. The diagram is not a deployment claim.

```text
Next.js website       CLI / SDK
       \                 /
        authenticated Go application API
        | identity, authorization, validation
        | request/specification/approval/catalog commands
        | evaluation, budget and trigger admission
        | safe history, event and artifact access
        +-- Supabase Auth / Postgres / private Storage
        +-- Go workers: outbox, execution dispatch, event persistence
        +-- Execution adapter
              +-- scripted dummy backend (interim)
              +-- OpenCode REST backend (target)
                    models / scoped tools / subagents
```

NMC3 owns business rules, employee permissions, immutable product versions, approval authority, execution admission, schema validation, policy translation, and durable product history. OpenCode owns the agent execution harness: model calls, permitted tools and delegation. Do not rebuild those internals under a new Go package name.

Supabase is the selected managed infrastructure. “All custom backend in Go” does not require replacing Supabase Auth/Postgres/Storage or reimplementing OpenCode in Go. Next.js remains TypeScript UI/rendering/proxy code; authorization and business mutations remain in Go. Convex custom functions conflict with the all-Go application boundary and are removed at cutover.

## Internal boundaries

Use a modular Go application before adding independent services. Logical packages cover platform/identity, organization, requests, approvals, capabilities, context, resources, evaluations, documents, notifications, audit, and execution integration. An API binary and worker binary can share packages while using separate credentials/privileges. No dashboard implementation package is required now.

Prefer explicit HTTP DTOs, domain commands, repositories and transactions over handlers containing scattered SQL and policy logic. Pin dependencies and generate/check API schemas. Keep one canonical contract model; don't maintain a website model and an incompatible SDK model.

Current agent code lives in a nested Go module. A root module may simplify the business migration, but relocation must preserve upstream changes, fixtures and import legality. Treat package layout as a reviewed migration, not a mandate to erase the existing service. The historical root-module plan is available for context but needs reconciliation.

## Runtime integration boundary

Define a narrow execution-backend adapter for start/cancel and execution observations, using the current service contracts or a deliberate versioned extension. NMC3 admits and persists the logical run before dispatch. Store the external OpenCode session mapping once available. Never return the OpenCode credential or expose its unrestricted server directly to clients.

Do not assume a local run row plus an OpenCode session creation is one transaction. Use a durable dispatch record and stable correlation/idempotency where supported. On uncertain creation or cancellation, reconcile before retrying. A timeout does not prove the external operation did nothing.

Persist ordered NMC3 events and authorized run snapshots in Supabase. OpenCode live events are not the replay store. Translate, deduplicate and redact them. Record source and source-event/session identifiers where available; allocate NMC3 sequence numbers atomically per run.

## Storage and identity boundaries

`app` holds business/organization records; `nmc3` holds executable definitions, run history, invocations/attempts/events or the mapped OpenCode representation. Keep schemas non-exposed by default. Scope access to the authenticated principal and approved resources, including history. The current service can use a Supabase RPC store or direct Postgres; choose deliberately and test the privileges of the selected path.

A service-role/secret credential bypasses normal user RLS. It must not be presented as equivalent to a non-owner runtime connection. For RPC access, restrict function grants and enforce explicit scoped service/application authorization; for direct SQL, use a least-privilege runtime role with transaction-local identity. Document which threat boundary each path enforces.

## Live delivery and compatibility

Go delivers authorized SSE or polling. The website, CLI and SDK consume the same persisted event cursor model. Disconnecting a viewer must not cancel the run. Losing permission must stop delivery and deny subsequent reads.

Keep the documented `/v1/agent-runs`, detail/events, and `/v1/agents` shapes compatible unless intentionally versioned. Main currently uses a website `/agent-api/*` proxy; preserve it while assessing any additional product proxy. Do not blindly add a competing `/v1/*` rewrite from an older prompt. Authenticate all aliases equally. Preserve frozen dashboard consumers and existing health/history contracts.

## Configuration and operation

Explicitly configure environment, Supabase project/issuer, allowed origins, database/store mode, storage buckets, OpenCode endpoint and execution mode. Avoid silent production defaults or fixed deployment-project fallbacks in reusable application configuration. Secrets stay server-only. Separate worker/admin permissions from request permissions. Provide safe health/readiness, request IDs, shutdown, bounded queues, retry/dead-letter visibility and documented failure behavior.
