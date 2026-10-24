# Current implementation handoff for Cursor

Continue the existing NMC3 workstream without resetting the branch. Fetch current main and reconcile its changes before editing. Read `chatgpt-docs/README.md`, `decisions.md`, `scope-and-ownership.md`, `architecture.md`, and the current agent contracts/docs. Archived prompts are historical and do not override this handoff.

NMC3 is NMC2's internal capability lifecycle platform. NMC2 offers GPUaaS and has Hardware/Software divisions, with AI inside Software. PR means pull request/code review. A team asks Software/AI for an agent capability, clarifies requirements, approves a versioned scope, evaluates it, publishes an eligible version and receives a structured handover. PM and BA may be the same person; functions do not automatically grant permissions.

The bigger lifecycle is request → configure → evaluate → approve → publish → trigger/run → observe → revise. Developers use CLI/SDK clients; BAs and other less technical employees use the website. They share one Go application API and the same identity, policy, definitions, decisions and history.

## Non-negotiable scope

- Keep Next.js as frontend and Supabase Auth/Postgres/private Storage as managed infrastructure.
- Migrate NMC3-owned Python/FastAPI business code and Convex backend functions to Go after parity/security gates.
- Follow main's OpenCode execution direction. Do not restore the deleted custom coordinator/model/tool engine.
- Use a bounded scripted dummy execution backend meanwhile. Label it clearly in code, developer logs/docs and persisted provenance; no website dummy badges and no fabricated live-model/hardware/external-action claims.
- Preserve all dashboard files and behavior now, including product dashboard routes, dashboard adapters/proposals/wiring and shared-file compatibility. Consult the exact frozen path list in `scope-and-ownership.md`.
- Remove public company/workspace creation, automatic organization access and external partner portal/accounts/endpoints. Provision one NMC2 organization and internal team membership.
- Do not implement the entire CLI/SDK unless explicitly assigned. Prepare documented client-independent contracts and API-only acceptance now.

## Build in order

1. Reconcile current tree, contracts, migration ledgers and module ownership. Preserve upstream OpenCode/store/history work and frozen frontend paths.
2. Establish the Go application, trusted Supabase identity, current grant checks, safe transaction role, errors, audit/outbox and private-file lifecycle. Port retained tests.
3. Complete request draft/submission/intake/clarification/specification/approval/handover workflows through real product interactions and API commands.
4. Add shared versioned capabilities for evaluation, trigger admission, budgets, guardrails, prompts and observation. Use backend rules rather than browser-only checks.
5. Add the scripted execution adapter and persisted scenario/evidence flow. Preserve compatible vendor fixtures and add PR-review fixtures. Keep actual integration facts truthful.
6. Integrate OpenCode through a narrow REST adapter with verified controls, session mapping, safe event persistence and uncertain-operation reconciliation. Coordinate this with its owner.
7. Complete catalog/release/team assignment/resource/budget workflows. Preserve separate requested vs approved vs measured values.
8. Cut over retained product calls to Go; remove obsolete Python/Convex runtime paths and env/docs. Keep unrelated ML work. Do not delete frozen mock-data files or break their consumers.

## Required corrections to earlier prompts

Do not assume `12beedc` or `df5575f` is still the branch head. Do not insist on one migration after an old filename. Do not copy the old `ModelAdapter.Next` signature or restore removed packages. Do not add a conflicting `/v1` proxy while ignoring the current `/agent-api` consumer. Do not relocate the canonical Go `internal` package without updating imports/fixtures and preserving current owner work.

Add negative regressions for cross-scope approval mutation, post-removal project/file/run access and default-secret production tokens. Bind decisions to exact target/revision/current authority and ensure generic completion commands cannot bypass approval. Implement refresh/callback/sign-out, not just token signature parsing.

## Completion evidence

Follow `delivery-and-testing.md`. Require actual forms, selected-object detail views, authenticated API-only flows, meaningful Go/race/integration checks, scripted execution provenance, and real OpenCode checks before claims about live enforcement. A plain Postgres run does not prove Supabase Auth or Storage. Report unavailable checks explicitly. Keep dashboard code/behavior frozen.

Document final removals, migrations, setup, compatibility, actual verification and remaining limits. Implementation PR title/body should reflect the final Go/internal/OpenCode direction. Do not incidentally deploy or merge implementation work. The separate authorization to publish this documentation on main does not authorize a code deployment.
