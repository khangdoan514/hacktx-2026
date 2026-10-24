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

The Pass 1 runtime, adapters, read-only catalog gateway, Supabase API backed Postgres store, HTTP API, and website client are implemented. Live Supabase, provider, and browser acceptance must pass before calling the pass complete. The FastAPI scaffold is not in the agent request path.

## Local setup

1. Apply [the agent schema migration](db/migrations/202610240001_nmc3_pass1.sql) and [the service API migration](db/migrations/202610240002_nmc3_data_api.sql) to the selected Supabase project. The tables remain in the private `nmc3` schema; the Go service calls restricted RPCs through Supabase's Data API.
2. Set `SUPABASE_URL` and the server-only `SUPABASE_SECRET_KEY` in your local environment. Never put the secret in `NEXT_PUBLIC_` variables or commit it. From `agents/`, run `go run ./cmd/nmc3-agent`. It listens on `127.0.0.1:8080` by default. Direct Postgres remains available only with `NMC3_STORE=postgres` and `DATABASE_URL`.
3. For deterministic local runs, use the default `NMC3_MODEL_ADAPTER=deterministic`. Definitions and scripted actions are loaded from `examples/definitions.json` and `examples/deterministic-actions.json`. The sample request is `examples/vendor-comparison.json`; run `./script/replay.sh` to submit and inspect it.
4. For the live provider, set `NMC3_MODEL_ADAPTER=openrouter`, `OPENROUTER_API_KEY`, and optionally `OPENROUTER_MODEL` (defaults to the cheap `meta/muse-spark-1.3-contributor`, $0.10/$0.20 per million tokens). Any OpenRouter ID (free OSS, GPT, Claude) can also be chosen per run from the Runs page or `model_id`. The full option list is `examples/openrouter-models.json`. The endpoint may be overridden with `OPENROUTER_ENDPOINT` for testing.
5. From `frontend/`, set `AGENT_API_URL=http://127.0.0.1:8080`, run `npm ci` and `npm run dev`, then open `/runs`. The website proxies `/agent-api/*` to the Go API.

Run `go test ./...` in `agents/`, and `npm run lint && npm run build` in `frontend/`. The deterministic adapter needs no provider credentials but still uses Supabase's Postgres database through the Go service. Optional direct Postgres tests use `NMC3_TEST_DATABASE_URL`.

Pass 1 persists completed run history and ordered events. An in-flight run interrupted by an API restart is **not** automatically resumed; automatic recovery belongs to Pass 3. This local API has no production authentication or tenant isolation and should use fictional data only.

## Guiding rules

- A depth-0 orchestrator may invoke authorized depth-1 specialists. Specialists cannot invoke other agents.
- Every agent invocation has explicit input, output, tool permissions, limits, and trace identity.
- The runtime, rather than a model prompt, enforces delegation and tool permissions.
- Checked-in fictional data and a deterministic adapter make the first pass testable; the configured provider path supplies live agent behavior.
