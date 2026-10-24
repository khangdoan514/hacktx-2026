# Domain model and workflows

The request flow connects teams; the capability lifecycle connects interfaces. All transitions below are proposed application behavior enforced by Go, not arbitrary client-edited status fields.

## Actors and grants

Separate organization placement, business function, action permission, and resource scope. A PM can also be a BA; that does not grant security approval authority. Employees can belong to multiple teams. A catalog summary may be discoverable without exposing its private prompt/context/run results.

| Actor | Typical responsibility |
| --- | --- |
| Requester / BA / PM | Define outcome, acceptance criteria, context and business constraints |
| Requesting-team lead | Prioritize and authorize demand within delegated limits |
| Software/AI intake owner | Triage, clarify, assign, propose reuse, own next action |
| AI engineer | Prepare versioned configuration and test evidence |
| Hardware owner | Review actual hardware requests and allocation evidence |
| Data/security reviewer | Approve particular source/tool/action scope |
| Budget owner | Authorize a bounded expenditure envelope |
| Release approver | Check evidence and publish eligible immutable versions |
| Operator | Invoke permitted versions and inspect/cancel permitted work |
| Administrator / auditor | Distinct administration and read-only review powers |

Membership must be active at decision/read time. Historical assignment is not enduring authority. Self-approval rules apply across all the functions a person holds.

## Request-to-handover sequence

1. Start from a use-case template or blank draft. Record requesting team, desired outcome, owner, due date, target users and acceptance criteria.
2. Identify possible reuse through deterministic category/scope filters initially. Do not describe filtering as semantic AI matching.
3. Submit a frozen request revision to Software/AI. Retain receiving team separately from requesting team.
4. Intake claims/assigns it, asks a targeted question, accepts/rejects with reason, or proposes an existing capability.
5. Engineer prepares a specification revision with tools, context, prompt, model policy, limits, trigger/interface and evidence plan.
6. Obtain the required reviews for that version. Hardware review is conditional on resource need, not compulsory for every hosted-model task.
7. Execute eligible test cases via the scripted backend or verified OpenCode backend. Preserve evidence and execution provenance.
8. Evaluate release gates and publish an immutable capability version with a support owner and authorized scope.
9. Send a structured handover; requester acknowledges or returns a specific question. Link acceptance evidence and any limitations.
10. Observe permitted operation, request changes, suspend or retire. New material changes require new versions and relevant review.

## Proposed state machines

| Record | States / transitions |
| --- | --- |
| Request | draft → submitted → triage → needs_information / accepted / rejected; accepted → in_delivery → ready_for_acceptance → fulfilled |
| Specification | draft → in_review → approved / changes_requested; replaced revisions become superseded |
| Capability version | draft → testing → eligible → published → suspended / retired |
| Approval | pending → approved / rejected / changes_requested / expired / superseded / cancelled |
| Handover | draft → sent → acknowledged → completed; controlled return/cancel paths |
| Run | preserve current contract's PENDING → RUNNING → SUCCEEDED / FAILED / CANCELLED; extend deliberately if needed |

Cancellation/reopening requires explicit policy and reason. Do not overload `FAILED` to mean denied input before admission without defining the distinction. Record attempts separately from logical runs; retries must not erase prior failures.

## Specification contents

Identity and accountable teams; desired output/non-goals; schemas; trigger/interface; versioned instructions/template; requested model policy; tools and versions; side-effect classifications; context references/classification/purpose; hardware requirements; budget owner/limits/period/currency; guardrails; evaluation suite/thresholds; support/retirement plan.

Pin referenced sources, executable version, policy revision and evidence hashes. The business specification is the requested/approved envelope; the canonical executable definition belongs to the execution domain. Catalog releases link them rather than creating two independent editable definitions.

## Invariants

- Decisions bind organization, resource, exact revision/hash, reviewer scope, expiry and state.
- A material tool/context/instruction/model/action/budget/evaluation change invalidates relevant approvals/evidence.
- One effective outcome survives concurrent decisions. Decision, target mutation, audit and outbox commit together.
- Publication does not allocate hardware, grant arbitrary tools, or approve every future external write.
- Completion endpoints cannot bypass required approval gates.
- Handover includes objective, requested output, source versions, assumptions, findings, unresolved questions, recipient/accountable owner, constraints and permitted use.
- Request, capability, evaluation, run and handover stay correlated so the business outcome can be traced to engineering evidence.
