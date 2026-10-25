# First pass: functional agent system

**Status:** in progress, not complete. Execution is the OpenCode path in `agents/internal/opencode`. The custom Go engine and its coordinator flag are gone. There is no agents-side deterministic model adapter. Repeatable contract tests use the scripted fake OpenCode backend in `internal/opencode/coordinator_test.go` (no network, no provider credentials, no database). The product application's scripted adapter (`internal/agents/dummy`) is a separate path. Live OpenCode on this machine is CLI `v2.1.4`. A live permission check against that binary passed for the cases below. `db/migrations/202610240003_nmc3_event_source.sql` is applied on the hosted database. This remains the first usable slice of the agent product in [product-spec.md](product-spec.md). Later passes add reliability, governance, integrations, and authoring features without replacing the core agent model.

## User outcome

A local user can start a run through an API or minimal UI. A depth-0 orchestration agent can invoke two versioned specialists, use an allowed read-only tool, combine structured results, and return a validated answer. The user can inspect run state, the invocation tree, tool outcomes, usage, and errors. Live runs use the local OpenCode service. Repeatable contract tests use the scripted fake OpenCode backend and do not call a model or a database.

The initial example is a vendor comparison using checked-in fictional records. It exercises the product capabilities without making live third-party data a dependency. The example is replaceable; the runtime is not vendor-comparison-specific.

## Functional requirements

1. **Definitions:** Load declarative, versioned orchestrator and specialist definitions. Validate their schemas, allowed tools, delegate targets, and limits at startup. Published-version storage and editing are later passes.
2. **Runs:** `POST /v1/agent-runs` accepts schema-valid input, returns a run ID, and starts work asynchronously. Supabase Postgres stores run snapshots and ordered events. GET endpoints expose current state and events while the run is active and after it ends.
3. **Model turns:** OpenCode performs model calls, delegation, and tools. The Go service validates input before the session and the structured result after it, and records input tokens, output tokens, and cost from the OpenCode session. Submitted limits must be positive. The wait deadline (`timeout_seconds`) records `TIMEOUT`. `max_tokens` (input plus output), `max_cost_usd`, `max_model_turns`, `max_tool_calls`, and `max_delegations` are stop conditions: the coordinator interrupts an in-flight OpenCode session, finishes the run with `BUDGET_EXCEEDED`, and does not accept a further tool call or child invocation past the cap. `max_concurrent_agents` is accepted and is not a stop condition in this layer.
4. **Delegation:** The root may invoke only allowlisted specialists at depth 1. Specialists cannot delegate. Independent specialists may run concurrently. Inputs and results are scoped and schema validated.
5. **Tools:** The fictional catalog is the local `catalog_search` MCP tool. `opencode.jsonc` denies every other action. The Go service maps `catalog_search` to `catalog.search` and records any other tool as `TOOL_NOT_ALLOWED`.
6. **Inspection:** A run snapshot includes state, final output or typed error, and invocations. Events show model, delegation, and tool progress in run order. The minimal UI can start a run and inspect that history.
7. **Local operation:** Document clean Supabase setup, configuration, test commands, and how to run the API and UI. Provider credentials come from environment or a local secret source, never source control. Contract tests use the scripted fake OpenCode backend and need no provider credentials and no database. Live runs need the local OpenCode service. Storage acceptance still needs Supabase.

## Acceptance scenarios

- **Successful run:** Start a vendor comparison, observe progress, and receive a schema-valid recommendation with source IDs and specialist results.
- **Concurrent work:** Two independent specialists execute within the configured limit and the orchestrator aggregates their outputs regardless of completion order.
- **Denied delegation:** A specialist requests another agent. The runtime returns `DELEGATION_DEPTH_EXCEEDED`, records the denial, and does not create a child.
- **Denied tool:** An agent requests a tool outside its allowlist. The handler does not run; the run records `TOOL_NOT_ALLOWED`.
- **Invalid data:** Malformed input or model output fails validation before a tool, delegate, or final result is accepted.
- **Limits and cancellation:** A run that exhausts a budget or deadline ends with a typed, inspectable outcome and stops scheduling new actions.
- **Provider failure:** A configured model failure produces a clear terminal error. The scripted fake OpenCode backend covers contract tests without a live model or a database.
- **Application path:** A user can submit and inspect the run in the browser without manually querying internal stores.
- **Storage path:** A completed run and its events remain inspectable after the Go API restarts. The first pass does not automatically resume a run interrupted mid-execution.

