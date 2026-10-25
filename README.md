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

Docker runs the same three processes plus Postgres:

```bash
docker compose up --build
```

The site is `http://localhost:3010`. The console signs in through Supabase at `/sign-in`. The demo employees `ada.hardware@nmc2.test`, `bao.ai@nmc2.test`, and `cam.review@nmc2.test` use `local-demo-pass`. The application API is on port 8000 and calls the agent container on the Compose network. Postgres is on `127.0.0.1:54330` with user `nmc3` and password `nmc3-local`. Live model runs still need OpenCode, and the host OpenCode service only sees host paths, so run the agent on the host with an absolute `NMC3_WORKDIR` (the `agents/` directory), bind it to `0.0.0.0:8080`, and start the API container with `COMPOSE_AGENT_API_URL=http://host.docker.internal:8080`. Point `OPENCODE_URL` at a service the agent container can reach, and set `OPENCODE_SERVICE_PASSWORD` to that service's password.

The business API listens on `:8000`, the agent API on `127.0.0.1:8080`, and Next.js on `:3000`. `/` is the console. Its `/agent-api` calls go to the business API, which checks the employee token and then calls `agents/cmd/nmc3-agent`. A CLI uses that same token: `curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8000/agent-api/agents`. The agent process executes through the local OpenCode service. `./script/dev.sh` creates the separate `hacktx_nmc3_agent` database, applies `agents/db/migrations`, and starts OpenCode when it is not already running. `/sign-in`, `/org`, and `/requests` stay on the business API. `GET /api/health` stays available.

Agent runs use `AGENT_EXECUTION_MODE=dummy`. The mode and adapter version are stored on the run. This build does not call a model provider and does not substitute dummy output for a failed provider call.

## Verification boundary

Auth token checks were exercised with a local JWKS fixture. Session refresh against a hosted Supabase project was not run. Private files are stored on local disk through the API. Supabase Storage authorization was not run. Plain Postgres covers schema, RLS, and the Go commands. It is not proof of Supabase Auth or Storage.

`ml/` is unchanged and is not part of the API.

See [docs/architecture.md](docs/architecture.md).
