# OpenCode adoption plan: stop rebuilding the agent runtime

**Status:** Phase 3 cleanup done, October 2026. Agent execution is one
OpenCode path. The custom engine (`internal/runtime/`, `internal/model/`,
`internal/tools/`) and any coordinator flag are gone; both remain only in
git history. There is no agents-side deterministic model adapter. Contract
tests use the scripted fake OpenCode backend in
`internal/opencode/coordinator_test.go`. This plan does not change the
product direction in [product-spec.md](product-spec.md) or the Pass 1
acceptance list in [first-pass.md](first-pass.md); it changes which code
executes runs. The first pass is not fully complete. Phase 0 was checked
against OpenCode V2 (`opencode v2.0.26`). This machine's CLI is `opencode
v2.1.4`. A live permission check against that binary has not been run.
`db/migrations/202610240003_nmc3_event_source.sql` is applied on the hosted
database.

## Why

The custom runtime (packages A–C) re-implements what OpenCode already ships:
model routing across OSS/GPT/Claude, subagent delegation, permission-scoped
tools, MCP servers, skills, session events, and usage stats. Keeping it means
we maintain a second agent engine forever. The parts worth keeping are the
parts OpenCode does not provide: NMC3's versioned `/v1` API, Supabase-backed
run history with replayable ordered events, and the Runs UI.

## Keep vs replace

| Current package | Verdict | Rationale |
| --- | --- | --- |
| A. Runtime coordinator (`internal/runtime/`) | **Replace** with OpenCode sessions + subagents | OpenCode's `experimental.subagent_depth` defaults to `1`: a subagent cannot start another subagent. That is our depth-1 rule, enforced by the harness instead of our code. |
| B. Model adapters (`internal/model/`) | **Replace** with OpenCode providers/models | One `provider/model` string covers OpenRouter OSS, GPT, and Claude. No per-provider adapter code, no prompt-format maintenance. The OpenRouter key moves to OpenCode credential/env config. |
| C. Tool gateway + catalog (`internal/tools/`, `fixtures/`) | **Replace** with permissions + MCP (or a narrow plugin) | Per-agent `permissions` deny everything except the allowlisted catalog tool; the fictional catalog becomes an MCP server or read-only plugin tool. Our allowlist tests become permission-rule tests. |
| D. Supabase run store + events (`internal/store/`, migrations) | **Keep, adapt** | OpenCode event subscriptions are live-only with no replay; our persisted per-run ordered events remain the inspection source of truth. The store persists OpenCode session IDs, message history, and stats instead of our invocation tree. No migration rewrite: snapshots gain an `opencode_session_id` field. |
| E. HTTP API (`internal/httpapi/`, `cmd/nmc3-agent/`) | **Keep, re-target** | The `/v1` contract stays stable for the UI. `POST /v1/agent-runs` creates an OpenCode session (`POST /api/session` + `/prompt`) instead of starting the Go coordinator; `GET` endpoints read Supabase. Add a Go HTTP client for the OpenCode server (REST; there is no Go SDK). |
| F. Examples + website (`examples/`, `script/`, `frontend/`) | **Keep** | Agent definitions translate to `opencode.jsonc` `agents` entries; `vendor-comparison.json` becomes a session prompt template; the Runs page is unchanged (same `/v1` shapes). |

## Target shape

```text
Runs UI / API clients
        │  frozen /v1 contract
Go service (thin): translate, validate, persist, inspect
        │  OpenCode REST (sessions, messages, events, stats)
OpenCode service: models, subagents, permissions, MCP tools
        │
