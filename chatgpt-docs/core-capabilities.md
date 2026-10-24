# Core capabilities: evaluate, trigger, budget, guardrails, prompt, observe

These capabilities apply to the same versioned agent lifecycle across website, CLI and SDK. Their data and enforcement belong to the Go API and execution integration, not client-only controls.

## Evaluation

Version suites, case inputs, expected results, allowed/prohibited actions, comparison criteria and reviewer judgments. Execute against a pinned capability, prompt, policy, source set and adapter/backend version. Persist per-case evidence and failures, not only a total score. Require appropriate security-denial cases before release.

Success rate needs an eligible-run denominator and separate cancelled/unknown outcomes. Tool-call accuracy needs judged calls and a correctness rubric; unjudged calls are not successes. Intervention rate needs a recorded definition of intervention. Model comparisons need comparable cases and actual model-backed observations. Scripted-adapter pass rates demonstrate contract/application behavior, not model quality.

Gate publication on required evidence for the exact version. Changed prompt/context/tools invalidate affected evidence. Preserve a comparison of old and new results. Do not let a client submit `passed=true` or fabricated provider statistics as authoritative evidence.

## Trigger

Start with real manual and authenticated API invocation. Store trigger intent separately from trigger activation. Planned webhooks and schedules must not display as connected/active before implemented.

Future webhook activation needs provider signature validation, replay protection, repository/resource grant checks, delivery idempotency and rate limits. Future schedules need timezone/DST semantics, missed-run/overlap policy, active owner authorization and disable/revoke behavior. CLI execution is another authenticated invocation surface, not a special privileged trigger.

Every admitted run pins the executable version, requested inputs, context references, effective limits, initiating principal and trigger/delivery ID. Revocation must stop new dispatch. A received event alone does not authorize reading the referenced repository.

## Budget

Separate requested budget, approved envelope, reserved amount, measured usage and estimated future cost. Record units/currency, period, owner and policy version. Per-run and team-period limits need atomic reservation/accounting under concurrency; a check followed by an uncoordinated debit can overspend.

Dummy mode records no invented provider usage. Missing telemetry is unknown, not zero. Test synthetic accounting scenarios explicitly in test fixtures. OpenCode usage mapping and enforcement timing require verification; post-run cost observation cannot guarantee a hard pre-action cap. If the runtime cannot enforce a required hard limit, block that capability or scope the promise down explicitly.

Hardware requests and provider token costs are different budgets. GPU count/VRAM/duration requests are not actual utilization or allocated inventory. Release publication never provisions hardware by implication.

## Guardrails

Policy is executable authority: current employee/resource access, allowed models/tools, permitted data, side-effect class, network restrictions, delegation limits, timeouts and human decision gates. Prompts may describe boundaries but cannot enforce them alone.

Use the intersection of caller grant, capability version, context grant, tool scope and policy. Translate that envelope into OpenCode permissions and verify its behavior. Reject unsupported required protections rather than silently dropping them. Deny write-capable tools in the initial execution scope. Arbitrary CLI command execution is not authorized by listing “CLI” on the whiteboard.

## Prompt

Store immutable instruction versions with template reference, editor identity, timestamp/hash and linked test/approval evidence. Editing a published prompt creates a new version. Never change an in-flight run's pinned prompt silently. Restrict who can inspect sensitive instructions and context.

Distinguish prompt content from policy. Retrieved documents, tool outputs and diffs cannot grant permissions or override the governing instruction hierarchy. Do not log complete prompts by default or expose them in globally visible event previews.

## Observation

Expose authorized run state, event sequence, source references, tool activity, safe errors, cancellation state, evidence, duration, execution provenance and available usage. Maintain stable correlation from business request to capability, evaluation, run and external session.

Store replayable ordered history in Supabase. SSE is a delivery channel, not the audit record. Events should be safe metadata with restricted artifact references where needed. Reconnect from a cursor, tolerate duplicate source events, and detect/reconcile gaps.

Observation can be supported through API and approved product run-detail views without editing the frozen dashboard. Keep dummy implementation labels in code/developer metadata, while keeping normal website language truthful about observed outcomes.
