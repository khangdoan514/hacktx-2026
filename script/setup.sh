#!/usr/bin/env bash
# Install frontend dependencies. The API is the Go module at the repository root.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "==> go modules"
cd "$ROOT"
go mod download

echo "==> frontend"
cd "$ROOT/frontend"
npm install
[ -f .env.local ] || cp .env.example .env.local

echo "==> agent API"
"$ROOT/agents/script/setup.sh"

echo "Done. The business API applies supabase/bootstrap_local.sql and supabase/migrations on startup when ENVIRONMENT=local."
echo "Local sign-in is available only when ENVIRONMENT is local or test."
echo "The console agent API listens on 127.0.0.1:8080. Run ./script/dev.sh."
