# Open decisions and dependencies

These items need evidence or an owner before the corresponding implementation is enabled. They do not block documenting or building independent product flows.

| Item | Required decision/evidence | Suggested owner |
| --- | --- | --- |
| Employee identity | Approved IdP/invitation source, employee lifecycle and revocation policy | Platform / NMC2 owner |
| CLI login | Supported interactive flow, credential protection and revocation | Developer experience / identity owner |
| SDK scope | Initial language, packaging, generated schemas and release ownership | Developer experience |
| Module layout | Root module vs preserved nested service, canonical contract ownership | Backend + agent owners |
| Store access | Direct Postgres vs restricted Supabase RPC privileges and tenant checks | Backend / database |
| Migration ledger | Reconcile `supabase/migrations` with agent migrations and deployed state | Database owners |
| OpenCode version/API | Installed supported API, auth, permissions, cancellation and events | Agent integration |
| Hard runtime limits | Which controls are enforced before action; unsupported-limit handling | Agent + governance |
| Evaluation rubric | Case definitions, judgments, thresholds and release policy | AI team + requesting team |
| Budget policy | Currency/period, reservation model, approval thresholds and true telemetry | Budget owner |
| Hardware inventory | Authoritative system, allocation evidence and access API | Hardware team |
| Credential system | Secret references, rotation and permitted runtime retrieval | Platform/security |
| Retention | Raw context, artifacts, run history and audit periods | Data/security owner |
| Live writes | Approval expiry, idempotency/reconciliation and external side-effect policy | Tool + business owners |
| Dashboard reopening | Exact files/behaviors transferred and intended functionality | User + dashboard owner |

Do not invent team names, approval dollar thresholds, model prices, deployment region, data residency claims, compliance status or production credentials to fill these gaps. Proposed local fixtures and defaults must be documented as such.

The user's confirmed OpenCode choice resolves the earlier runtime-engine conflict. It does not establish that the existing adoption document's version-specific claims are verified. The user also did not assign the whole CLI/SDK implementation to this workstream merely by describing the product vision.
