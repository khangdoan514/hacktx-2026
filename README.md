# hacktx-2026

| Folder      | What                                         |
|-------------|----------------------------------------------|
| `frontend/` | Next.js (App Router, TypeScript, Tailwind)   |
| `backend/`  | FastAPI                                      |
| `ml/`       | Notebooks, training scripts, model artifacts |
| `db/`       | SQL migrations and seed data                 |
| `script/`   | Dev helper scripts                           |
| `docs/`     | Project documentation                        |

## Quick start

Requires Node 22+ and Python 3.11+.

Business data lives in Supabase Postgres (`supabase/migrations`). Copy `backend/.env.example` and set `DATABASE_URL`. For a local Postgres without the Supabase CLI, apply `supabase/bootstrap_local.sql` and then the migrations. `ALLOW_LOCAL_AUTH=true` enables email/password sign-in at `/sign-in` when Supabase Auth is not running. Leave `CONVEX_URL` empty unless you want live hints; the product still works by polling.

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
