# Live web research with SerpApi

`web-researcher@1.0.0` researches a task through the official SerpApi MCP
server using the existing OpenCode execution path. The separate extensions
gateway is not involved. Existing vendor agents still use the fictional catalog
and cannot call SerpApi.

## Setup

You need Go, OpenCode V2, a configured model provider, and the agent service's
Supabase or Postgres settings. Set `SERPAPI_API_KEY` in the ignored `agents/.env`
or your local environment. Keep it out of request bodies, frontend variables,
checked-in configuration, and MCP URLs.

The key must be available to the **OpenCode server process**, which resolves
`{env:SERPAPI_API_KEY}` in `opencode.jsonc`. Exporting it only for the Go API
does not update an already-running service. Configure your service manager's
environment and restart it, or start a dedicated foreground server:

```sh
cd agents
set -a
source .env
set +a
opencode serve --hostname 127.0.0.1 --port 49374
```

Use an available port if the shared service already occupies `49374`. In the
API shell set `OPENCODE_URL` to match and `OPENCODE_SERVICE_PASSWORD` to the
dedicated server's printed password. The existing shared-service registration
remains an alternative. Load your database settings in the API shell and start
the API from `agents/` with `go run ./cmd/nmc3-agent`.

`opencode mcp list` from this project checks discovery. Discovery can succeed
without a key; only a search validates authentication and quota.

References: [SerpApi MCP](https://serpapi.com/integrations/mcp),
[OpenCode MCP configuration](https://opencode.ai/v2/docs/mcp-servers/),
[OpenCode server configuration](https://opencode.ai/v2/docs/cli/web/).

## Run and frontend contract

From `agents/`, with the API running:

```sh
bash script/replay.sh examples/web-research.json
```

The example uses the service's default model; optionally add `model_id` for a
configured provider/model. Replay accepts a request file and waits for its
declared timeout plus ten seconds, exiting nonzero on failure.

- `GET /v1/agents` includes `web-researcher` and its schemas.
- `POST /v1/agent-runs` accepts the example request.
- `GET /v1/agent-runs/{id}` returns state, usage, output, and tool invocations.
- `GET /v1/agent-runs/{id}/events` returns tool and run success/failure events.

Successful output shape (illustrative, not a saved result):

```json
{
  "recommendation": "An answer based on returned search snippets.",
  "source_ids": ["https://go.dev/blog/context"],
  "sources": [
    {
      "title": "Go Concurrency Patterns: Context",
      "url": "https://go.dev/blog/context",
      "snippet": "The snippet returned by the provider."
    }
  ],
  "limitations": ["Based on search result snippets; full pages were not fetched."]
}
```

`recommendation` and `source_ids` work with the current frontend result adapter.
The frontend can also display `sources` as titled links and `limitations`.
The backend checks cited URLs against actual search results and fills their
titles/snippets itself, overwriting model-provided source descriptions.

`tool_invocations[].tool_id` is `serpapi.search`. Input records only `q`,
`engine`, and optional `location` under `params`, plus `mode` when present;
output records `{ "sources": [...] }` from
organic results. Headers, arbitrary search parameters, provider metadata, and
raw provider errors are not copied into snapshots or event metadata.

A run requires a successful search. Missing credentials, quota errors,
unreadable results, or connection failures produce a tool failure; the run
fails if no search succeeds. Invented citations, or missing citations when
results exist, fail with `OUTPUT_SCHEMA_INVALID`. Empty results produce an
explicit no-results answer and empty source arrays.

## Scope and validation

This integration supports Google JSON `organic_results`, requested with
`params.engine=google` and `mode=compact`. Only `serpapi_search` is permitted
for this agent. Shell, files, delegation, built-in web search, MCP resource
reads, and SerpApi app tools remain denied. Search snippets are untrusted
evidence; URL provenance does not verify the answer's factual claims.

Events and tool snapshots are collected after the OpenCode session idles,
following existing coordinator behavior. This does not introduce streaming or
enforce additional token, search-count, or cost limits. The local API still
lacks production authentication and tenant isolation; use public,
non-sensitive inputs.

Run `go test ./...` in `agents/`. Research tests use a fake OpenCode backend
to exercise coordinator recording, failures, source resolution, and the
checked-in request. Policy tests check that only this agent can search.
On macOS, if the existing extension filesystem test fails because `/var`
resolves to `/private/var`, run `TMPDIR=/private/tmp go test ./...`.

An authenticated search was separately checked against the hosted MCP server.
To repeat that check with the key exported, run
`NMC3_TEST_SERPAPI_LIVE=1 go test ./internal/opencode -run '^TestSerpApiLive$' -v`.
This opt-in test spends one search credit and verifies transport and result
normalization. A full model-driven API run additionally needs the configured
OpenCode server, model provider, and database.
