# Operator runbook

Use this when a local agent run fails, the API process stops, or a deployment rollback is in progress. Pair it with [install.md](install.md) for backup, restore, and process rollback, and with [observability.md](observability.md) for the log and trace fields.

Service targets are unset. Do not page, scale, or raise a budget because a latency, throughput, availability, or cost number looks wrong. No such number has been measured.

The current API has no production authentication. Use fictional data on a disposable database.

## Locate a failed run

Collect the run id from the caller. Every operator log and trace produced by `metrics.LogLine` or `metrics.Span` carries `run_id`. `trace_id` and `span_id` join a log line to a span when the caller set them. `tenant_id` is the organization id.

Search the records you encoded with this package:

| Field | Use |
| --- | --- |
| `tenant_id` | Organization |
| `agent_id` and `agent_version` | Pinned definition |
| `state` | `PENDING`, `RUNNING`, `SUCCEEDED`, `FAILED`, `CANCELLED` |
| `error_code` | Typed cause |
| `timestamp` | Inclusive `Since` / `Until` window |

`metrics.SelectLogs` applies that filter in memory. The API still serves the stored run:

```sh
curl -sS "http://127.0.0.1:8080/v1/agent-runs/RUN_ID"
curl -sS "http://127.0.0.1:8080/v1/agent-runs/RUN_ID/events"
```

The run snapshot is the current state, error, usage, and invocation list. Events are ordered by sequence. An event payload is not a place for secrets or full prompts. If a snapshot or event contains credential text, treat the store as exposed and follow the secret incident below. Do not paste the payload into a ticket.

`state=FAILED` plus `error_code` is the lookup for a failed run. `metrics.Explain` maps that code to a cause and an operator action.

## Explain the cause

| `error_code` | Cause | Operator action |
| --- | --- | --- |
| `INVALID_INPUT` | Validation failed before new work was scheduled | Correct the request. This failure did not dispatch a protected write |
| `NOT_FOUND` | The id is not in this environment's store | Confirm the id and the database the process is using |
| `AGENT_NOT_ALLOWED` | Policy denied the agent | Read the allowlist. Model text does not grant the agent |
| `DELEGATION_DEPTH_EXCEEDED` | A depth-1 agent tried to delegate | The denial is final for that action. No child was created |
| `TOOL_NOT_ALLOWED` | The tool was denied before its handler | Read the pinned tool allowlist |
| `BUDGET_EXCEEDED` | A turn, tool, delegation, time, token, or cost budget stopped scheduling | Read the usage stored on the run. Leave production caps unchanged while service targets are unset |
| `TIMEOUT` | The deadline elapsed | Do not repeat a protected write whose outcome is uncertain |
| `OUTPUT_SCHEMA_INVALID` | Model output failed schema checks | The payload is not a successful side effect |
| `INTERNAL` | The service recorded an internal failure | Read the snapshot and the ordered events |
| any other code | No catalogued cause | Read the snapshot and the ordered events, then use the uncertain-write rule |

Platform failures (`TIMEOUT`, `INTERNAL`, a process crash) and model-output failures (`OUTPUT_SCHEMA_INVALID`, `BUDGET_EXCEEDED` from token or cost usage) stay distinct because the error code and the stored usage are different fields. Do not collapse them into one retry.

## Recovery path

1. Locate the run with the API calls above and, when you have records from this package, with `run_id`.
2. Read `error.code` on the run and the matching events. Call `metrics.Explain` or use the table above.
3. If the run reached `SUCCEEDED`, `FAILED`, or `CANCELLED`, that outcome is committed. Leave it in place. The store rejects a later transition out of a terminal state.
4. If the API process died while a run was still `PENDING` or `RUNNING`, the row remains inspectable and the in-flight attempt is not resumed. Record that the attempt stopped. Do not claim the work finished.
5. If the failure was a bad process build, roll the process back using [install.md](install.md). Committed rows stay in the database.
6. If a write's outcome is uncertain, stop. Reconciliation or an explicit operator decision comes before another dispatch. This package does not dispatch tools and does not invent an idempotency key.

A read-only tool retry policy belongs to the runtime, not to this runbook. This runbook does not authorize a second write.

## Incidents

### Secret in a log, trace, or ticket

1. Stop copying the file. Identify the run id without repeating the secret.
2. Remove or replace the exposed file. Rotate the credential with the procedure in [install.md](install.md).
3. New records encoded as `metrics.LogLine` or `metrics.Span` replace sensitive fields and scrub bearer tokens, URL userinfo, assignments such as `password=`, and PEM blocks. Restricted fields (`prompt`, `instructions`, retrieved document text, raw model input) are dropped.
4. Records that never passed through that encoder are unchanged. The `cmd/nmc3-agent` startup logger is one of those paths.

### Organization at its concurrency cap

`metrics.Limiter` admits a fixed number of in-flight runs per organization inside one process. A caller that waits past the context deadline is rejected and counted. Other organizations keep their own caps. Releasing a finished run frees a slot. A second release is a no-op.

The cap is a local control, not a measured throughput target. Raising it is a configuration change in the caller of `NewLimiter`, after a workload has been measured. Two API processes do not share the cap.

### Process crash or restart

1. Start the known-good binary with the same disposable database.
2. `GET` the run. A row that was committed is still there.
3. A run left in `PENDING` or `RUNNING` is interrupted. Do not mark it succeeded. Automatic resume is outside this package.
4. Do not re-send a protected write that was in flight at the crash.

### Failed release

Follow [install.md](install.md): stop the new process, start the previous binary, and read a run that was terminal before the failure. Use the dump restore only when the database itself must be replaced by an earlier dump. `DROP SCHEMA nmc3 CASCADE` is not an incident response.

### Retention

No retention period is set. This package does not delete runs, events, or dumps. Keep committed runs until a reviewed retention decision exists. Redaction at log time does not erase a row already stored in Postgres.

## Checks this runbook does not replace

The in-process limiter tests show that one process enforces an organization cap under parallel acquires. They are not a cluster load test. Backup, restore, process rollback, and crash drills in a shared environment are operator exercises and are not run by `go test ./ops/metrics`.
