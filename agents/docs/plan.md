# Agent delivery roadmap

The complete [agent product specification](product-spec.md) needs **eight functional passes**. The detailed scope, acceptance tests, interfaces, and dependencies for each are in [pass-specs.md](pass-specs.md). These are capability gates, not calendar estimates.

| Pass | Outcome | Depends on |
| --- | --- | --- |
| 1. Functional core | Model-backed orchestrator, specialists, read-only tool, Supabase run history, API, events, minimal UI | Shared v1 contracts, already created |
| 2. Definition lifecycle | Draft, test, publish, version, retire through the website | 1 |
| 3. Durable runs | Attempts, checkpoints, cancellation, safe restart recovery | 1–2 |
| 4. Governance | Identity, tenant policy, secrets, approvals, safe writes | 2–3 |
| 5. Context | Scoped memory, knowledge, retrieval, provenance | 4 |
| 6. Integrations | MCP/HTTP/internal tools, Go extension SDK, isolated workers | 4; can overlap 5 |
| 7. Platform integration | Agent nodes in workflows and full application flows | 2–6 plus workflow/application workstreams |
| 8. Operations | Self-hosted deployment, scale, observability, measured targets | 3–7 |

The [first-pass spec](first-pass.md) is the immediate build target, and [parallel-work.md](parallel-work.md) splits it across coding agents. The Go API remains usable independently; the existing Next.js website gains capabilities in every relevant pass, with visual workflow features in Pass 7. The full NMC3 workflow engine is an adjacent workstream: Pass 7 integrates with its DAG, join, and approval capabilities; it does not place all workflow development inside `agents/`.

The [storage specification](storage.md) selects Supabase Postgres and Storage, with optional `pgvector` retrieval in Pass 5. Durable execution is a separate Pass 3 decision.

Later passes may overlap where their contracts are independent. A pass is complete only after its end-to-end acceptance checks pass and earlier behavior remains compatible.

All coding agents and the integrator follow the [testing and retest workflow](testing.md). A pass remains in progress until its required checks and integrated flow pass.
