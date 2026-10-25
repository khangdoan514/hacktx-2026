# NMC3 agent runtime

This directory is the home of the **general-purpose Go agent and orchestration service**. It exposes versioned APIs that can be used by different applications and workflows. The existing Next.js website in `frontend/` is the first integrated client.

## Product direction

- [End-to-end agent product specification](docs/product-spec.md)
- [Target architecture and execution lifecycle](docs/target-architecture.md)
- [Eight-pass product specifications](docs/pass-specs.md)
- [Storage decision and data model](docs/storage.md)
- [Testing and coding-agent workflow](docs/testing.md)

## First delivery slice

- [First pass scope and acceptance](docs/first-pass.md)
- [Agent architecture and contracts](docs/architecture.md)
- [Delivery roadmap](docs/plan.md)
- [Parallel work packages and agent handoffs](docs/parallel-work.md)
- [Shared Go and HTTP contracts v1](docs/contracts-v1.md)

## Current status

The agents service has one execution path: OpenCode. `cmd/nmc3-agent` creates
an OpenCode session, validates NMC3 schemas, and persists runs and ordered
events. There is no coordinator flag and no agents-side deterministic model
adapter. The custom engine (`internal/runtime/`, `internal/model/`,
`internal/tools/`) remains only in git history. See
[the OpenCode adoption plan](docs/opencode-adoption.md). From `agents/`, run
`go run ./cmd/nmc3-agent`. It listens on `127.0.0.1:8080` by default. The
FastAPI scaffold is not in the agent request path.

Contract tests in `internal/opencode/coordinator_test.go` use a scripted fake
OpenCode backend. They do not call the network, a provider, or a database.
The product application's scripted adapter (`internal/agents/dummy`, script
`examples/deterministic-actions.json`) is a separate path and is not this
service.

The first pass is not fully complete. This machine's CLI is `opencode
v2.1.4`. A live policy check against that binary has not been run.
`db/migrations/202610240003_nmc3_event_source.sql` is applied on the hosted
database.

## Local setup

1. Apply [the agent schema migration](db/migrations/202610240001_nmc3_pass1.sql), [the service API migration](db/migrations/202610240002_nmc3_data_api.sql), and [the event-source migration](db/migrations/202610240003_nmc3_event_source.sql) to the selected Supabase project. The tables remain in the private `nmc3` schema; the Go service calls restricted RPCs through Supabase's Data API.
2. Set `SUPABASE_URL` and the server-only `SUPABASE_SECRET_KEY` in your local environment. Never put the secret in `NEXT_PUBLIC_` variables or commit it. The service also needs the local OpenCode service: `OPENCODE_URL` (default `http://127.0.0.1:49374`) and its service password via `OPENCODE_SERVICE_PASSWORD` or the default registration file `~/.config/opencode/service.json`. From `agents/`, run `go run ./cmd/nmc3-agent`. It listens on `127.0.0.1:8080` by default and uses that directory as the OpenCode session location so `opencode.jsonc` is loaded. That file denies every action, allows only the named subagents, and exposes the fictional catalog through the local `catalog_search` MCP tool (`go run ./cmd/catalog-mcp`). Direct Postgres remains available only with `NMC3_STORE=postgres` and `DATABASE_URL`.
3. Contract tests do not need a running API, OpenCode, provider credentials, or a database. From `agents/`, `go test ./internal/opencode/` uses the scripted fake backend. Definitions for the service are loaded from `examples/definitions.json`. The sample live request is `examples/vendor-comparison.json`.
4. For live runs, the model comes from `OPENCODE_MODEL` (default `meta/muse-spark-1.3-contributor`, $0.10/$0.20 per million tokens) or per-run `model_id`. Any OpenRouter ID (OSS, GPT, Claude) may be supplied; the full option list is `examples/openrouter-models.json`. OpenCode records session usage and cost. The endpoint is the local OpenCode service, not OpenRouter directly.
5. From `frontend/`, set `AGENT_API_URL=http://127.0.0.1:8080`, run `npm ci` and `npm run dev`, then open `/runs`. The website proxies `/agent-api/*` to the Go API.

Run `go test ./...` in `agents/`, and `npm run lint && npm run build` in `frontend/`. Postgres tests skip without `NMC3_TEST_DATABASE_URL`. The scripted OpenCode tests need no provider credentials and no database. Live runs need the local OpenCode service and Supabase, or `NMC3_STORE=postgres` with `DATABASE_URL`.

Pass 1 persists completed run history and ordered events. An in-flight run interrupted by an API restart is **not** automatically resumed; automatic recovery belongs to Pass 3. Submitted limits must be positive. The wait deadline is enforced (`TIMEOUT` when `timeout_seconds` elapses). Token and cost on the run are copied from OpenCode session usage; crossing `max_tokens` or `max_cost_usd` does not stop the run. Turn, tool-call, delegation, and concurrency limits are not stop conditions in this service. This local API has no production authentication or tenant isolation and should use fictional data only.

## Guiding rules

- A depth-0 orchestrator may invoke authorized depth-1 specialists. Specialists cannot invoke other agents.
- Every agent invocation has explicit input, output, tool permissions, limits, and trace identity.
- `opencode.jsonc` enforces delegation and tool permissions. `experimental.subagent_depth` is 1, so a specialist cannot launch another agent. Specialists search the catalog with `catalog_search` and cannot read the fixture file, the shell, or `.env`. The Go service records a denied delegation or tool as a failure and does not create a child.
- Checked-in fictional data and the scripted fake OpenCode backend make contract tests repeatable. Live agent behavior comes from the local OpenCode service.
