# hacktx-2026

NMC3 is the internal tool for NMC2. Employees request a capability from Software/AI, clarify it, review an exact specification revision, run the PR Review Assistant, approve, and hand the result over. There is one organization. Hardware and Software are the divisions, and AI is a team inside Software.

| Folder | What |
| --- | --- |
| `frontend/` | Next.js client |
| `cmd/` and `internal/` | Go API and worker |
| `agents/` | Agent fixtures, examples, and notes |
| `ml/` | Notebooks and training scripts, separate from this service |
| `supabase/` | SQL migrations |
| `docs/` | Architecture and the record of the replaced FastAPI behavior |

## Quick start

Requires Node 22+ and Go 1.22+.

```bash
./script/setup.sh
cp .env.example .env
```

For a local Postgres without the Supabase CLI, create a database and set `DATABASE_URL`. With `ENVIRONMENT=local`, the API applies `supabase/bootstrap_local.sql` and `supabase/migrations` on startup and seeds three employees:

- `ada.hardware@nmc2.test`
- `bao.ai@nmc2.test`
- `cam.review@nmc2.test`

The local password is `local-demo-pass`. `ALLOW_LOCAL_AUTH=true` works only when `ENVIRONMENT` is `local` or `test`. Production refuses that flag, a missing issuer/audience/JWKS URL, the secret `dev-only-change-me`, and a development issuer.

```bash
./script/dev.sh
```

Docker runs the web app, API, OpenCode agent bridge, Postgres, and a private S3-compatible Silo bucket for thread attachments:

```bash
docker compose up --build
```

The site is `http://localhost:3010`. The console signs in through Supabase at `/sign-in`. The demo employees `ada.hardware@nmc2.test`, `bao.ai@nmc2.test`, and `cam.review@nmc2.test` use `local-demo-pass`. Postgres is on `127.0.0.1:54330`; Silo's S3 endpoint and admin console are on `127.0.0.1:9000` and `127.0.0.1:9001`. The root `.env` is ignored and contains local Docker settings. Its `PROJECT_ROOT` and `NMC3_WORKDIR` must be absolute host paths; the agent container mounts the repository at that same path so the host OpenCode service can read the agent config and workspace. Start OpenCode with `opencode serve --service --hostname 0.0.0.0 --port 49374` so the container can reach it. `OPENCODE_SERVICE_PASSWORD` is set from the host OpenCode registration file when available. The default model is `opencode/beam-free`; set `OPENCODE_MODEL` to override it. Thread history and tool snapshots are indexed in Postgres; uploaded files are stored in the private S3 bucket and read from there when building the agent prompt. Text, PDF, images, and common Office files can be analyzed, subject to model input support.

The business API listens on `:8000`, the agent API on `127.0.0.1:8080`, and Next.js on `:3000`. `/` is the console. Its `/agent-api` calls go to the business API, which checks the employee token and then calls `agents/cmd/nmc3-agent`. A CLI uses that same token: `curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8000/agent-api/agents`. The agent process executes through the local OpenCode service. `./script/dev.sh` creates the separate `hacktx_nmc3_agent` database, applies `agents/db/migrations`, and starts OpenCode when it is not already running. `/sign-in`, `/org`, and `/requests` stay on the business API. `GET /api/health` stays available.

Agent runs use `AGENT_EXECUTION_MODE=dummy`. The mode and adapter version are stored on the run. This build does not call a model provider and does not substitute dummy output for a failed provider call.

## Verification boundary

Auth token checks were exercised with a local JWKS fixture. Session refresh against a hosted Supabase project was not run. Thread attachment bytes use the configured private S3-compatible bucket; database access is scoped to the authenticated principal and tenant. Hosted S3-compatible storage permissions and Supabase Auth were not exercised here.

`ml/` is unchanged and is not part of the API.

See [docs/architecture.md](docs/architecture.md).
