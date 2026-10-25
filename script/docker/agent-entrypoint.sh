#!/bin/sh
# Apply the agent schema, then start the private agent process.
set -eu

url="${NMC3_DATABASE_URL:?NMC3_DATABASE_URL is required}"

i=0
until pg_isready -d "$url" >/dev/null 2>&1; do
  i=$((i + 1))
  if [ "$i" -gt 30 ]; then
    echo "agent database did not become ready" >&2
    exit 1
  fi
  sleep 1
done

for migration in \
  db/migrations/202610240001_nmc3_pass1.sql \
  db/migrations/202610240002_nmc3_data_api.sql \
  db/migrations/202610240003_nmc3_event_source.sql \
  db/migrations/202610240004_nmc3_approvals.sql \
  db/migrations/202610240005_nmc3_durable.sql \
  db/migrations/202610240006_nmc3_definitions.sql \
  db/migrations/202610240009_nmc3_policy_pg.sql \
  db/migrations/202610240010_nmc3_context.sql \
  db/migrations/202610250001_nmc3_threads.sql
do
  psql "$url" -v ON_ERROR_STOP=1 -f "$migration"
done

exec nmc3-agent
