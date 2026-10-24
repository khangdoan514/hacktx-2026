# Shared API, CLI and SDK contract

The developer CLI/SDK and nontechnical website are clients of one Go application. This workstream must make the backend client-independent. Full CLI/SDK development is not automatically included; agree on ownership and first supported language before implementation.

## Contract requirements

- Version HTTP contracts and canonical input/output schemas. Generate or validate client types against them.
- Use stable opaque IDs, UTC timestamps, explicit enums and optionality. Document units and money representation.
- Provide safe machine-readable error codes, HTTP mapping, field errors where useful and request/correlation IDs.
- Support filtered stable pagination for lists and ordered event cursors for runs.
- Require expected revision/ETag for concurrent mutations. Return conflicts without silently overwriting work.
- Scope idempotency keys to principal/action and request-body hash; replay identical results and reject mismatched payloads.
- Distinguish input validation from admission, asynchronous execution, terminal outcome and external side-effect status.
- Document resource visibility, approval requirements and rate limits. Clients cannot bypass them.

Retain existing `/v1/agent-runs`, run detail/events and `/v1/agents` contracts while reconciling new business `/api/v1` operations. Proposed commands include request submit/triage, spec review, approval decide, capability publish/assign/suspend, evaluation start/read, run start/cancel/retry, context grant/revoke and handover acknowledge. These are requirements, not claims that all endpoints exist today.

## Authentication across clients

Website: supported Supabase session flow, refresh, callback, actual sign-out and current employee membership. Do not maintain a stale copied access token disconnected from refresh events.

CLI: select a supported interactive authentication flow or narrowly scoped developer credential mechanism. Design it explicitly; do not assume Supabase already supplies a particular device flow. Store credentials in an OS-appropriate protected location, support logout/revocation, and never print them by default. Do not ask developers to use a database/service-role key.

SDK: accept a token provider or scoped credential abstraction with renewal/cancellation support. Avoid embedding a universal key. Automated service identities need their own limited grants, expiry and audit attribution, not impersonation of a privileged employee.

Authentication identifies the caller; current database policy still authorizes each object/action. CLI/SDK requests must not claim trusted `actor_type`, organization, reviewer, execution mode or policy fields. Server-derived attribution applies equally to every client.

## SDK behavior

Expose typed responses and errors, request timeouts, pagination, cursor-based event iteration, cancellation requests and explicit retries. Retry only operations whose semantics are safe or protected by idempotency. A timed-out write or run submission may already have happened; provide lookup/reconciliation rather than blind resubmission.

Keep the SDK thin. Business policy, state-machine decisions and budget enforcement must remain on the server. Offer a structured/machine-readable CLI output mode, meaningful exit codes and no dependence on browser-only state. Do not force raw JSON on website users to compensate for missing forms.

## Proposed developer journey

Authenticate → discover authorized capabilities/templates → create/update an eligible draft → validate → execute an evaluation → inspect failures/events → submit review → publish if authorized → invoke an approved version → observe/cancel/revise. Commands that require a human decision should clearly return the pending decision/resource reference; they must not silently self-approve.

## Contract acceptance

Run retained business operations via authenticated HTTP without a browser, then confirm the website sees the same IDs/revisions/results. Test identical authorization denials across surfaces. Verify event resume, stale update, expired credentials, rate limits and ambiguous submission recovery. Client success must reflect a committed/accepted server result, not just an optimistic local UI state.
