> **HISTORICAL / SUPERSEDED PLAN:** Read [current decisions](../decisions.md) and [current handoff](../prompts/01-current-implementation-handoff.md). The dashboard freeze and OpenCode/scripted-adapter direction override conflicting instructions below. Findings remain pinned to their stated snapshot.

# Cursor branch review — NMC3

Reviewed 24 October 2026: `cursor/dev-0d85` at `12beedc14d00053947d8bb9f1c6501c76c3e2f02`, [PR #3](https://github.com/khangdoan514/hacktx-2026/pull/3), based on `main` at `df5575f13f86b790c872406a24fe77174832ea58`. The PR is open. This is a review of that snapshot, not a certification of a deployed system.

The branch contains useful backend foundations, but should not be treated as ready for real NMC2 data. Its current product model also needs to change to the internal cross-team capability-management scope in the accompanying Cursor plan.

## Reproduced security findings

### 1. P1 — approval can mutate a target in another workspace

`create_approval` checks access to the project named in the request URL, then stores the supplied target ID without checking that the target belongs to that project. The privileged `app.apply_approval_target` function updates by target ID and revision without matching the target's workspace/project to the approval.

In an isolated local database, I created two unrelated workspaces. A reviewer in workspace A received 404 when reading a requirement in workspace B. Nevertheless, creating an approval under project A for that requirement returned 201, approving it returned 200, and the private requirement in B changed to `accepted`.

Fix both admission and decision: validate target type, existence, workspace, project, state, revision, and current decision authority. Enforce the same relationship inside the privileged function. Bind the approval to an immutable reviewed version and appropriate approver policy. Do not rely on opaque UUIDs to protect objects. Add this negative API test and a direct runtime-role database test.

Evidence: [approval creation, lines 755–785](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/backend/src/app/domain/work.py#L755), [privileged target update](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/supabase/migrations/20261024163000_dashboard_provenance.sql#L20).

### 2. P1 — removing company membership leaves project access active

`update_member` changes the workspace membership and emits a revocation event. It does not remove project memberships. Both `require_project` and the RLS `project_role` helper accept the surviving active project membership without requiring active workspace membership. The outbox recipient check has the same project-only assumption.

Reproduction: removing a workspace member returned 200; the same bearer identity subsequently read its former project with 200. An internal-only company still needs revocation between departments, projects, and employment states.

Fix authorization as an intersection of active employee, organization membership, resource grant, and action permission. Apply it to reads, mutations, approvals, files, events, and notification delivery. Recheck current access at the time of a decision. Historical assignment must not preserve authority after removal.

Evidence: [membership update](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/backend/src/app/domain/tenancy.py#L133), [project check](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/backend/src/app/domain/common.py#L78), [RLS project helper](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/supabase/migrations/20261024120000_app_baseline.sql#L613).

### 3. P1 — production accepts tokens signed with the shipped fallback secret

The production configuration permits `jwt_secret="dev-only-change-me"`. `ALLOW_LOCAL_AUTH=false` disables the local login endpoint but does not disable the HS256 bearer verifier. With that configuration, I signed a token for an existing active test account and received 200 from `/api/v1/me` while local login was disabled.

This is conditional on deploying without replacing the default; I did not probe any real deployment. Fix it by failing startup on missing/unsafe production authentication configuration and separating the test issuer from the Supabase verifier. Require expected issuer, audience, subject, and expiration; support the configured Supabase signing algorithm and key rotation. Production must reject test-issued tokens.

Evidence: [configuration defaults](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/backend/src/app/core/config.py#L8), [bearer verification](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/backend/src/app/core/auth.py#L64).

## Integration and product gaps confirmed by inspection

- **Supabase authentication is incomplete.** FastAPI accepts only HS256, while the Convex configuration expects RS256/ES256. New Supabase identities have no implemented profile-provisioning path outside local login. Frontend API calls use a separately copied access token without synchronization to refreshed Supabase sessions; logout clears only that copy. The callback page is informational. These require end-to-end Auth tests, not just the current test JWT issuer. [Auth code](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/backend/src/app/core/auth.py), [sign-in and callback](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/frontend/src/product/screens.tsx#L44).
- **Documents always use local disk.** `files.py` reads and writes `STORAGE_DIR`; it does not switch to Supabase Storage when `SUPABASE_URL` is set. Private bucket configuration alone does not implement uploads, downloads, or object authorization. [File implementation](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/backend/src/app/domain/files.py#L19).
- **Many routes are functional placeholders.** Approval and handoff detail pages render `EntityScreen`, which displays JSON and has no decision/acknowledgment interaction. Some document, metric, dataset, and question detail paths fetch the collection rather than the selected object. Counting those routes as completed workflows would be misleading. [EntityScreen](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/frontend/src/product/screens.tsx#L458).
- **Convex delivery exists but browser subscriptions do not.** Architecture docs acknowledge this. Keep it optional; finish the request workflows before making live hints a dependency. Outbox dispatch currently runs synchronously after business writes, so delivery failures need an independent retry worker. [Architecture](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/docs/architecture.md), [write wrapper](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/backend/src/app/api/routes/v1.py#L259).
- **The Go roadmap intentionally postpones identity/policy to a later pass.** Its first anonymous/read-only slice must remain a fictional-data development environment. The internal product must not send company repository content or documents to it before authenticated, scoped integration is implemented. [First-pass boundary](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/agents/docs/first-pass.md), [governance pass](https://github.com/khangdoan514/hacktx-2026/blob/12beedc14d00053947d8bb9f1c6501c76c3e2f02/agents/docs/pass-specs.md).

## Verification performed

I retrieved the pinned branch files, inspected application/domain code, migrations, product routes, Convex handlers, and agent security/architecture documents. I ran the branch's backend suite against a fresh, isolated local PostgreSQL 17 database: **16 passed**. I then ran the three additional API reproductions described above. The temporary database server was stopped afterward.

The branch's Supabase configuration targets PostgreSQL 15, so this local run is not a target-version compatibility test. I did not rerun frontend/Convex builds, exercise a hosted deployment, use real organizational data, or verify real Supabase Auth and Storage. Passing the existing suite does not cover the reproduced missing authorization cases. No repository code, PR comments, or live infrastructure were changed.
