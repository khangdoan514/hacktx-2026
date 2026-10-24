# Parallel implementation work

This is the handoff plan for multiple coding agents building the NMC3 agent first pass. Assign **one agent per work package** and one integrator. A work package is done when its code, focused tests, and usage notes are merged against the shared contracts.

Every package follows the [build → test → fix → retest workflow](testing.md) before handoff. The integrator runs the joined API and website acceptance gate before calling the pass complete.

For the intended end-to-end agent product beyond this first pass, read [product-spec.md](product-spec.md) and [target-architecture.md](target-architecture.md). Package assignments here cover only the first slice.

## Start gate: shared contracts v1

The Go module, shared `internal/contracts` package, and API fixtures are now defined in [contracts-v1.md](contracts-v1.md). Package owners should use these shapes for the first implementation pass:

- `AgentDefinition`, `AgentInvocation`, `ToolDefinition`, `ToolInvocation`, `RunContext`, and `RunEvent` from [architecture.md](architecture.md).
- `ModelAdapter`, `Tool`, `RunStore`, and `EventSink` interfaces. Context cancellation must be accepted by any operation that can block.
- Invocation states and typed error codes from [architecture.md](architecture.md), including depth and authorization errors.
- The JSON request/response and event shapes for the four draft `/v1` endpoints.

The shared package compiles and its fixture test validates the wire examples. Package owners may now start in parallel. An interface change needs an integrator decision and an update to the fixtures before dependent packages rebase.

## Work packages

| Package | Owns | Depends on | Deliverable |
| --- | --- | --- | --- |
| A. Runtime coordinator | `agents/internal/runtime/` | Shared contracts | Invocation lifecycle, depth-1 delegation, allowlists, budgets, cancellation, bounded concurrency, structured output validation. |
| B. Model adapters | `agents/internal/model/` | Shared contracts | Provider-backed adapter and deterministic test adapter, structured action parsing, usage/error reporting. |
| C. Tool gateway and data | `agents/internal/tools/`, `agents/fixtures/` | Shared contracts | Tool registry, schema and allowlist checks, read-only fictional catalog tool, fixture data. |
| D. Run store and events | `agents/internal/store/`, agent schema migrations | Shared contracts; Supabase project config | Supabase Postgres run/event store, ordered append/read, concurrency safety, completed-run persistence across API restart. |
| E. HTTP API | `agents/internal/httpapi/`, `agents/cmd/nmc3-agent/` | Shared contracts; store interface | Run submission/status/events/agent listing endpoints, request validation, typed errors, server configuration. Can use fakes until runtime is ready. |
| F. Examples and application | `agents/examples/`, `agents/script/`, and the Runs inspector only | API fixture; real API for final integration | Sample request, replay script, and the Runs inspector. Mock responses are acceptable until E is integrated. Package F does not own all of `frontend/`. |

Package A should **not** implement model adapters, tools, storage, or HTTP handlers. Package E should call the coordinator interface, not duplicate runtime rules. Package F should not implement a second agent engine in the frontend or FastAPI service.

## Integration order

```text
Shared contracts + API fixture
    ├── A runtime ────────────────┐
    ├── B model adapter ──────────┤
    ├── C tool gateway ───────────┤──> integrator wires end-to-end Go service
    ├── D run store/events ───────┤
    ├── E HTTP API with fakes ────┘
    └── F UI with mock API ────────────────> UI switches to running service
```

The integrator owns wiring between packages, cross-package tests, and the final browser flow. Do not make every package owner edit a shared `main.go`; package E owns the entrypoint and the integrator makes the final wiring change there. Keep package APIs narrow and avoid import cycles: shared contracts have no imports of implementation packages; runtime depends on interfaces; implementations do not import runtime.

## Acceptance by package

**A — Runtime:** A depth-0 orchestrator can invoke two allowlisted depth-1 specialists concurrently. A depth-1 delegation attempt, unknown target, exhausted budget, and cancellation each produce the expected typed outcome and event. Final outputs are schema checked.

**B — Model:** Both adapters return structured actions through `ModelAdapter`, respect cancellation, and cannot directly call tools or specialists. The provider adapter reports usage and typed errors; the deterministic adapter returns reproducible fixture actions. Credentials stay out of source control.

