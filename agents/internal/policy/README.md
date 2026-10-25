# Agent-run policy

This package is the Pass 4 policy layer for agent runs. It checks tenant, allowlist, budget, approval, and protected-write rules before a tool or delegate runs.

It does not replace NMC2 employee identity. People are still `app.employees` and `app.role_grants`. Authenticate with `platform.Verify`, load the employee with `organization.Load`, then convert the actor:

```go
identity, err := platform.Verify(cfg, rawToken, keys)
actor, err := organization.Load(ctx, tx, identity.UserID, identity.Email)
principal, err := policy.PrincipalFromActor(actor)
```

`Principal.TenantID` is `platform.Actor.OrgID`. Role checks call `platform.Actor.Has`, so the role codes stay `ai_engineer`, `release_approver`, `security_reviewer`, `operator`, `administrator`, and `auditor`. Email and profile text are not authorization claims. An actor with no user id or organization is rejected. Pass 1 anonymous local mode is not a principal.

Service callers that were authenticated outside this package use `ServicePrincipal`. A service cannot approve a protected write unless that policy revision sets `AllowServiceApproval`. Default policies require a human approver.

## Checks

| Call | Rule |
| --- | --- |
| `StartRun` | Starter role (`ai_engineer`, `operator`, `administrator`), definition in the caller's tenant, model on the pinned allowlist, budget within the policy ceiling. Same idempotency key returns the original pin. |
| `ReadDefinition` / `Policy` | Any role in the same tenant. Another tenant is `CROSS_TENANT` and the body is not returned. |
| `AuthorizeDelegate` | Target must be on both the pinned policy and the caller definition, and the target definition must be in the same tenant. |
| `CheckTool` / `BeginProtectedWrite` | Tool must be on both allowlists. `READ_ONLY` is not a protected write unless the policy lists it in `ProtectedTools`. `WRITE`, an empty side effect, or a listed tool waits for approval. |
| `ReadRun` / `ReadEvents` / `ReadContext` | Same tenant, and the caller is the requester or an inspector (`auditor`, `administrator`, `operator`, `ai_engineer`, `security_reviewer`). |
| `ResolveSecret` | Same tenant and a starter role. Auditors do not receive secret values. The resolver runs only after the check. |
| `Decide` | Same tenant, approver role on the pinned revision, not the requester, one decision, and not expired. |
| `Dispatch` | Starter role, approval `READY`, and `dispatch_count` still 0. |
| `Reconcile` | Human `operator` or `administrator`, and only from `UNCERTAIN`. |

A model message or retrieved document is `UntrustedInput`. Non-empty text is audited as "untrusted input ignored". It cannot add a tool, add a delegate, change a side effect, or skip approval. Depth-1 delegation remains the coordinator's rule.

## Approvals and protected writes

`BeginProtectedWrite` stores one `WAITING` approval and one write attempt, and holds the run at `StateWaiting` (`WAITING`), the same value as `contracts.StateWaiting`. The coordinator should apply that value while the approval is waiting. Restarting the process keeps the row: construct a new `Engine` on the same store.

Expiry is inclusive (`now >= expires_at`). The default TTL is 24 hours when the policy TTL is 0. An expired request records an audit entry for the escalation role (default `administrator`). Escalation does not approve and does not dispatch. A denied, expired, unauthorized, or self-approval decision does not grant `Dispatch`.

`IdempotencyKey(tenant, run, invocation, tool, actionDigest)` is the stable key. A timeout after `Dispatch` moves the attempt to `UNCERTAIN` and leaves `DispatchCount` at 1. `Dispatch` and `BeginProtectedWrite` with that key return the same attempt and `Granted` is false. `ReportResult` will not guess the outcome. `Reconcile` records an explicit succeeded or failed outcome and does not call the tool.

`DispatchResult.Granted` is the only signal to perform the external write. Count grants in the gateway. A second true grant for one key is a bug.

Secret values are not columns on `SecretRef` and are not written to audit or events. `PutEvents` redacts JSON keys such as `token`, `password`, `secret`, and `api_key`.

