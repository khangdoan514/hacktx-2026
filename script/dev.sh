#!/usr/bin/env bash
# Run backend (:8000) and frontend (:3000) together. Ctrl+C stops both.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

trap 'kill 0' EXIT

(cd "$ROOT/backend" && .venv/bin/uvicorn app.main:app --app-dir src --reload --port 8000) &
(cd "$ROOT/frontend" && npm run dev) &

wait