**C — Tools:** Allowed catalog queries return schema-valid records. Disallowed tools and malformed inputs never reach a handler. Tool failures and timeouts are visible as structured outcomes. Fixture data is fictional and checked in.

**D — Store:** Concurrent event writes get unique increasing sequence numbers per run. A reader can reconstruct current run state and invocation history. A completed run remains readable after the API restarts; an interrupted run is marked clearly until Pass 3 recovery exists.

**E — API:** The example request and responses match the frozen fixture. Invalid requests and missing runs have consistent HTTP codes and typed error bodies. The API works against fake coordinator/store implementations before integration.

**F — Application:** A user can start a run, inspect the invocation tree and ordered events, and see a denied-delegation result. The UI clearly distinguishes loading, success, and failure. The replay script works without the UI.

## Shared rules for coding agents

1. Work only in the owned paths unless the integrator approves a contract change. Add tests next to owned code.
2. Read [first-pass.md](first-pass.md) and [architecture.md](architecture.md) first. Treat their first pass boundaries as scope limits.
3. Record assumptions and interface needs in the package handoff; do not silently add a second version of a shared type.
4. Make each package compile with fakes or stubs while other packages are in progress.
5. Send the integrator the exported symbols, test command/result, negative cases, checks not run, and any unresolved integration issue at handoff. A failed required check keeps the package in progress.

## Suggested assignment prompts

Use one of these with a coding agent. Include the current shared-contract revision in its context.

> Implement package A from `agents/docs/parallel-work.md`. Work in `agents/internal/runtime/`. Use the frozen shared contracts and focus on depth-1 orchestration, limits, cancellation, and tests. Do not edit other package paths. Report exported APIs and integration needs.

> Implement package B from `agents/docs/parallel-work.md`. Work in `agents/internal/model/`. Build one provider-backed adapter and a deterministic test adapter against the frozen `ModelAdapter` interface, with structured actions, cancellation, usage, typed errors, and tests. Keep credentials out of source control. Report exported APIs and integration needs.

> Implement package C from `agents/docs/parallel-work.md`. Work in `agents/internal/tools/` and `agents/fixtures/`. Build the authorized read-only tool gateway and fictional catalog fixture with tests. Report exported APIs and integration needs.

> Implement package D from `agents/docs/parallel-work.md`. Own `agents/internal/store/` and the agent schema migrations. Build a Supabase Postgres-backed run/event store with concurrency and API-restart persistence tests against the shared contracts. Report exported APIs and integration needs.

> Implement package E from `agents/docs/parallel-work.md`. Work in `agents/internal/httpapi/` and `agents/cmd/nmc3-agent/`. Implement the frozen `/v1` HTTP contract against fakes first, with tests. Report exported APIs and integration needs.

> Implement package F from `agents/docs/parallel-work.md`. Work in `agents/examples/`, `agents/script/`, and the Runs inspector. Build the sample run and replay script against the frozen mock API fixture; switch to the live API at integration. Report any contract mismatch.

## Frontend ownership proposal

This section is a proposal until the other team accepts it. It is not an agreement, and it does not move files by itself.

Package F does not own the whole `frontend/` tree. The dashboard currently in progress stays with that work:

- `frontend/src/app/(dashboard)/`
- `frontend/src/app/(product)/w/[w]/projects/[p]/dashboards/`
- `frontend/src/components/AppShell.tsx`
- `frontend/src/theme/theme.ts`
- `frontend/src/components/PageHeader.tsx`
- `frontend/src/components/StatusTag.tsx`
- `frontend/src/components/WorkflowTile.tsx`
- `frontend/src/components/BackendStatus.tsx`
- `frontend/src/lib/mock-data.ts`
- `frontend/src/app/layout.tsx`
- `frontend/src/app/providers.tsx`
- `FRONTEND.md`, `docs/frontend-design.md`, and `docs/design/`

Shared files that both sides still have to keep building: `frontend/package.json`, `frontend/package-lock.json`, `frontend/src/lib/api.ts`, `frontend/src/lib/mock-data.ts`, and `frontend/next.config.ts`. `mock-data.ts` stays a shim while those dashboard pages import it. `apiGet("/health")` must keep fetching `/api/health`. The existing `/api/:path*` proxy stays in place.
