# Testing and coding-agent delivery workflow

**Status:** required working agreement for implementation passes. This document describes how a coding agent moves work from a spec to a verified handoff. It applies to the general-purpose Go service and the existing Next.js website. A green test result is evidence for acceptance; the integrator checks that the tests cover the requirement and the joined system works.

## The loop

```text
Read spec and contract
       ↓
Define acceptance checks and failure cases
       ↓
Build the smallest complete behavior
       ↓
Run focused checks → failure? → diagnose → fix → rerun focused checks
       ↓ all focused checks pass
Run package and contract gates → failure? → diagnose → fix → rerun affected gates
       ↓ all package gates pass
Integrate with other packages and website → failure? → fix → retest
       ↓ end-to-end acceptance passes
Review evidence and hand off / finish
```

An agent must not label work complete because code compiles, a mocked happy path passes, or a provider returned one plausible answer. If a test fails, identify whether the code, contract, fixture, environment, or test expectation is wrong. Fix the actual cause, rerun the failed test, then run the relevant package and integration gates. A missing credential or external service may block a live-provider check, but it must be reported as **not run**; it is not a pass.

## Before coding

1. Read the assigned pass in [pass-specs.md](pass-specs.md), the package acceptance criteria in [parallel-work.md](parallel-work.md), and the current [v1 contract](contracts-v1.md) or its successor.
2. Write a short check list mapping each assigned behavior and denied/failure path to a test level. Agree on any contract change with the integrator before implementation.
3. Use fakes only at boundaries owned by another package. Preserve the real public contract and JSON fixtures.
4. Keep deterministic fixtures free of live API calls and real secrets. Provider-backed checks run separately with configured credentials.

## Test levels and ownership

| Level | What it proves | Owner | Gate |
| --- | --- | --- | --- |
| Contract | Go types, JSON wire fixtures, schema compatibility, typed errors | Contract owner; affected package owner updates | Every shared-contract change |
| Package behavior | State transitions, budgets, validation, allowlists, cancellations, ordering, adapter behavior | Package owner | Before package handoff |
| Cross-package integration | Coordinator + model + tools + store + API use the same IDs, states, errors, and event order | Integrator with package owners | Before pass acceptance |
| Website flow | User starts a run and sees loading, progress, result/error, and denied actions through the real API | Website owner + integrator | Before pass acceptance when UI changes |
| Live provider | One configured model returns structured actions through the real adapter; usage and failure handling are visible | Model owner + integrator | Pass 1 and provider changes; credential-dependent |
| Recovery/security/isolation | Restart, concurrency, tenant denial, approvals, uncertain writes, worker boundaries | Owning package + integrator | Passes 3–8 as specified |

Tests should exercise behavior at a public or package boundary. Do not write tests that merely restate a private implementation line. Use stable fixtures and controlled clocks for state/time tests; avoid making routine CI depend on nondeterministic model text or external services.

## Required first-pass checks

The first pass cannot finish until the [first-pass acceptance scenarios](first-pass.md) are covered by tests or a documented browser check. At minimum:

- Valid and invalid run request/response bodies match the versioned fixtures; `GET` works during and after a run.
- Two specialists can complete in either order and produce one valid final result.
- A depth-1 delegation attempt is denied before a child is created, with the expected typed error and event.
- A disallowed tool and malformed tool input do not reach the handler.
- Model actions with mismatched type/payload or invalid final output are rejected before dispatch/completion.
- Turn, tool, delegation, time, token, cost, and concurrency limits stop further scheduling at the boundary.
- Cancellation and provider/tool timeouts end with inspectable state; errors do not leak secrets.
- Events have unique increasing per-run sequence numbers under concurrent writes.
- The website uses the real Go API for its final start/inspect check, including loading, success, and failure states.
- A completed run and its ordered events remain readable after the Go API restarts against Supabase Postgres; interrupted work is not claimed as automatically recovered.
- The deterministic path runs without provider credentials; one separately configured live-provider run verifies the provider adapter.

The Go module currently has only contract tests. `go test ./...` from `agents/` is the present baseline. Add package, Supabase-backed integration, and API-restart tests as code arrives. For website changes, run the existing `npm run lint` and `npm run build` from `frontend/`, then perform the relevant browser flow. If FastAPI is changed as a proxy, run its `pytest` suite and an end-to-end request through the proxy. These commands verify current scaffolds; they do not replace the new acceptance checks.

## Later-pass gates

| Pass | Additional mandatory checks |
| --- | --- |
| 2 | Invalid publish, immutable versions, old-run pinning, website draft/test/publish, import/export round trip |
| 3 | Kill/restart at each checkpoint, duplicate claim prevention, durable cancellation, ordered events and attempts |
| 4 | Cross-tenant denial, policy checks at every action/read, approval identity/expiry/restart, no duplicate uncertain write |
| 5 | Context scope and revocation, memory retention/deletion, retrieval prompt injection cannot elevate permissions |
| 6 | MCP/HTTP/custom tool contracts, worker crash/timeout, resource/network/credential isolation, pinned tool versions |
| 7 | Workflow-to-agent correlation, parallel/join behavior, approval boundary, cancellation propagation, full website workflow |
| 8 | Migration/rollback, Postgres and object restore, load limits, failure drills, trace redaction, service targets |

The detailed acceptance cases remain in [pass-specs.md](pass-specs.md). Add tests when implementing a pass; do not claim Pass 3 recovery passed because Pass 1 persisted a completed run.

## Failure handling and retest rule

| Result | Required action |
| --- | --- |
| Reproducible code failure | Fix code, rerun the failing check, then rerun affected package and integration gates. |
| Contract mismatch | Stop dependent edits, propose one contract change to integrator, update types/fixtures/docs, rerun all affected packages. |
| Flaky or timing-dependent check | Reproduce and remove the race or unstable assumption; do not repeatedly rerun until green. |
| External provider unavailable | Run deterministic and mocked checks, mark live check not run, keep pass incomplete until live acceptance is obtained. |
| Test expectation conflicts with spec | Resolve the spec or test with integrator, record the decision, then rerun. |
| Security or data-leak finding | Fix before handoff; rerun the negative case and related authorization checks. |

Do not weaken or delete a failing test simply to produce green output. If a requirement is intentionally changed, update the spec, acceptance case, and test together.

## Handoff and finish gate

A package owner sends the integrator:

- Implemented requirements and file paths.
- Focused and package test commands with results, plus any checks not run and why.
- Negative/failure cases exercised and fixture or contract changes.
- Known limitations and integration needs.

The integrator then wires packages, reruns the contract/package checks, runs the end-to-end API and website flow, and compares results against the pass acceptance list. A pass is marked **accepted** only when required checks pass, required external checks have actually run, and no known failure contradicts its acceptance criteria. If any check fails, ownership returns to the relevant package or integrator, and the build → test → fix → retest loop continues. Human product review may add requirements, but it does not substitute for these technical gates.
