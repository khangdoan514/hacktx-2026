# Security requirements and regression priorities

Internal-only does not mean unrestricted. NMC2 still needs boundaries between employees, teams, repositories, budgets, artifacts and execution identities. These are engineering requirements, not a certification claim.

## Known reviewed defects

The historical Cursor snapshot `12beedc` was tested against an isolated local PostgreSQL database. Its 16 existing backend tests passed, but three additional API checks reproduced failures:

1. An approval in one workspace changed an unreadable requirement in another workspace to accepted.
2. Removing workspace membership left project reads authorized.
3. Production mode with local login disabled accepted a forged token when the shipped default signing secret was used.

These findings apply to that pinned snapshot, not automatically to today's main or a live deployment. See the [full historical review](archive/cursor-branch-review.md). Add regression tests to the Go implementation and every equivalent API path; do not merely hide the vulnerable UI action.

## Identity

Verify the actual Supabase signing scheme with trusted issuer/audience, required subject/expiry and bounded key-rotation behavior. Reject development issuers and missing/unsafe production configuration. Never follow token-supplied arbitrary key URLs. Frontend refresh/callback/logout must use a coherent session integration.

A verified profile is not an NMC2 employee grant. Provision membership from authorized employee/invitation records; do not grant access solely from user-editable metadata or an email-domain suffix. Check current membership and session validity where sensitive revocation requirements demand it. Deleting an identity does not by itself prove all issued access tokens are invalid.

Developer/service credentials require explicit scope, expiry/revocation and audit identity. Browser, CLI and SDK enforce the same permission matrix. Never provide database or Supabase service-role credentials to end users.

## Authorization and decisions

Intersect active identity, organization membership, resource grants, action permission and current policy. Recheck at read, execution admission, context resolution, decision and consequential action. Scope search, counters, events, exports and notifications; previews must not leak inaccessible titles or data.

Bind approvals to exact target organization/project/type/revision/hash, reviewer authority, expiry and state. Lock/CAS decision and target atomically. Reject self-approval, foreign targets, old evidence, removed reviewers and mismatched idempotent retries. Task completion/publication routes must not bypass required review. Historical recipient/assignee fields do not preserve access after revocation.

## Database and privileged functions

Direct SQL runtime roles must be non-owner and non-BYPASSRLS. Request identity is transaction-local; tests must prove no leakage through pooling. RLS grants and policies are separate controls. Update policies require appropriate visibility/check semantics. Views must not accidentally bypass intended row policy.

Review every privileged function for safe search path, restricted execute grants, current caller identity, resource scope and exact target checks. A privileged service/RPC path needs explicit authorization; do not call it RLS-protected when it bypasses the end user's policies.

## Agent/tool trust boundary

User inputs, repository diffs, retrieved context, prompts, model/tool results and external events are untrusted data. None may add permissions, choose arbitrary credentials, approve its own action, change budgets or bypass decisions. Translate the approved envelope into OpenCode controls and test the actual installed runtime.

Begin with read-only allowlisted tools. Block arbitrary shell/network/URL access and SSRF paths. Scope MCP/plugin operations and sandbox any future code execution before enabling it. OpenCode adoption does not remove NMC3's responsibility to authorize who may start/read/control a session.

Context is selected by purpose and current grant. Do not forward a parent transcript or entire organizational corpus automatically. Validate outputs and sources before storing/applying structured results. A source ID is not proof of access.

## Data, secrets and artifacts

Keep secrets in approved server-side credential mechanisms; specs contain references only. Redact tokens, private context and raw secrets from logs/events. Private Storage calls using a bypassing credential must be authorized by the Go application. Bound uploads before buffering, verify finalization, and document short-lived link revocation limits.

SSE and event replay require current authorization, including after a subscriber loses membership. Stop delivery and reject subsequent reads. Outbox workers must recheck recipients and avoid leaking body previews after access changes.

## Operational controls

Bound requests, execution queues, concurrency, retries and deadlines. A retry after an uncertain external write/submission needs reconciliation. Releasing a budget reservation requires an evidence-based outcome. Distinguish admission controls from runtime enforcement and measured usage from estimates.

Keep attributable append-only audit metadata. Record policy/spec/backend versions, correlation IDs and decision reasons. Separate raw payload retention from audit retention. Test database/object restoration, graceful shutdown, worker leases, duplicate-event handling and interrupted-run outcomes.

## Security release gate

Before live private repository/company data reaches OpenCode, demonstrate authenticated session isolation, scoped source/tool access, prompt-injection denial cases, safe secret handling, output/event filtering, revoked-access behavior and required limit enforcement. Scripted adapter tests are necessary for application behavior but insufficient evidence for those live execution guarantees.
