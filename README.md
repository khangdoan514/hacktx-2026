# hacktx-2026

| Folder      | What                                         |
|-------------|----------------------------------------------|
| `frontend/` | Next.js (App Router, TypeScript, Tailwind)   |
| `backend/`  | FastAPI                                      |
| `agents/`   | Go agent contracts and product docs            |
| `ml/`       | Notebooks, training scripts, model artifacts |
| `db/`       | SQL migrations and seed data                 |
| `script/`   | Dev helper scripts                           |
| `docs/`     | Project documentation                        |

## Quick start

Requires Node 22+ and Python 3.11+.

Business data lives in Supabase Postgres (`supabase/migrations`, schema `app`). Copy `backend/.env.example` and set `DATABASE_URL`. For a local Postgres without the Supabase CLI, apply `supabase/bootstrap_local.sql` and then the migrations. That path does not prove Supabase Auth, session refresh, or Storage policies.

`ALLOW_LOCAL_AUTH=true` enables email/password sign-in at `/sign-in` only when `ENVIRONMENT` is `local` or `test`. The product still runs if `CONVEX_URL` is empty; pages poll FastAPI. Convex hints replace Supabase Realtime for those invalidations only.

`frontend/src/lib/mock-data.ts` is an empty compatibility shim so the dashboard imports resolve. The dashboard owner should replace that file. It does not contain sample agents, runs, or workflows.

```bash
./script/setup.sh   # install deps, create .env files
./script/dev.sh     # backend on :8000, frontend on :3000
```

Open http://localhost:3000 – the page shows a message fetched from the backend.
API docs: http://localhost:8000/docs

## Running separately

```bash
# backend
cd backend && source .venv/bin/activate
uvicorn app.main:app --app-dir src --reload

# frontend
cd frontend && npm run dev
```

See [docs/architecture.md](docs/architecture.md) for how they connect.

## NMC3 agent first pass

The agent and orchestration service is specified in [agents/README.md](agents/README.md). That first pass is not running in this checkout, and its HTTP contract is not connected to the business app. The business screens are separate: open `/sign-in` after the API and database are up. `/` remains the existing dashboard shell.
