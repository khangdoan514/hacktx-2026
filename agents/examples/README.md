# Local first-pass examples

`definitions.json` contains loaded, versioned definitions for the vendor comparison and a depth-denial demonstration. `deterministic-actions.json` is the script for the product application's adapter in `internal/agents/dummy` (`internal/productapi/wire.go`). It is not an execution mode of `cmd/nmc3-agent`. `vendor-comparison.json` is the sample `POST /v1/agent-runs` request.

With the Go API and Supabase Postgres running, execute `agents/script/replay.sh` to submit the comparison and print its terminal snapshot and ordered events. Set `AGENT_API_URL` if the API is not on `http://localhost:8080`.

For the browser, copy `frontend/.env.example` to `frontend/.env.local`, run `npm ci` and `npm run dev` in `frontend/`, and open `/runs`. The Next.js server proxies `/agent-api/*` to the Go service; the browser never receives database or provider credentials. Paste a saved run ID to inspect it after restarting the API. To see a denied depth-1 delegation, change the request's agent ID to `denial-orchestrator`. `opencode.jsonc` denies that specialist's attempt to launch `catalog-specialist`, and it also denies shell, edits, network tools, and direct reads of the catalog file. Catalog specialists use the `catalog_search` MCP tool.

Run history persists in Postgres. In-flight work does not automatically resume after an API restart; that is planned for Pass 3.
