# OpenCode execution and the interim scripted adapter

The user selected main's OpenCode direction after PR #5 removed the custom engine. Preserve that choice. Historical instructions to build `ModelAdapter.Next` plus a new custom coordinator/tool engine are superseded as implementation tasks. Existing contracts and fixtures remain compatibility inputs, not a mandate to restore removed packages.

## Execution adapter

NMC3 validates identity, inputs, version eligibility, context grants and budget admission before delegating. A narrow backend adapter supplies execution start, cancellation where supported and observations. Use the actual current service interface or an intentional versioned change; do not assume the older `Next` signature still matches current contracts.

Interim scripted adapter: deterministic inputs/scenarios generate bounded structured results and actual persisted run/event transitions without live model/network calls. Name the implementation clearly, for example `ScriptedExecutionBackend` in a dummy/scripted package. Persist server-assigned execution mode and adapter version. Select mode explicitly. Never fall back to scripted success when OpenCode/provider execution fails.

This adapter tests NMC3 admission, lifecycle, event persistence, UI/API behavior and failure handling. It does not prove real model quality or OpenCode's enforcement. It must not grow into a second general-purpose agent engine with custom providers, tool orchestration and delegation semantics.

## Scenarios

Preserve vendor-comparison fixtures as a contract regression where compatible. Add a PR-review scenario over a checked-in fictional diff with two scripted specialist contributions: code quality and test/security checklist. Findings cite only actual fixture lines and sources. Normal product names are acceptable; code and developer provenance must make the scripted implementation clear.

Failure scenarios include unauthorized tool/scope, forbidden delegation, malformed output, deadline, exhausted limit, cancellation and unavailable backend. They are deterministic test inputs, not random failures. Do not implement dashboard-assistance scenarios during the freeze.

## OpenCode integration

Translate the approved version into supported OpenCode configuration, scoped tool permissions, context and model selection. Never forward arbitrary user-supplied server endpoints, shell commands or credential references. Map logical run IDs to session IDs without exposing raw unrestricted sessions to clients.

Persist safe observations with a monotonic NMC3 sequence. Deduplicate source messages/events and recover a consistent snapshot after reconnect when supported. Preserve history across process restarts. In-flight recovery is a separate feature; report interruption/unknown state honestly until reconciliation determines a terminal outcome.

Validate structured output before applying it. Treat unstructured output as data, not an authorized mutation. An OpenCode response cannot approve its own tool expansion, budget increase, publication or human decision.

## Verify instead of assuming

The repository adoption document mentions particular OpenCode versions, API paths, depth behavior, models and pricing. Those are repository design claims; this documentation pack did not independently verify their present external validity. Before implementation, check the installed version's official API and prove:

- supported authentication and network isolation;
- session creation/correlation and duplicate/uncertain submission handling;
- depth/permission enforcement including escape through shell/MCP;
- allowed model/tool mapping and current-grant revocation;
- cancellation and deadline behavior;
- structured-output validation path;
- usage availability, units and cost provenance;
- whether limits are enforceable before consequential execution;
- event subscription/reconnect semantics and recovery mapping.

The current history-only binary does not satisfy these checks. Unknown capabilities must be represented as unknown, not silently passed. Block live use requiring an unsupported control. A scripted test cannot substitute for an OpenCode integration test.

## Website wording and records

Do not add dummy/demo badges to the website. “Completed” can accurately mean the scripted execution completed. Do not claim a real code review was posted, paid model ran, GPU was allocated or organization-wide security scan occurred. Show actual recorded duration and available values; missing usage remains unavailable. Internal/API provenance must remain available to authorized developers/auditors even if not promoted into website chrome.
