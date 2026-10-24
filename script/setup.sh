#!/usr/bin/env bash
# Install dependencies for backend and frontend.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

echo "==> backend"
cd "$ROOT/backend"
python3 -m venv .venv
.venv/bin/pip install -q --upgrade pip
.venv/bin/pip install -q -e ".[dev]"
[ -f .env ] || cp .env.example .env

echo "==> frontend"
cd "$ROOT/frontend"
npm install
[ -f .env.local ] || cp .env.example .env.local

echo "==> convex tests"
cd "$ROOT/convex"
npm install

echo "Done. Run ./script/dev.sh to start both servers."
echo "Apply supabase/bootstrap_local.sql and supabase/migrations before using /sign-in against local Postgres."