## First-pass limits

Run records are persisted in Supabase Postgres, but in-flight work does not automatically recover after a worker restart. Only read-only tools are supported. Agent definitions are loaded rather than managed through a publish UI. Authentication is limited to local development; no real organization data should be used. The first pass does not claim durable recovery, approvals, write operations, tenant isolation, long-term memory, arbitrary workflow graphs, or isolated extension workers. Those are explicit later-pass goals in [product-spec.md](product-spec.md).

On the OpenCode path, submitted limits must be positive. The wait deadline records `TIMEOUT`. Token and cost are stored from OpenCode session usage and also stop the run: input plus output above `max_tokens`, or cost above `max_cost_usd`, records `BUDGET_EXCEEDED`. The same code stops a run that passes `max_model_turns`, `max_tool_calls`, or `max_delegations`, and the coordinator does not record the action that crossed the cap or any action after it. While the session is still running, the coordinator interrupts it so OpenCode does not keep executing. `max_concurrent_agents` is not a stop condition. `opencode.jsonc` is the execution policy for tools and subagents. This machine's CLI is `opencode v2.1.4`. The live permission check below passed. The event-source migration is applied on the hosted database.

## Completion gate

The first pass is not complete. It is complete when the API, live OpenCode path, scripted contract tests, delegation and tool controls, event inspection, and minimal UI work together from a clean checkout, and focused tests cover the acceptance scenarios above. Scripted contract tests now cover a successful mapping, denied delegation, a disallowed tool, invalid output, the wait deadline (`TIMEOUT`), and `BUDGET_EXCEEDED` for token, cost, turn, tool-call, and delegation caps. `max_concurrent_agents` is still not enforced. The browser flow, a completed run read back after an API restart, and a live model vendor comparison were not re-run. Contract tests use the scripted fake OpenCode backend, not a database-backed deterministic adapter. The README states these operational limits.

Live permission check, 2026-10-24, `opencode v2.1.4` at `/home/ali/.opencode/bin/opencode`. Nothing was listening on `http://127.0.0.1:49374`, and no service password was configured, so `opencode serve --hostname 127.0.0.1 --port 49374 --service` was started with the agents module as its working directory. It logged `server listening on http://127.0.0.1:49374`. `--service` wrote `~/.config/opencode/service.json`; the password is not recorded here. Sessions used that directory so `agents/opencode.jsonc` loaded. `POST /api/session/{id}/permission` then showed:

- `denial-specialist` requesting subagent `catalog-specialist`: `deny`. `GET /api/session?parentID=` that session returned no children.
- `catalog-specialist` shell `cat fixtures/catalog/vendors.json`: `deny`.
- `catalog-specialist` read of `fixtures/catalog/vendors.json`, and of that file's absolute path: `deny`.
- `catalog-specialist` `catalog_search`: `allow` on every check after the one noted below.
- `catalog-specialist` subagent, and `vendor-orchestrator` subagent `general` or shell: `deny`, with no children. `vendor-orchestrator` subagent `catalog-specialist`: `allow`, and that permission request also created no child.

The first `catalog_search` evaluation on session `ses_e8f33927fffe9TsdUKB0b4j0fU` returned `deny`. Rechecking that same session, and every later session (`ses_e8f32f33dffeFF4lTPb8AQzalH`, `ses_e8f32f32fffe4l3V9jveSe9hla`, `ses_e8f32f327ffeXspByvjhBjU2DV`), returned `allow`. Shell, catalog-file reads, and denied subagents were `deny` on every call. The serve process was stopped after the check.
