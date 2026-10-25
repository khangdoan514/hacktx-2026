# Architecture

```
browser or CLI ──► Next.js or :8000
                     │
                     └── Authorization: Bearer ──► Go API (cmd/api :8000)
                                                     │
                                                     ├── Postgres schema app (employees and roles)
                                                     ├── Postgres schema nmc3 (business runs and worker queue)
                                                     ├── private document bytes
                                                     └── after the employee check, /agent-api/*
                                                           └── agents/cmd/nmc3-agent (127.0.0.1:8080)
                                                                 ├── its own Postgres database (schema nmc3)
                                                                 └── local OpenCode service (:49374)
```

- Business commands are under `/api/v1`. The business API also serves `/v1/agent-runs` for its dummy worker. Console and CLI calls to `/agent-api/*` stop at `cmd/api`, which checks the employee token and forwards to the private agent process for `/v1/agents`, `/v1/agent-runs`, run detail, events, `/v1/definitions`, `/v1/approvals`, and `/v1/extension-tools`. `GET /api/health` does not require a session. `./script/dev.sh` sets `NMC3_DATABASE_URL` to `AGENT_DATABASE_URL` for those stores. Run history stays on Supabase when `SUPABASE_SECRET_KEY` is set, and on that same agent database otherwise.
- Next.js proxies `/api/*` and `/agent-api/*` to the business API (`BACKEND_URL`, default `http://localhost:8000`). A CLI uses the same bearer token against `http://127.0.0.1:8000/agent-api/...`. The agent process listens on `127.0.0.1:8080` and, when `NMC3_INTERNAL_TOKEN` is set, rejects requests that did not come from `cmd/api`.
- The API verifies a Supabase RS256 or ES256 token against the configured issuer, audience, and JWKS. Local HS256 is available only when `ENVIRONMENT` is `local` or `test`, and never with the secret `dev-only-change-me`.
- Each employee transaction runs as `app_runtime` (`NOLOGIN`, `NOBYPASSRLS`) with transaction-local `app.user_id` and `app.user_email`. The role does not own the tables.
- Schema `app` holds the NMC2 organization, requests, specifications, approvals, handovers, catalog releases, and files. Schema `nmc3` holds runs, events, and the worker queue. Older business tables remain in place.
- Live updates are an authorized server-sent event stream plus polling. Convex is no longer in the tree.
- `AGENT_EXECUTION_MODE=dummy` is server configuration. A run stores that mode and the adapter version `dummy-1`. Depth 0 may call two allowlisted depth-1 specialists. Specialists cannot delegate. The vendor, catalog, and analysis fixtures remain. The product example is PR review over `agents/fixtures/pr-review/sample.diff`. Findings may cite only lines in that diff. Publishing a catalog release requires a passing evaluation and does not allocate hardware or grant a write tool.

## Verification boundary

Supabase CLI `start` needs Docker, which is not available here. Tests use plain Postgres. JWT verification was tested with a local JWKS fixture, not with a hosted Supabase project. Session refresh was not run. Document bytes are on local disk. Supabase Storage authorization was not run.

The record of the replaced FastAPI and Convex behavior is in [implemented/](implemented/). The direction is recorded in [adr/go-internal-platform.md](adr/go-internal-platform.md).

## Dashboard boundary

The console rail links Definitions, Tools, and Approvals beside the existing run pages. Those three call the agent API. Product routes stay on the business API. `frontend/src/lib/mock-data.ts` remains the compatibility shim the older pages import.
