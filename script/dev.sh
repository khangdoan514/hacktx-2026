#!/usr/bin/env bash
# Run the business API (:8000), the agent API (:8080), and Next.js (:3000).
# Ctrl+C stops those three. The OpenCode service keeps running.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

if [[ ! -f "$ROOT/.env" ]]; then
  cp "$ROOT/.env.example" "$ROOT/.env"
fi
set -a
# shellcheck disable=SC1091
source "$ROOT/.env"
set +a

"$ROOT/agents/script/setup.sh"

trap 'kill 0' EXIT

(cd "$ROOT" && go run ./cmd/api) &
(
  cd "$ROOT/agents"
  if [[ -f .env ]]; then
    set -a
    # shellcheck disable=SC1091
    source .env
    set +a
  fi
  if [[ -n "${SUPABASE_SECRET_KEY:-}" && -z "${NMC3_STORE:-}" ]]; then
    store="supabase"
  else
    store="${NMC3_STORE:-postgres}"
  fi
  export NMC3_STORE="$store"
  if [[ "$store" == "postgres" ]]; then
    export DATABASE_URL="${AGENT_DATABASE_URL:-postgresql:///hacktx_nmc3_agent}"
  fi
  export PATH="${HOME}/.opencode/bin:${PATH}"
  go run ./cmd/nmc3-agent
) &
(cd "$ROOT/frontend" && npm run dev) &

wait
