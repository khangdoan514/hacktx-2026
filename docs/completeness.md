# Project completeness

Observation of main at `51dd87f` (25 October 2026), after pull request #12. This page records what the code does. It does not change the requirements in [chatgpt-docs](../chatgpt-docs/README.md). Those documents remain the backlog. `TestEmployeeJourney` in `internal/productapi/flow_test.go` is the best single proof of what a pull request can claim today.

## Two products in one repository

A capability request and a console run do not share a definition or a database.

| Plane | Entry | Persistence | Execution |
| --- | --- | --- | --- |
| Business lifecycle | `cmd/api` → `internal/productapi` | Schema `app` in `supabase/migrations/20261024190000_nmc2_internal.sql`, plus business `nmc3` runs and jobs | Dummy adapter in `internal/agents/runtime`. `Start` refuses any mode other than `dummy`. |
| Agent console | `agents/cmd/nmc3-agent`, reached through `/agent-api/*` | Separate database, also schema `nmc3`, from `agents/db/migrations` | OpenCode coordinator in `agents/internal/opencode` |

`cmd/worker` leases jobs for the dummy engine. The console calls `/agent-api`. The request screens call `/api/v1`. Compose starts both processes. `AGENT_EXECUTION_MODE=dummy` applies to the business API. The agent process expects a host OpenCode service.

## What already holds

`TestEmployeeJourney` walks the business state machine with the three local employees.

| Person | Email | Roles | What the journey uses them for |
| --- | --- | --- | --- |
| Ada Hardware | `ada.hardware@nmc2.test` | `requester` | Draft, submit, answer, acknowledge, fulfill. She can record a budget request. She cannot triage, publish, or start a console run. |
| Bao AI | `bao.ai@nmc2.test` | `ai_intake`, `ai_engineer` | Triage, specify, open testing, record an evaluation, start the dummy review. He cannot publish, and he cannot approve his own specification. |
| Cam Review | `cam.review@nmc2.test` | `release_approver`, `administrator` | Approve, publish, remove a member. Administrator also lets him approve a budget or a hardware request. |

The password in local and test is `local-demo-pass`. The schema also defines `team_lead`, `hardware_owner`, `security_reviewer`, `budget_owner`, `operator`, and `auditor`. Nobody is seeded into those jobs.

The journey checks these behaviors:

- Ada drafts and submits. Bao triages, asks which repository is in scope, and accepts the answer. Answering returns the request to triage, so intake triages a second time.
- Bao cannot approve his own specification. Cam approves that exact revision. A second different decision on the same approval conflicts. Replaying the same idempotency key returns the original decision.
- A material edit of the instructions calls `app.note_material_revision`. That supersedes pending and approved specification approvals, stamps `invalidated_at` on every evaluation for that specification, and moves catalog releases in `draft`, `testing`, or `eligible` back to `testing`. A release already in `published` stays published.
- Publish is refused until an evaluation row exists. The published release reports that it allocated no hardware and granted no write tool.
- Removing Ada blocks her next read and her next run, through `app.nmc_actor_active`.
- An approved budget stores the amount and leaves `usage_amount` unset.

On the console, these surfaces talk to the agent process and render what it stored: the run table, the agent drawer (spec, activity, prompts), the visual builder, tool packs and the tool registry, threads, and the live inspector (graph, timeline, limit meters, cancel, and approvals on that run). OpenCode is configured deny-by-default. `agents/opencode.jsonc` sets `subagent_depth` to 1, so a specialist on that path cannot start another agent. GitHub and Drive are connectable packs whose credentials live on disk, not in the definition.

The older production-auth bugs are closed in the current Go API. A production config rejects the dev JWT secret and refuses HS256 outside `local` and `test`. Anonymous access is limited to health, readiness, and local login. Cross-scope approval checks live in the NMC2 SQL.

## How a run gets lost between the two planes

The request page posts “Run PR Review Assistant” to `/v1/agent-runs`. Next.js forwards `/api` and `/agent-api` only, so that browser call never reaches Go. The same screen then links `/live-run/<id>`. The inspector loads `/agent-api`, which reads the agent database. The dummy worker writes the business database.

Starting a console run has the opposite constraint. `POST /v1/agent-runs` on the agent process only accepts an id loaded from `agents/examples/definitions.json` at startup. The session is opened under that same id, which has to exist in `agents/opencode.jsonc`. The builder’s Publish button writes the definition registry when `NMC3_DATABASE_URL` is set. `definitions.ResolveAndPinRun` is implemented and tested. Start does not call it.

The tested review is `pr-review-orchestrator` over `agents/fixtures/pr-review/sample.diff`. The console roster is the vendor orchestrator, the catalog specialist, the accessibility specialist, and the denial pair. The Idea Consultant described in `FRONTEND.md` is in neither list. `ml/` is unused by either service.

