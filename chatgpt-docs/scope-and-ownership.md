# Scope, removal inventory and dashboard freeze

## Absolute dashboard freeze for this phase

Another contributor owns the dashboard. Do not create, edit, restyle or wire dashboard screens in either route group. Do not implement dashboard-assistance agents, dashboard proposals or apply/publish flows now. Preserve existing dashboard data, files and behavior. Future requirements can be documented only.

Protected paths supplied in the Cursor handoff:

```text
frontend/src/app/(dashboard)/
frontend/src/components/AppShell.tsx
frontend/src/components/PageHeader.tsx
frontend/src/components/StatusTag.tsx
frontend/src/components/WorkflowTile.tsx
frontend/src/components/BackendStatus.tsx
frontend/src/theme/theme.ts
frontend/src/lib/mock-data.ts
FRONTEND.md
docs/frontend-design.md
docs/design/
```

Also preserve behavior and appearance of `frontend/src/app/layout.tsx` and `frontend/src/app/providers.tsx`. Product work belongs in `(product)/` and `frontend/src/product/`, except that their dashboard routes are frozen too. Route groups do not provide distinct URL namespaces; verify that new routes do not collide.

Shared files include `frontend/package.json`, its lockfile, `frontend/src/lib/api.ts`, and rewrite configuration. Necessary changes must preserve existing consumers. Keep compatibility endpoints/proxy routes when the Go migration would otherwise break a frozen component. A dependency upgrade can change the dashboard even if no protected file is edited; check the existing page's behavior after shared changes.

The existing `/runs` UI may itself be inside the frozen group. Do not modify it to satisfy the new vision. Integrate through compatible backend contracts or build approved non-dashboard product routes without collision. Defer any change requiring editing the frozen screens until the user reopens that scope.

## Remove or replace during implementation

| Remove from active application | Replacement |
| --- | --- |
| Python/FastAPI business server, Python worker and backend-only startup/dependencies | Go API and workers after parity/security gates |
| Convex backend, auth config, functions, deployment/env wiring | Go outbox/inbox plus authenticated SSE or polling |
| Public signup with automatic organization access | Verified company identity plus explicit authorized employee membership |
| Create-company/workspace flow and arbitrary workspace ownership | One provisioned NMC2 organization, division/team navigation |
| External partner invitations, portal and guest/partner mutation endpoints | Internal reference records where useful |
| Generic project/partner sales management as primary navigation | Capability lifecycle and cross-team delivery |
| Hard-coded successful runs, random metrics, fake timestamps | Persisted scripted execution and actual recorded events |
| Raw JSON placeholders masquerading as completed detail/action pages | Selected-record product interactions |
| Production default JWT secret and anonymous application routes | Fail-closed verified identity and resource checks |
| Production local-disk documents | Private Supabase Storage |
| Plans to restore the custom runtime deleted on main | OpenCode execution integration with interim scripted adapter |

Remove obsolete API access, not only buttons. External partner routes remaining callable would contradict internal-only scope. Preserve unrelated `ml/` experiments. Do not delete frozen mock-data files in this phase; remove new work's dependency on them and coordinate eventual cleanup separately.

## Retain and adapt

Retain Supabase, useful transactions/RLS/revision checks, audit/outbox patterns, requirements, documents, scoped approvals, handovers and delivery tasks. Retain current canonical agent contracts, compatible `/v1` shapes, persisted history and existing agent migration lineage. Fix known authorization defects rather than porting them faithfully.

Do not reset a database, drop legacy data, rename established IDs or rewrite applied migrations to simplify the new model. Disable obsolete flows, map relevant records, migrate additively, and retire old tables only once dependencies are gone and data handling is approved.

## Workstream boundaries

This workstream supplies Go application/business capabilities, secure API contracts, Supabase integration, nontechnical product workflows, and the bounded execution-adapter seam. CLI/SDK implementation is a separate deliverable unless explicitly assigned; API readiness is required now. OpenCode integration is coordinated with the agent owner. Do not revive or maintain an alternative custom engine to avoid that coordination.

The proposed root module consolidation is an implementation option, not permission to break an actively changing nested agent module. Decide module/package ownership with the current tree in view. Avoid importing a sibling's Go `internal` package illegally or cloning contracts into divergent copies. Update canonical imports, fixtures and docs together if relocation is chosen.
