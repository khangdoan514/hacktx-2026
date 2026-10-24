# Architecture

```
browser ──► Next.js
              │
              ├── (dashboard) existing agent shell
              └── (product) business routes at /sign-in and /w/...
                    │
                    ├── Supabase Auth access token, or local sign-in when explicitly enabled
                    └── bearer /api/v1 ──► FastAPI
                                              │
                                              ├── Postgres schema app (business source of truth, RLS)
                                              ├── private document bytes
                                              └── optional Convex per-user hints
                                                    └── product pages refetch FastAPI
```

- The frontend calls relative `/api/...` URLs (`frontend/src/lib/api.ts`). `frontend/next.config.ts` proxies them to `BACKEND_URL`.
- `/api/health` is liveness. `/api/ready` checks the database. Business routes live under `/api/v1`.
- FastAPI verifies the bearer JWT, then sets a transaction-local user id and `SET LOCAL ROLE app_runtime` so Postgres RLS applies. `app_runtime` must not own protected tables and must not bypass RLS.
- Business migrations in `supabase/migrations` are the history for schema `app`. Agent run tables are not in that schema. A future Go service would own its own schema, such as `nmc3`, and FastAPI does not write those rows.
- Convex replaces Supabase Realtime for these business invalidation hints only. There is no second live-delivery path. Hints are optional. If Convex is unset, product pages poll FastAPI. Hints are per authenticated user, and losing project access deletes them.

## Planned agent service

The Go agent service is specified under `agents/`. Its first HTTP contract (`POST /v1/agent-runs` and the related reads) is not implemented here and has no authentication in that first pass. This application does not call it and does not send company or partner context to it. See [agents/README.md](../agents/README.md) and [agents/docs/storage.md](../agents/docs/storage.md).

## Frontend ownership proposal

This is a proposal, not an agreement. `agents/docs/parallel-work.md` still assigns package F ownership of `frontend/` as a whole. That claim is not overridden by this document. Both teams still need to accept a narrower split.

Proposed paths for the business app: `frontend/src/app/(product)/` and `frontend/src/product/`. There is no `(product)/page.tsx`, so `/` stays the dashboard overview. Proposed paths for the agent UI: `frontend/src/app/(dashboard)/` and the dashboard components.

Shared files that any split has to cover: `frontend/package.json`, `frontend/package-lock.json`, `frontend/src/lib/api.ts`, and `frontend/src/lib/mock-data.ts`. `mock-data.ts` is an empty compatibility shim so the dashboard typechecks. The dashboard owner should replace it. It does not preserve previous sample content.