The journey fulfills the request, then opens the catalog, then starts the dummy review. The run row has no `request_id` or `catalog_id`. The handover body is the specification id and revision. The test proves the state machine. It does not prove “publish this version, then run it for this request.”

## Rules that differ by engine

| Rule | Dummy engine (`internal/agents/runtime`) | OpenCode (`agents/internal/opencode`) |
| --- | --- | --- |
| Delegation | `MaxDelegationDepth` is 2. An agent at depth 0 or 1 may delegate. Depth 2 is refused. The scripted review stops at two depth-1 specialists, so the journey still passes. | `subagent_depth` is 1. A specialist cannot delegate. The product rule is this one. |
| Budgets | Turns, tools, and delegations are enforced in process. Provider usage is zeroed. | Turn, tool, delegation, token, and cost caps are polled and can interrupt the session. `max_concurrent_agents` is stored and validated on a definition, and is not a stop condition. |
| Tool denial | Gateway allowlists. | `opencode.jsonc` plus transcript mapping. |
| Approvals | Not on the engine. | The inbox is mounted when the agent database is configured. The coordinator does not call `policy.Engine`, so a decision there does not block a tool or a delegation. |
| Definitions | `dummy.LoadDefinitions()` | File at startup. Registry is a second store. |

There are two contract packages, `agents/internal/contracts` and `internal/agents/contracts`. `StateWaiting` lives on the agent copy.

`app.create_scoped_approval` is narrower than the role list. The caller must be an active `ai_engineer`. The reviewer must be an active `release_approver`, and must not be the caller. A `security_reviewer` grant does not satisfy that function. Hardware and budget rows in `app.resource_records` are never read by specification approval, catalog publish, or `Engine.Start`.

Evaluation is a state change plus an attestation. `StartTesting` moves `draft` to `testing`. `RecordEvaluation` inserts the client’s `passed` boolean and evidence text. Nothing executes the specification’s evaluation suite.

Packages that exist and are not called by the process that starts runs:

| Package | What it contains | Wired into `nmc3-agent` |
| --- | --- | --- |
| `agents/internal/durable` | Attempt journal, leases, restart proofs | No. Completed history survives. In-flight work does not resume. |
| `agents/internal/context` | Resolver, grants, retrieval | No. |
| `agents/internal/workflownode` | Invoke, cancel, join, HTTP handler | No. There is no workflow engine. |
| `agents/internal/extensions` | Registry, gateway, HTTP API, separate worker | The HTTP API is mounted. Agent runs use OpenCode MCP, not this gateway. |
| `agents/ops/metrics` | Limit and log helpers, with tests | Not imported by `main`. |

## Website

The rail is Dashboard, Threads, Agents, Tools, and Settings. `/` redirects to `/dashboard`, so `Landing` is not what the root serves. Prompts survive inside the agent drawer. Monthly budgets, an evaluation suite, and triggers do not. Threads are OpenCode chats whose list is kept in `localStorage`. They are not the clarification thread on a request.

`/requests` and `/requests/[id]` are the lifecycle screens. They are a button sequence: one canned specification, a hardcoded reviewer id, and “Record evaluation” sending `passed: true`. Every signed-in person sees every button. The API rejects the wrong role after the click. The profile menu links to `/org`, and `/org` redirects to `/settings`. The organization screen component is unused. `/sign-in` uses the Supabase employee session. An older local-password sign-in component remains in `frontend/src/product/screens.tsx` and is not what that route renders. The tool registry treats a 404 as unwired and stays on the Tools page.

`FRONTEND.md` and `docs/design/relay-console.html` specify a different homepage: a four-number strip, an idea river, decisions beside an agent setup card, and SerpAPI visible in a scout step. Those components are not in the tree. The shipping console is the run table.

About forty routes under `/w/…` are an earlier client. Partner, workspace, and onboarding pages redirect toward settings. The remaining project pages call `/api/v1/workspaces/…`. The Go server does not register those routes. Several detail pages fetch a collection and ignore the id in the URL (documents, metrics, datasets, questions, dashboard revisions). My-work, inbox, and audit exist as lists in that shell and are not on the rail. Fixing those id bugs before deleting or replacing the shell would preserve a client for an API the server no longer has.

## Data, tests, and operations

Both databases use a schema named `nmc3` with different `runs` tables. The business database holds the dummy worker queue. The agent database holds OpenCode history, definitions, policy, the durable journal, and context. Pointing either process at the other URL fails in confusing ways rather than at startup.

| SQL | Role |
| --- | --- |
| `supabase/migrations` | Canonical business schema. The API applies it on local and test startup. |
| `agents/db/migrations` | Agent database. Applied by the agent setup script. |
| `db/migrations/001_init.sql` | Superseded stub. Do not apply it. |

