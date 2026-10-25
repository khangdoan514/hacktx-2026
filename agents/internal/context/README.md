# Scoped context

Pass 5 context resolver for one agent invocation. It assembles step, run, agent-memory, and organization fragments, records provenance, and keeps a snapshot so a later grant revocation does not erase the explanation of a historical run.

This package does not import `pgvector` or Supabase. Organization retrieval is a `Retriever` interface. `StaticRetriever` is the in-memory fake. Large bodies go through `ObjectStore`; `MemoryObjectStore` is the in-memory stand-in for private object storage.

Import it with an alias:

```go
agentctx "github.com/khangdoan514/hacktx-2026/agents/internal/context"
```

## Invariants

- A fragment is published only when the pinned definition grant and an active runtime grant agree on tenant, principal, agent, scope, resource, and purpose.
- The grant is checked before a store or retriever is read, and again before loaded text is copied into the bundle. Revoking a grant during retrieval drops those hits.
- A specialist bundle is built by `Forward`. It reloads selected run-artifact IDs from the store. It does not copy the orchestrator's step text, unselected artifacts, memory, organization hits, or tool allowlist.
- Depth 1 cannot forward again.
- Every fragment has `Trust: untrusted`. Retrieved text, step text, artifacts, and memory are data. They cannot add a tool, grant, or scope.
- `Bundle.AllowedTools` is a copy of the allowlist the coordinator passed in. `Bundle.ToolAllowed` reads only that list.
- A denied scope adds a `Denial` with the code and the identifier the caller already supplied. It does not add titles, source URIs, document IDs, or body text. A collection that was not requested is omitted entirely.
- Cross-tenant reads do not return the other tenant's bytes. An admin role does not bypass a missing grant.
- Snapshots keep fragment text until `RetainUntil`. After `DeleteExpired`, text, document title, and source URI are cleared. Scope, source ID, grant ID, content hash, and collection time remain.
- Deleting memory or an artifact hides it from new reads. Snapshots already taken keep their copy until retention redacts them.
- Version 0 definition grants allow only step and run. Version 0 snapshots may omit `memory_refs` and still inspect.

The coordinator must pass the pinned agent ID, depth, and allowlist. This package does not authenticate that caller. Model text must not be parsed into those fields.

Only fragment text is model-facing input. Denials, grant IDs, and the allowlist are for the runtime and for inspectors. The tool gateway must keep authorizing from the invocation allowlist.

## Grants

Runtime grants are installed with `PutGrant` by a principal with role `admin` and revoked with `RevokeGrant`. Revocation is idempotent and blocks new reads. `GetGrant` shows the revoked timestamp.

Definition grants travel on the resolve request as `VersionedGrants` at `GrantSchemaVersion` (1). An empty definition resource matches any runtime resource of that scope. A set resource pins one invocation, run, agent, or collection.

| Scope | Resource | Read purpose | Write / delete |
| --- | --- | --- | --- |
| `step` | invocation ID | `resolve` | — |
| `run` | run ID | `resolve` | `write_artifact`, `delete_artifact` |
| `agent_memory` | agent ID | `read_memory` | `write_memory`, `delete_memory` |
| `organization` | collection ID | `retrieve` | — |

`inspect` on a run grant lets that principal read snapshots for that agent and run. Roles `inspector`, `admin`, and `operator` can inspect any snapshot in their tenant. Role `operator` or `admin` can run `DeleteExpired` for their tenant only.

## Forwarding

`Resolve` at depth 0 returns every live run artifact the grant allows. `Forward` accepts an orchestrator bundle (`Depth == 0`, same tenant) and a list of artifact IDs. The specialist receives an ID only when it is both in that list and already present on the parent bundle, and only when the specialist's own run grant still authorizes the load. Artifact bodies above `MaxInlineBytes` (4096) are stored under an object key. The fragment provenance carries the key, checksum, and size. A checksum mismatch or a deleted object produces a denial and does not copy the bytes.

## Compatibility

`BundleSchemaVersion` is 1. `PutSnapshot` accepts version 0 records that have no memory references. This package does not change `contracts.RunContext`. The integrator can point a run at `Bundle.SnapshotID` and leave older runs without a memory reference readable through `Inspect`.

## Checks not run

No Supabase, Storage, or `pgvector` adapter is included. No HTTP route, website flow, or coordinator wiring was added. Live retrieval and cross-package API tests were not run.
