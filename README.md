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

Requires Node 18+ and Python 3.11+.

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

The main project focus is the agent and orchestration service. Start with [agents/README.md](agents/README.md) for the end-to-end product specification, first-pass scope, and later delivery passes. The current quick start runs only the existing frontend and FastAPI scaffold; it does not start an agent runtime yet.