`20261024120000_app_baseline.sql` still defines the older workspace and project tables. The current Go API does not reference them.

Other boundaries:

- If `NMC3_INTERNAL_TOKEN` is empty, the agent process serves `/v1` with no caller check. The business proxy stamps the employee id and roles into headers after it has checked the session. Anyone who can reach the agent port with that shared secret can present any employee. The OAuth callback for tool packs is unauthenticated on purpose. Its check is the single-use state value.
- When the agent store is Supabase, it uses the service secret and does not run as the employee. Tenant isolation then depends on the Go checks in front of it.
- Document bytes go to `STORAGE_DIR` on disk. Row-level security covers `app.private_files` and does not cover that directory. `internal/documents` has no tests.
- In `local` and `test`, a request with `X-NMC3-Console: 1` and no bearer token is treated as Bao. That path is available only when `ENVIRONMENT` is `local` or `test`.

About 184 Go tests exist. The agent packages pass without a database. The product journey, RLS, and approval tests need Postgres. There is no `.github` workflow running them. `requests`, `capabilities`, and `documents` have no package tests of their own. The journey test is their coverage. Hosted Supabase Auth and Storage have not been exercised. Session refresh against a hosted project has not been run.

## Documents that will mislead the next change

| Document | What it still says | What the code does |
| --- | --- | --- |
| `chatgpt-docs/repository-status.md` | The agent entrypoint is history-only and errors on submit. | `agents/cmd/nmc3-agent` creates an OpenCode session. |
| `README.md` | Dummy execution, and also a host OpenCode service. | Both are real, on different processes. |
| `frontend/README.md` | `/evaluate`, `/triggers`, `/budgets`, `/guardrails`, and `simulate.ts`. | Those routes redirect. `simulate.ts` is gone. |
| `agents/internal/policy/README.md` | The approval handler is not mounted. | It is mounted when the agent database is set. Execution still does not call it. |
| `agents/docs/contracts-v1.md` | The agent API is unauthenticated. | Integrated calls pass through `cmd/api`. The raw process is unauthenticated only when the internal token is unset. |
| `docs/implemented/platform.md` | The old FastAPI JWT defect. | The Go API rejects that secret outside local and test. |

`chatgpt-docs/` is still the right description of the product that was asked for. It says it is requirements, not a status report. Use this page for status.

There is no CLI and no SDK. `CodeHint` can show a CLI and a TypeScript snippet, and no page passes it one. `curl` with the employee bearer token, as the root README describes, is the developer client. Schedules are implemented: `agents/internal/automations` stores a run request with a five-field cron schedule, a loop in the agent process starts it through the coordinator as the employee who saved it, and `/automations` in the console lists automations, their firings, and the runs they started. Without `NMC3_DATABASE_URL` schedules live in memory until restart. Webhooks are not implemented.

## Order to finish

1. One database target per process, checked at startup, and one run row that stores the request id and the pinned definition version. Call `ResolveAndPinRun` from start. Point the request screen and the console at `/agent-api` and that same history, so the live inspector opens the run the request started.
2. Make the dummy depth constant match OpenCode: a depth-1 agent cannot delegate. Enforce `max_concurrent_agents`, or stop accepting it as a limit.
3. Keep the evaluation invalidation that already exists. Make `passed` come from a suite that ran against the pinned version. When the specification’s material revision changes, move a `published` release back as well.
4. Make catalog publish, resource approval, and run start read each other. Seed a hardware owner and a budget owner when those reviews are visible, so administrator is not standing in for both.
5. Put Requests on the rail. Replace the button page with a draft, a specification form, an approval that lists real reviewers, and a handover the requester can read. Show each person only the actions their role can perform.
6. Add templates, reuse, my-work, teams, documents, and an audit view on that same lifecycle. Retire `/w/…`. A CLI on the same bearer token can follow. An SDK waits until that CLI is the one worth wrapping.
7. Mount durable recovery, the context resolver, and workflow-node HTTP only after a published definition is what OpenCode executes. Leave webhooks off until signature checks and an owner exist.
8. Add a Postgres job for the journey test before calling any of this done. Require `NMC3_INTERNAL_TOKEN` whenever the agent listens beyond localhost.

The idea river, the command palette, the KPI strip, and the SerpAPI scout step in `FRONTEND.md` are a separate demo narrative. Building them now would be a third frontend. If that demo is still wanted, it should be a view over the same runs, after step 1, and SerpAPI should appear only on a step that called it.

Do not restore the deleted Go model engine. Do not mark dummy runs as model usage. The zero-token result in the journey test is the correct behavior for the scripted adapter.
