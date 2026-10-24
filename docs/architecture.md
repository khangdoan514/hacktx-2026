# Architecture

```
browser ──► Next.js (frontend, :3000) ──/api/* rewrite──► FastAPI (backend, :8000)
                                                            │
                                                            ├── db/   (schema + seeds)
                                                            └── ml/models/ (trained artifacts)
```

- The frontend calls relative `/api/...` URLs (`frontend/src/lib/api.ts`).
  `frontend/next.config.ts` proxies them to `BACKEND_URL`, so there are no CORS issues in dev.
- All backend routes live under the `/api` prefix (`backend/src/app/api/router.py`).
- Add a new endpoint: create `backend/src/app/api/routes/<name>.py` with an `APIRouter`, then include it in `router.py`.

## Planned agent service

The general-purpose Go agent service will live in `agents/`. The Next.js website will call its API, directly or through a thin proxy. Supabase Postgres is selected for agent definitions, runs, and events; Supabase Storage will hold artifacts, with `pgvector` available for later retrieval features. See [agents/README.md](../agents/README.md) and the [storage specification](../agents/docs/storage.md). None of these connections is implemented in the current starter scaffold.