## SQL

`agents/db/migrations/202610240004_nmc3_approvals.sql` adds `nmc3.agent_policies`, `nmc3.approval_requests`, `nmc3.approval_decisions`, `nmc3.write_attempts`, and `nmc3.policy_audit`. Decisions are unique per request and cannot name the requester as approver. Write attempts share the approval's tenant, run, and idempotency key. `dispatch_count` is 0 before dispatch and 1 after. Browser roles are not granted.

`agents/db/migrations/202610240009_nmc3_policy_pg.sql` adds `nmc3.policy_definitions`, `nmc3.policy_runs`, `nmc3.policy_secret_refs`, `nmc3.policy_contexts`, and `nmc3.policy_events` so `OpenPostgres` can implement the same `Store` as `NewMemory`. Apply 004 before 009. Secret values are not columns. `policy_runs.state` accepts `WAITING`. The coordinator table `nmc3.runs` still does not.

`OpenPostgres` is the Postgres store. `NewMemory` remains the in-process store. Do not call `Store` transition methods from request handlers; call `Engine`.

## HTTP

`Handler` serves the inbox. `ListPending` and `DecideApproval` are the same routes and can be mounted on their own.

- `GET /v1/approvals/pending` lists `WAITING` requests in the caller's tenant.
- `POST /v1/approvals/{id}/decision` with `{"outcome":"APPROVED"}` or `{"outcome":"DENIED"}`.

A cross-tenant read or decision responds `404` with `NOT_FOUND` and no approval body. The requester receives `403` `SELF_APPROVAL`. An expired decision receives `409` `EXPIRED` and does not dispatch.

The principal function must authenticate the employee. The body cannot choose the approver:

```go
func principals(database *platform.DB, cfg platform.Config, keys *platform.JWKS) policy.PrincipalFunc {
    return func(r *http.Request) (policy.Principal, error) {
        raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
        identity, err := platform.Verify(cfg, raw, keys)
        if err != nil {
            return policy.Principal{}, err
        }
        var actor platform.Actor
        err = database.WithUser(r.Context(), identity.UserID, identity.Email, func(ctx context.Context, tx pgx.Tx) error {
            loaded, loadErr := organization.Load(ctx, tx, identity.UserID, identity.Email)
            actor = loaded
            return loadErr
        })
        if err != nil {
            return policy.Principal{}, err
        }
        return policy.PrincipalFromActor(actor)
    }
}
```

Mount in `agents/cmd/nmc3-agent/main.go` inside `run`, after the coordinator exists and instead of passing `httpapi.New(...)` directly as `http.Server.Handler`:

```go
openCtx, openCancel := context.WithTimeout(context.Background(), 15*time.Second)
policyStore, err := policy.OpenPostgres(openCtx, os.Getenv("DATABASE_URL"))
openCancel()
if err != nil {
    return err
}
defer policyStore.Close()
engine := policy.New(policyStore)

root := http.NewServeMux()
root.Handle("/v1/approvals/", policy.Handler(engine, principals(database, cfg, keys)))
root.Handle("/", httpapi.New(coordinator, runs, events, definitions))
server := http.Server{Addr: addr, Handler: root, ReadHeaderTimeout: 10 * time.Second}
```

`cmd/nmc3-agent/main.go` mounts this handler on the dedicated agent database and uses the local console principal.

## Not wired here

The coordinator and tool gateway still do not call this package. `main.go` does not construct an `Engine` or mount `Handler`, so tenant denial and approval dispatch are not enforced on a live run. Pass 4 is not complete until that wiring exists.

`contracts.StateWaiting` is `WAITING`. `nmc3.runs` still rejects that state, and the run store's transition function does not move a run into it. `internal/agents/contracts` is a separate copy and does not include `StateWaiting`.

Postgres tests use `NMC3_POLICY_TEST_DATABASE_URL` against a local Postgres 16 with migrations 001, 004, and 009. This package was not run against Supabase.
