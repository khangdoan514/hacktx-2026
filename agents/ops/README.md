# Agent operations

Local operations material for Pass 8. This directory is the operations package. It does not deploy a service, and it does not contain production credentials.

| Path | Purpose |
| --- | --- |
| [install.md](install.md) | Fresh local install, backup, restore, and rollback without deleting committed runs |
| [runbook.md](runbook.md) | Locate a failed run, explain the cause, and follow a recovery path |
| [observability.md](observability.md) | Log and trace fields, redaction, concurrency controls, unset service targets |
| [env.example](env.example) | Empty local environment names |
| [metrics](metrics) | Go package: organization concurrency, redaction, usage totals, unset targets |

From `agents/`, the package check is:

```sh
go test ./ops/metrics
```

That check does not need a database, OpenCode, or a secret.

`metrics.LogLine` and `metrics.Span` are the structured records. The process entrypoint in `cmd/nmc3-agent` does not call them yet. Until an integrator wires that call, startup logs from the entrypoint are outside this redactor. Do not print secrets from the shell that starts the process.

Service targets for latency, throughput, recovery, availability, and cost are unset until a representative workload is measured. `metrics.ServiceTargets` encodes that state and carries no numeric objective.
