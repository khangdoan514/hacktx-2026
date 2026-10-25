# Agent operations

Local operations material for Pass 8. This directory is the operations package. It does not deploy a service, and it does not contain production credentials.

| Path | Purpose |
| --- | --- |
| [install.md](install.md) | Fresh local install, backup, restore, and rollback without deleting committed runs |
| [runbook.md](runbook.md) | Locate a failed run, explain the cause, and follow a recovery path |
| [observability.md](observability.md) | Log and trace fields, redaction, concurrency controls, unset service targets |
| [env.example](env.example) | Empty local environment names |
| [metrics](metrics) | Go package: process logger, organization concurrency, redaction, usage totals, unset targets |

From `agents/`, the package check is:

```sh
go test ./ops/metrics
```

That check does not need a database, OpenCode, or a secret.

`metrics.LogLine` and `metrics.Span` are the structured records. `metrics.Install` is the one call that writes them to an `io.Writer`. `cmd/nmc3-agent` does not call it. The process still logs with the standard library at these lines of `agents/cmd/nmc3-agent/main.go`:

| Line | Function | Statement |
| --- | --- | --- |
| 26 | `main` | `log.Fatal(err)` |
| 81 | `run` | `log.Printf("NMC3 API listening on %s (OpenCode backend %s)", addr, opencodeURL)` |

`run` begins at line 30. The standard-library import is line 12. Place this call at the start of `run`, before `DATABASE_URL` (line 36) and `SUPABASE_SECRET_KEY` (line 48) are read:

```go
records, err := metrics.Install(os.Stderr)
if err != nil {
    return err
}
```

Replace line 81 with `Startup`. Emit `Request` from the HTTP handler when the request has a run id, a trace id, and a span id. Line 26 stays `log.Fatal` until `main` uses the same logger, so a failure returned from `run` is still outside this redactor. This package does not edit `main.go`.

```go
if err := records.Startup(time.Now().UTC(), runID, addr, opencodeURL, nil); err != nil {
    return err
}
if err := records.Request("GET", "/v1/agent-runs/"+runID, metrics.Fields{
    RunID:   runID,
    TraceID: traceID,
    SpanID:  spanID,
}, nil); err != nil {
    return err
}
```

`Startup` and `Request` each write one JSON record and a newline. Both include `run_id` when the caller supplies one, and both write nothing when it is missing. Passwords, tokens, database URLs, and `supabase_secret_key` are redacted before the write. Keep secrets out of the shell that starts the process.

Service targets for latency, throughput, recovery, availability, and cost are unset until a representative workload is measured. `metrics.ServiceTargets` encodes that state and carries no numeric objective. Load, backup-restore, and chaos checks have not been run. Rollback remains: stop the new process and start the last known-good binary against the same database.
