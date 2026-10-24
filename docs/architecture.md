# Architecture

```
browser ──► Next.js
              │
              ├── (dashboard) agent shell, owned separately
              └── (product) NMC3 routes
                    │
                    ├── Supabase Auth access token, or local sign-in when Supabase is not configured
                    └── bearer /api/v1 ──► FastAPI
                                              │
                                              ├── Supabase Postgres schema `app` (source of truth, RLS)
                                              ├── private document bytes
                                              └── optional Convex hint after commit
                                                    └── product pages refetch FastAPI
```

- The frontend calls relative `/api/...` URLs. `frontend/next.config.ts` proxies them to `BACKEND_URL`.
- `/api/health` stays a liveness check. `/api/ready` checks the database.
- Business routes live under `/api/v1`. FastAPI verifies the bearer JWT, then sets a transaction-local user id and `SET LOCAL ROLE app_runtime` so Postgres RLS applies.
- Supabase migrations in `supabase/migrations` are the only schema history.
- Convex stores revision hints (`workspaceId`, `resourceType`, `resourceId`, `revision`). It does not store business rows. If `CONVEX_URL` is unset, the worker skips the push.
- Product screens are under `frontend/src/app/(product)`. They do not replace the dashboard shell.
