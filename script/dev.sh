#!/usr/bin/env bash
# Run the Go API (:8000) and Next.js (:3000). Ctrl+C stops both.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

if [[ ! -f "$ROOT/.env" ]]; then
  cp "$ROOT/.env.example" "$ROOT/.env"
fi
set -a
# shellcheck disable=SC1091
source "$ROOT/.env"
set +a

trap 'kill 0' EXIT

(cd "$ROOT" && go run ./cmd/api) &
(cd "$ROOT/frontend" && npm run dev) &

wait
