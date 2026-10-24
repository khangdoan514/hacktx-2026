# First pass: functional agent system

**Status:** planned. This is the first usable implementation of the agent product described in [product-spec.md](product-spec.md). It establishes stable contracts and a working path from request to inspected result. Later passes add reliability, governance, integrations, and authoring features without replacing the core agent model.

## User outcome

A local user can start a run through an API or minimal UI. A depth-0 orchestration agent can invoke two versioned specialists, use an allowed read-only tool, combine structured results, and return a validated answer. The user can inspect run state, the invocation tree, tool outcomes, usage, and errors. A real model provider performs the agent reasoning when configured; a deterministic adapter supports development and repeatable tests.

The initial example is a vendor comparison using checked-in fictional records. It exercises the product capabilities without making live third-party data a dependency. The example is replaceable; the runtime is not vendor-comparison-specific.

## Functional requirements

1. **Definitions:** Load declarative, versioned orchestrator and specialist definitions. Validate their schemas, allowed tools, delegate targets, and limits at startup. Published-version storage and editing are later passes.
2. **Runs:** `POST /v1/agent-runs` accepts schema-valid input, returns a run ID, and starts work asynchronously. Supabase Postgres stores run snapshots and ordered events. GET endpoints expose current state and events while the run is active and after it ends.
3. **Model turns:** A provider adapter returns structured finish/tool/delegate actions. The coordinator validates every action before dispatch. Enforce turn, time, token, cost, tool-call, delegation, and concurrency limits and record provider usage. The deterministic adapter implements the same interface. Add token/cost fields to the shared v1 contract before provider-backed acceptance.
4. **Delegation:** The root may invoke only allowlisted specialists at depth 1. Specialists cannot delegate. Independent specialists may run concurrently. Inputs and results are scoped and schema validated.
5. **Tools:** A central gateway checks the caller's allowlist and schemas, runs a read-only catalog tool with a timeout, and records safe metadata and outcomes.
6. **Inspection:** A run snapshot includes state, final output or typed error, and invocations. Events show model, delegation, and tool progress in run order. The minimal UI can start a run and inspect that history.
7. **Local operation:** Document clean Supabase setup, configuration, test commands, and how to run the API and UI. Provider credentials come from environment or a local secret source, never source control. The deterministic model path works without provider credentials but still uses the configured Supabase database.

## Acceptance scenarios

- **Successful run:** Start a vendor comparison, observe progress, and receive a schema-valid recommendation with source IDs and specialist results.
- **Concurrent work:** Two independent specialists execute within the configured limit and the orchestrator aggregates their outputs regardless of completion order.
- **Denied delegation:** A specialist requests another agent. The runtime returns `DELEGATION_DEPTH_EXCEEDED`, records the denial, and does not create a child.
- **Denied tool:** An agent requests a tool outside its allowlist. The handler does not run; the run records `TOOL_NOT_ALLOWED`.
- **Invalid data:** Malformed input or model output fails validation before a tool, delegate, or final result is accepted.
- **Limits and cancellation:** A run that exhausts a budget or deadline ends with a typed, inspectable outcome and stops scheduling new actions.
- **Provider failure:** A configured model failure produces a clear terminal error; the deterministic adapter can still run the same contract tests.
- **Application path:** A user can submit and inspect the run in the browser without manually querying internal stores.
- **Storage path:** A completed run and its events remain inspectable after the Go API restarts. The first pass does not automatically resume a run interrupted mid-execution.

## First-pass limits

Run records are persisted in Supabase Postgres, but in-flight work does not automatically recover after a worker restart. Only read-only tools are supported. Agent definitions are loaded rather than managed through a publish UI. Authentication is limited to local development; no real organization data should be used. The first pass does not claim durable recovery, approvals, write operations, tenant isolation, long-term memory, arbitrary workflow graphs, or isolated extension workers. Those are explicit later-pass goals in [product-spec.md](product-spec.md).

## Completion gate

The first pass is complete when the API, provider-backed path, deterministic test path, tool gateway, delegation controls, event inspection, and minimal UI work together from a clean checkout. Focused contract and integration tests cover the acceptance scenarios above. The README states the known operational limits clearly.
