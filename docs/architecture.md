# Architecture

```
browser ──► Next.js
              │
              ├── (dashboard) existing shell, proxied /api/health and /agent-api
              └── NMC2 product routes at /sign-in, /org, and /requests
                    │
                    └── employee JWT ──► Go API (cmd/api)
                                           │
                                           ├── Postgres schema app
                                           ├── Postgres schema nmc3
                                           ├── private document bytes
                                           └── cmd/worker leases agent runs
```

- Business commands are under `/api/v1`. Agent inspection is under `/v1/agent-runs`, `/v1/agent-runs/{id}/events`, and `/v1/agents`. Both require the same employee session. `GET /api/health` does not.
- Next.js proxies `/api/*` to the Go API and `/v1/*` to the same API. The dashboard's `/agent-api/*` proxy is unchanged.
- The API verifies a Supabase RS256 or ES256 token against the configured issuer, audience, and JWKS. Local HS256 is available only when `ENVIRONMENT` is `local` or `test`, and never with the secret `dev-only-change-me`.
- Each employee transaction runs as `app_runtime` (`NOLOGIN`, `NOBYPASSRLS`) with transaction-local `app.user_id` and `app.user_email`. The role does not own the tables.
- Schema `app` holds the NMC2 organization, requests, specifications, approvals, handovers, catalog releases, and files. Schema `nmc3` holds runs, events, and the worker queue. Older business tables remain in place.
- Live updates are an authorized server-sent event stream plus polling. Convex is no longer in the tree.
- `AGENT_EXECUTION_MODE=dummy` is server configuration. A run stores that mode and the adapter version `dummy-1`. Depth 0 may call two allowlisted depth-1 specialists. Specialists cannot delegate. The vendor, catalog, and analysis fixtures remain. The product example is PR review over `agents/fixtures/pr-review/sample.diff`. Findings may cite only lines in that diff. Publishing a catalog release requires a passing evaluation and does not allocate hardware or grant a write tool.

## Verification boundary

Supabase CLI `start` needs Docker, which is not available here. Tests use plain Postgres. JWT verification was tested with a local JWKS fixture, not with a hosted Supabase project. Session refresh was not run. Document bytes are on local disk. Supabase Storage authorization was not run.

The record of the replaced FastAPI and Convex behavior is in [implemented/](implemented/). The direction is recorded in [adr/go-internal-platform.md](adr/go-internal-platform.md).

## Dashboard boundary

This phase does not change the dashboard shell, its overview, agents, workflows, or runs pages, or the product dashboard routes. `frontend/src/lib/mock-data.ts` remains the compatibility shim those pages import.
