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

The custom Go execution engine (runtime, model adapters, tool gateway) was
removed in favor of OpenCode; the implementation remains in git history. See
[the OpenCode adoption plan](docs/opencode-adoption.md). The Go service
translates the versioned `/v1` API to the local OpenCode service, validates
requests and structured results against NMC3 schemas, and persists runs and
ordered events to Supabase. From `agents/`, run `go run ./cmd/nmc3-agent`.
It listens on `127.0.0.1:8080` by default. The FastAPI scaffold is not in the
agent request path.

## Local setup

From the repository root, `./script/dev.sh` runs `agents/script/setup.sh` and then starts this service. Setup creates the database `hacktx_nmc3_agent` (override with `NMC3_AGENT_DB` / `AGENT_DATABASE_URL`), applies the three SQL files below, installs OpenCode when the `opencode` command is missing, and starts its background service. Use that database rather than the business API database: both use a schema named `nmc3`, and the tables are not the same.

1. Apply [the agent schema migration](db/migrations/202610240001_nmc3_pass1.sql), [the service API migration](db/migrations/202610240002_nmc3_data_api.sql), and [the event-source migration](db/migrations/202610240003_nmc3_event_source.sql) to the selected database. The tables remain in the private `nmc3` schema. Hosted Supabase calls restricted RPCs through the Data API. Local setup applies the same files with `psql`.
2. For hosted Supabase, set `SUPABASE_URL` and the server-only `SUPABASE_SECRET_KEY` in your local environment and leave `NMC3_STORE` unset. Never put the secret in `NEXT_PUBLIC_` variables or commit it. For this repository's local Postgres, set `NMC3_STORE=postgres` and `DATABASE_URL` to `hacktx_nmc3_agent`. The service also needs the local OpenCode service: `OPENCODE_URL` (default `http://127.0.0.1:49374`) and its service password via `OPENCODE_SERVICE_PASSWORD` or the default registration file `~/.config/opencode/service.json`. From `agents/`, run `go run ./cmd/nmc3-agent`. It listens on `127.0.0.1:8080` by default.
3. For deterministic local runs, use the scripted test harness (`go test ./internal/opencode/`); it replays fixture sessions with no network or credentials. Definitions are loaded from `examples/definitions.json`. The sample request is `examples/vendor-comparison.json`.
4. On startup the service writes an OpenCode project into the session directory: `opencode.jsonc` plus `catalog/vendors.json`. The orchestrator may call only its allowlisted specialists. Specialists may read that catalog and cannot delegate. Shell, edit, and web tools are denied. The model comes from `OPENCODE_MODEL` (default `opencode/beam-free`) or per-run `model_id`. OpenCode's free models currently reject this HTTP service with `FreeTierError`; sign a provider in with `opencode auth login` before expecting a live run. OpenCode records session usage and cost.
5. From `frontend/`, `AGENT_API_URL` defaults to `http://127.0.0.1:8080`. Run `npm ci` and `npm run dev`, then open `/runs`. The website proxies `/agent-api/*` to this Go API.

Run `go test ./...` in `agents/`, and `npm run lint && npm run build` in `frontend/`. The deterministic adapter needs no provider credentials but still uses Supabase's Postgres database through the Go service. Optional direct Postgres tests use `NMC3_TEST_DATABASE_URL`.

Pass 1 persists completed run history and ordered events. An in-flight run interrupted by an API restart is **not** automatically resumed; automatic recovery belongs to Pass 3. This local API has no production authentication or tenant isolation and should use fictional data only.

## Guiding rules

- A depth-0 orchestrator may invoke authorized depth-1 specialists. Specialists cannot invoke other agents.
- Every agent invocation has explicit input, output, tool permissions, limits, and trace identity.
- The runtime, rather than a model prompt, enforces delegation and tool permissions.
- Checked-in fictional data and a deterministic adapter make the first pass testable; the configured provider path supplies live agent behavior.
