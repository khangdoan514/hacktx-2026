# Go internal platform

NMC3 is the internal tool for NMC2. One organization, a Hardware division, a Software division, and an AI team inside Software. Employees request a capability from Software/AI, clarify it, review an exact specification revision, run a deterministic check, approve, and hand it over. The pull-request agent reviews code. It is not a public-relations agent. Customers and partners do not get accounts.

This decision replaces the FastAPI application and the Convex hint bus. It does not replace Supabase Auth, Postgres, or private Storage as managed services. The behavior those earlier pieces actually implemented is recorded in [../implemented/](../implemented/).

## Retain

- Next.js as the client, including the dashboard shell already on this branch.
- Supabase Auth when a project issuer, audience, and JWKS are configured.
- Postgres schema `app` for organization and business rows, with the existing migrations left in place.
- Private file bytes served only through the application. Local disk is the stand-in until a Supabase Storage project is exercised.
- The shared agent contracts, now at `internal/agents/contracts`, and the checked-in API fixtures.
- Transaction-local identity, a non-owner `app_runtime` role, and `NOBYPASSRLS`.
- `/api/health` for the dashboard proxy.

## Replace

- FastAPI `/api/v1` business commands, with Go commands for the capability request, specification, approval, and handover state machines.
- HS256 local sessions. They remain available only when `ENVIRONMENT` is `local` or `test`.
- Workspace creation, partner accounts, and email-domain membership. NMC2 membership comes from seed data or an internal invitation.
- The Convex hint bus, with an outbox plus authorized server-sent events and polling.
- The unauthenticated first-pass agent HTTP sketch, with the same `/v1/agent-runs`, run detail, events, and `/v1/agents` shapes behind employee authentication.
- Provider-backed execution, for this phase, with `AGENT_EXECUTION_MODE=dummy`. The mode and adapter version are stored on the run. A failed provider does not fall back to dummy results, because this phase does not call a provider.

## Remove

- Public company signup and partner routes.
- `backend/` and `convex/` once the Go tests cover the retained behavior.
- The nested `agents/go.mod` module. There is one module, `github.com/khangdoan514/hacktx-2026`.
- Dashboard proposal agents, dashboard routes, and dashboard wiring. Those stay untouched in this phase.

## Access rules this phase locks

The Go tests are written to fail if the previous defects come back:

- An approval stores the organization, scope, target kind, target version, and review state. Create and decide both reject a target from another scope, including inside the privileged SQL function.
- Project, file, and run reads require an active employee and an active organization membership. An active project grant by itself is not enough.
- Production startup rejects a missing issuer, audience, or JWKS URL, the secret `dev-only-change-me`, and a development issuer. `ALLOW_LOCAL_AUTH` stays impossible unless `ENVIRONMENT` is `local` or `test`.

## Runtime

`cmd/api` accepts employee commands and enqueues agent work. `cmd/worker` leases that work. Depth 0 may call two allowlisted depth-1 specialists. Specialists cannot delegate. The vendor orchestrator, catalog specialist, and analysis specialist stay as a fixture. The product example is PR review over a checked-in fictional diff. Findings may cite only lines in that diff. Publishing a catalog version requires a passing evaluation and does not allocate hardware or grant a write tool.
