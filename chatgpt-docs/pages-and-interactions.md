# Website pages and interactions

The website serves less technical employees through structured decisions and evidence. Existing visual design is retained. The dashboard freeze applies throughout; no dashboard/proposal/assistance work is authorized by this document.

| Surface | Required behavior |
| --- | --- |
| Sign-in / access pending | Verified identity, explicit employee access, refresh/expiry handling, actual sign-out |
| My work | Permission-filtered tasks, clarifications, decisions and handovers needing attention |
| Teams | Hardware/Software, Software/AI intake ownership, relevant memberships/capability assignments |
| Request list/new | Filter and paginate; template/reuse selection; draft save, validation, submission |
| Request detail | One selected record, revision comparison, owners, comments, questions, linked decisions and next action |
| AI intake | Assign/claim, prioritize, accept/reject, ask information, propose reuse |
| Specification | Structured fields, authorized tools/sources, revision history and review submission |
| Approval inbox/detail | Exact version and evidence, authorized approve/reject/request-changes, stale/expired conflicts |
| Capability catalog/detail | Discover permitted metadata, request access/reuse, published version, support ownership and permitted invocation |
| Test/evaluation | Authorized inputs/cases, actual execution, per-case outcomes, evidence and gate failures |
| Run list/detail | Start permitted work, inspect persisted events/results, cancel and retry as linked new work |
| Context/documents | Upload, finalize, attach immutable versions, controlled download, revoke/expire/delete |
| Connector inventory | Intended vs actually configured state, scope and side-effect class; no raw secrets |
| Resources | Requested/approved hardware and owner review, recorded allocation evidence; no fake live inventory |
| Budgets | Proposed/approved envelope, accountable owner, actual available usage and missing-telemetry state |
| Handover | Versioned package, recipient acknowledgment, returned question and completion evidence |
| Notifications | Read/unread, preferences, authorized deep links without private previews leaking |
| Administration/audit | Employee/team/grant changes, suspension, protected history search |

## Interaction quality is functional scope

Detail routes must fetch their object, not an entire collection. Buttons invoke real commands with current revision information. Show validation, permission, conflict and unavailable-integration states. Preserve draft work where loss is material. Record user-supplied evidence rather than submitting canned completion assertions.

No raw JSON view substitutes for approve/clarify/acknowledge actions. Technical details can be available in expandable evidence views without becoming required input for every BA. Expose permitted actions returned by the backend for usability, while retaining server-side checks regardless of button visibility.

## Cross-interface parity

A BA's website specification and a developer's CLI/SDK definition must resolve to the same canonical version and policy checks. A developer may submit structured JSON while the BA uses forms, but neither may alter server-assigned identity, provenance, authorization or review status.

## Frozen surfaces

If an existing run/agent page is under `(dashboard)/`, keep its files and behavior unchanged. Preserve its API contract where compatible. Do not create a second conflicting route. Build approved product interactions outside the frozen paths or defer the specific change. The vision's emphasis on observation does not lift the dashboard freeze.

## Acceptance walkthrough

Use two or more real test identities with fictional business data: Hardware submits a PR-review need; AI asks a clarification; engineer versions a spec; authorized reviewer approves; an old revision is rejected; a scripted run produces inspectable persisted evidence; evaluation gates release; requester acknowledges. Revocation blocks the next read and run. The same retained commands should also work through authenticated HTTP without the website.
