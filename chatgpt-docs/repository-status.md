# Repository status and evidence

This page separates observations from intended behavior. All dates are 24 October 2026 unless stated otherwise.

## Pinned snapshots

| Snapshot | What was observed |
| --- | --- |
| `f0f0b1fd6467914e5b2424c49093f02283183e8b` | Original comprehensive audit baseline; now historical |
| `df5575f13f86b790c872406a24fe77174832ea58` | Main added agent roadmap, shared Go interfaces/types, fixtures and contract tests |
| `12beedc14d00053947d8bb9f1c6501c76c3e2f02` | Reviewed Cursor PR #3: Python business API, Supabase-oriented SQL, product pages and Convex hint code |
| `c1a4fb603a73d84f46d07c3fcc8a20045377cb43` | Main at documentation preparation: PR #5 removed custom runtime/model/tool engine in favor of planned OpenCode integration |

The branch security review is pinned to `12beedc`, not the newer main. Do not assume its bugs were fixed or remain present elsewhere without checking the relevant code.

## Current main's execution state

The inspected `agents/cmd/nmc3-agent/main.go` calls a disabled execution implementation that returns a typed error for run submission. It serves persisted history and definitions through the Go HTTP service. It uses Supabase RPC storage by default with optional direct Postgres. The custom runtime/model/tool packages were removed. **OpenCode-backed execution is planned, not demonstrated by that entrypoint.**

Main now has richer contracts than the original snapshot: model selection/usage/tool-invocation additions and a `ModelAdapter.Next` returning `ModelResult`. The old prompt's exact signatures cannot be copied blindly.

The existing website uses `/agent-api/*` in the agent setup documentation. Reconcile this with business `/api/v1` and canonical agent `/v1` routes; preserve frozen consumers.

## Documentation inconsistencies discovered

`agents/README.md` accurately announces the history-only state but still contains setup passages for removed deterministic/live adapters. `opencode-adoption.md` says the engine is deleted while later phase/revert passages still mention keeping it behind a flag. The contract document retains custom-coordinator semantics during the migration.

These are inconsistencies to reconcile with the agent owner in implementation work. This documentation publication does not edit those files or pretend the absent fallback exists. The latest user decision is to follow OpenCode with a scripted interim adapter, not restore the engine.

## Verification actually performed earlier

On the pinned Cursor snapshot, the existing backend suite passed 16 tests using a fresh isolated local PostgreSQL 17 cluster. Additional API probes reproduced cross-workspace approval mutation, post-removal project read access and default-secret production token acceptance. The temporary DB server was stopped. The configured target Postgres version was 15, so that run did not prove target-version compatibility.

No hosted Supabase Auth/Storage validation, production probing, live OpenCode enforcement test, frontend/Convex build rerun or complete security assessment was performed for that review. See [the historical report](archive/cursor-branch-review.md) for source links and exact limits.

## Documentation publication boundary

Only `chatgpt-docs/` files are added by this publication. No code, dashboard, migration, secret, deployment or existing agent document is changed. Content has been organized and reconciled, but repository implementation and runtime behavior remain as they were before the documentation commit.
