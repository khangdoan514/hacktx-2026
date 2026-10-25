#!/usr/bin/env bash
# Prepare the dedicated agent API: its own Postgres database, the nmc3 schema,
# and the local OpenCode service. This database is not the business API database.
# Both services use a schema named nmc3 with different tables.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
AGENTS="$ROOT/agents"
DB_NAME="${NMC3_AGENT_DB:-hacktx_nmc3_agent}"
export PATH="${HOME}/.opencode/bin:${PATH}"

if [[ ! "${DB_NAME}" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
  echo "NMC3_AGENT_DB must be a plain database name." >&2
  exit 1
fi

if ! command -v psql >/dev/null 2>&1 || ! command -v createdb >/dev/null 2>&1; then
  echo "psql and createdb are required to set up the agent database." >&2
  exit 1
fi

if ! psql -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${DB_NAME}'" | grep -q 1; then
  createdb "${DB_NAME}"
fi

echo "==> agent database ${DB_NAME}"
psql "postgresql:///${DB_NAME}" -v ON_ERROR_STOP=1 <<'SQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
    CREATE ROLE anon NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
    CREATE ROLE authenticated NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'service_role') THEN
    CREATE ROLE service_role NOLOGIN;
  END IF;
END $$;
SQL

for migration in \
  "$AGENTS/db/migrations/202610240001_nmc3_pass1.sql" \
  "$AGENTS/db/migrations/202610240002_nmc3_data_api.sql" \
  "$AGENTS/db/migrations/202610240003_nmc3_event_source.sql"
do
  psql "postgresql:///${DB_NAME}" -v ON_ERROR_STOP=1 -f "$migration"
done

echo "==> agent Go module"
(cd "$AGENTS" && go mod download)

if ! command -v opencode >/dev/null 2>&1; then
  echo "==> installing OpenCode"
  curl -fsSL https://opencode.ai/v2/install | bash -s -- --no-modify-path
  export PATH="${HOME}/.opencode/bin:${PATH}"
fi
if ! command -v opencode >/dev/null 2>&1; then
  echo "OpenCode is not on PATH after install. Expected ${HOME}/.opencode/bin/opencode." >&2
  exit 1
fi

if ! opencode service status >/dev/null 2>&1; then
  echo "==> starting OpenCode"
  opencode service start
fi
if [[ ! -s "${HOME}/.config/opencode/service.json" ]]; then
  echo "OpenCode did not write ~/.config/opencode/service.json." >&2
  exit 1
fi

echo "Agent API database is ready (${DB_NAME})."
echo "OpenCode is available. Start the API with ./script/dev.sh or, from agents/:"
echo "  NMC3_STORE=postgres DATABASE_URL=postgresql:///${DB_NAME} go run ./cmd/nmc3-agent"