Supabase Postgres: runs, ordered events, usage (replayable history)
```

The Go service keeps three jobs it is good at: (1) validating requests
against NMC3 schemas before execution, (2) validating structured results
after execution (OpenCode returns free-form text; schema enforcement stays
our layer), and (3) persisting everything to Supabase so history survives
restarts and remains ordered. OpenCode owns model calls, delegation,
tool execution, and credentials.

## New configuration artifacts

- `opencode.jsonc` (project): default model (`meta/muse-spark-1.3-contributor`
  stays the cheap default), `agents` entries for orchestrator + specialists
  with `mode: "subagent"`, per-agent `permissions` (deny-all except catalog),
  MCP catalog server, `{env:OPENROUTER_API_KEY}` secret reference.
- `agents/<id>.md` or inline `agents` entries carrying the current
  `definitions.json` instructions verbatim for the first cut.
- No secrets in git: keys stay in env/files per OpenCode `{env:}`/`{file:}`
  substitution. `agents/.env` remains the local secret source.

## Contract changes (integrator-owned, one revision)

1. `Run` gains `opencode_session_id`; `Usage` is filled from OpenCode session
   stats (`SessionStats.ModelUsage`) instead of per-turn adapter reports.
2. `RunEvent` gains an optional `source` marker (`nmc3` vs `opencode`) so
   provenance stays auditable; sequence and ordering rules are unchanged.
3. `StartRunRequest.model_id` maps to OpenCode's `provider/model` format
   (already compatible: `openai/gpt-4o-mini`, `anthropic/claude-haiku-5.5`,
   `meta/muse-spark-1.3-contributor`).
4. Contract tests use the scripted fake OpenCode backend in
   `internal/opencode/coordinator_test.go` (no live calls, no secrets, no
   database). That harness is not a database-backed deterministic adapter.
   The product application's scripted adapter (`internal/agents/dummy`,
   script `examples/deterministic-actions.json`) is a separate path.

## Phase 0 result (completed 2026-10-24, opencode v2.0.26)

Manual spike via `opencode api`: session `ses_e8ff4bf11ffeYPpJZmlt8MNHCX`
(`meta/muse-spark-1.3-contributor`) ran the vendor comparison with two
parallel `subagent` calls (price + accessibility, both completed) and
returned exactly the schema-shaped JSON result with zero file writes.
Per-session usage is reported on the session object
(input 9956 / output 626 / reasoning 406, cost $0.00122), which is what
`Run.usage` will map from. Open items for Phase 1: enforce JSON-only output
via a validation layer (not harness-native), confirm depth-1 non-escalation
under deny-by-default permissions, and build the scripted non-network test
harness.

## Phase 1 result (completed 2026-10-24)

`internal/opencode/` implements the execution backend: REST client (Basic
auth via service registration), session create/prompt/wait/messages,
coordinator mapping OpenCode sessions to NMC3 runs (validation, invocation
tree with one depth-1 child per subagent call, ordered `source: "opencode"`
events, provider-reported usage), and the restored schema validator.
Verified live: vendor run SUCCEEDED on `meta/muse-spark-1.3-contributor`
($0.00148), byte-identical across API restart (14 ordered events), website
`/agent-api` flow SUCCEEDED. Fixes found live: newest-first message order,
30s client timeout killing long waits, 60s fixture limit too short for model
runs. Pending: apply `db/migrations/202610240003_nmc3_event_source.sql`
(source column currently omitted on read).

## Phase 2 result

`agents/opencode.jsonc` is the execution policy. Global and per-agent
permissions deny every action, then allow only the orchestrator's named
subagents. Built-in `build`, `plan`, `general`, and `explore` are denied, so
a session cannot escalate by switching agent. `experimental.subagent_depth`
is 1. Sessions are created with the NMC3 agent id and the `agents/` directory,
so OpenCode loads that file.

The fictional catalog is the local MCP server `cmd/catalog-mcp` (`catalog_search`,
Code Mode off). `catalog-specialist` and `accessibility-specialist` may call
that tool. They may not read `fixtures/catalog/vendors.json`, and no agent may
list or read MCP resources. The Go service maps `catalog_search` back to the
contract id `catalog.search` and records any other tool as `TOOL_NOT_ALLOWED`.
Denied delegations are `AGENT_NOT_ALLOWED` or `DELEGATION_DEPTH_EXCEEDED` and
do not create a child. `go test` evaluates the same last-match rules OpenCode
documents. This machine's CLI is `opencode v2.1.4`. A live check against
that binary has not been run.

## Phase 3 result (completed 2026-10-24)

One OpenCode execution path remains. `cmd/nmc3-agent` always constructs
`internal/opencode.Coordinator`. There was no coordinator flag left to
remove, and `internal/runtime/`, `internal/model/`, and `internal/tools/`
were already deleted in Phase 2. Those packages were not restored.

There is no agents-side deterministic model adapter to archive.
`internal/model/` left with Phase 2. Contract tests are the scripted fake
OpenCode backend in `internal/opencode/coordinator_test.go`: in-process, no
network, no provider credentials, and no database. That harness is not a
database-backed deterministic adapter.

`internal/agents/dummy` and `agents/examples/deterministic-actions.json`
stay. They are the product application's scripted adapter
(`internal/productapi/wire.go`), not a second engine for this service.

Submitted run limits must be positive. The wait deadline
(`limits.timeout_seconds`) cancels the OpenCode wait and records `TIMEOUT`.
Token and cost figures on a run are copied from OpenCode session usage.
This service does not stop a run for `max_tokens`, `max_cost_usd`, turn
count, tool-call count, or delegation count.

`README.md`, `first-pass.md`, and this plan describe that shape. This
machine's CLI is `opencode v2.1.4`. A live permission check against that
binary has not been run. The event-source migration is applied on the
hosted database. The first pass is not claimed complete.

## Migration phases

**Phase 0 — Spike (1 session, no merges).** Drive one vendor comparison
through `opencode api` manually: create session, prompt, read messages/stats,
confirm usage numbers and subagent behavior. Verifies the API surface before
we commit to it.

**Phase 1 — Provenance layer (completed).** Go service gained an OpenCode
REST client; `Start` creates a session and persists the mapping; events are
written to Supabase `run_events`. The Phase 1 note kept the old coordinator
behind a flag. That flag is gone; see the Phase 3 result. Contract tests
use the scripted fake OpenCode backend only.

**Phase 2 — Cutover.** Agent definitions move to `opencode.jsonc`; catalog
becomes MCP; permissions encode the allowlists; validation layer checks
structured outputs. Delete `internal/runtime/`, `internal/model/`,
`internal/tools/` and their tests. Acceptance: full Pass 1 scenario list
(success, concurrent specialists, denied delegation, denied tool, invalid
data, limits, provider failure, restart inspection) passes against OpenCode
execution.

**Phase 3 — Cleanup (completed).** See the Phase 3 result. The coordinator
flag and the agents-side deterministic adapter were already absent. Docs
now match the single OpenCode path. The product dummy adapter stays.

## Gaps to verify in Phase 0 (not assumed)

- Structured output: OpenCode has no first-class "exactly one JSON action"
  constraint. Our post-execution schema validation must reject bad results as
  typed errors. Confirm instruction-following is reliable enough for the
  vendor example across the models we list.
- Depth enforcement: `subagent_depth: 1` matches our rule, but confirm a
  depth-1 agent cannot escalate (e.g. via shell or MCP) under our
  deny-by-default permissions.
- Usage mapping: confirm session stats expose input/output tokens and cost
  per model so `Run.usage` stays truthful.
- Deterministic tests: OpenCode sessions are live model calls. Contract
  tests use the scripted fake OpenCode backend, never the network and never
  a database-backed model adapter.
- Restart semantics: unchanged from Pass 1 — completed runs stay inspectable
  in Supabase; interrupted runs are not auto-resumed (Pass 3).

## Revert plan

The custom coordinator is not a flag and not an in-tree fallback. Restoring
it means recovering `internal/runtime/`, `internal/model/`, and
`internal/tools/` from git history and revising this plan. The product
application's scripted adapter is a separate path and is not that fallback.
If OpenCode cannot meet a Pass 1 scenario, revise this plan; do not treat
the dummy adapter as the agents execution engine.
