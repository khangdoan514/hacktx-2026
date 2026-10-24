#!/usr/bin/env bash
set -euo pipefail
api_url="${AGENT_API_URL:-http://localhost:8080}"
script_dir="$(cd "$(dirname "$0")" && pwd)"
response="$(curl -fsS -H 'Content-Type: application/json' --data-binary "@$script_dir/../examples/vendor-comparison.json" "$api_url/v1/agent-runs")"
run_id="$(printf '%s' "$response" | python3 -c 'import json,sys; print(json.load(sys.stdin)["run_id"])')"
echo "Run: $run_id"
for attempt in $(seq 1 120); do
  snapshot="$(curl -fsS "$api_url/v1/agent-runs/$run_id")"
  state="$(printf '%s' "$snapshot" | python3 -c 'import json,sys; print(json.load(sys.stdin)["run"]["state"])')"
  if [[ "$state" == SUCCEEDED || "$state" == FAILED || "$state" == CANCELLED ]]; then
    printf '%s\n' "$snapshot" | python3 -m json.tool
    curl -fsS "$api_url/v1/agent-runs/$run_id/events" | python3 -m json.tool
    [[ "$state" == SUCCEEDED ]]
    exit
  fi
  sleep 0.5
done
echo "Timed out waiting for run $run_id" >&2
exit 1
