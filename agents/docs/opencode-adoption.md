# OpenCode adoption plan: stop rebuilding the agent runtime

**Status:** in progress, October 2026. The custom engine is deleted as of this
revision (`internal/runtime/`, `internal/model/`, `internal/tools/` removed;
recoverable from git history). The Go service serves history only until the
OpenCode backend lands. This plan re-homes NMC3 agent execution on
OpenCode V2 (verified locally: `opencode v2.0.26`, HTTP API, `@opencode/client`)
instead of extending the hand-built Go coordinator. It does not change the
product direction in [product-spec.md](product-spec.md) or the Pass 1
acceptance list in [first-pass.md](first-pass.md); it changes which code
executes runs.

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
4. The deterministic adapter is replaced by a scripted harness at the
   translation layer for contract tests (no live calls, no secrets).

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

## Migration phases

**Phase 0 — Spike (1 session, no merges).** Drive one vendor comparison
through `opencode api` manually: create session, prompt, read messages/stats,
confirm usage numbers and subagent behavior. Verifies the API surface before
we commit to it.

**Phase 1 — Provenance layer.** Go service gains an OpenCode REST client;
`Start` creates a session and persists the mapping; a subscriber streams
OpenCode events into Supabase `run_events`. Old coordinator stays behind a
flag. Acceptance: deterministic scripted run works through both paths with
identical `/v1` shapes.

**Phase 2 — Cutover.** Agent definitions move to `opencode.jsonc`; catalog
becomes MCP; permissions encode the allowlists; validation layer checks
structured outputs. Delete `internal/runtime/`, `internal/model/`,
`internal/tools/` and their tests. Acceptance: full Pass 1 scenario list
(success, concurrent specialists, denied delegation, denied tool, invalid
data, limits, provider failure, restart inspection) passes against OpenCode
execution.

**Phase 3 — Cleanup.** Remove the coordinator flag, archive the deterministic
adapter, update `README.md`, `first-pass.md` status, and this plan with the
final shape.

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
- Deterministic tests: OpenCode sessions are live model calls; contract and
  regression tests need the scripted harness, never the network.
- Restart semantics: unchanged from Pass 1 — completed runs stay inspectable
  in Supabase; interrupted runs are not auto-resumed (Pass 3).

## Revert plan

Until Phase 2 completes, the Go coordinator remains on a flag and all
contract tests run against both backends. If OpenCode cannot meet a Pass 1
scenario, the custom path is still the fallback and this plan is revised
instead of the product.
