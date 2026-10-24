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

echo "Done. Apply supabase/bootstrap_local.sql and supabase/migrations, then run ./script/dev.sh."
echo "Local sign-in is available only when ENVIRONMENT is local or test."
