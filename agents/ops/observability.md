# Structured logs and traces

`agents/ops/metrics` defines the operator records for a run. Encode logs with `metrics.LogLine` and spans with `metrics.Span`. Both refuse to encode when `run_id` is missing. A span also requires `trace_id`, `span_id`, and a name. JSON encoding redacts attributes before anything is written.

No OpenTelemetry exporter, collector endpoint, or credentials are included. The field names below are the contract an exporter would map: `trace_id` and `span_id` are the trace context, and `run_id` is a span attribute. `tenant_id` is the organization id used by `metrics.Limiter`.

The API process does not emit these records yet. Wiring them into `cmd/nmc3-agent` is integrator work.

## Log fields

| JSON field | Required | Meaning |
| --- | --- | --- |
| `timestamp` | yes | UTC RFC3339Nano |
| `level` | yes | `info`, `warn`, or `error` |
| `message` | yes | Short operator summary. Free text is scrubbed, and it is the wrong place for a prompt |
| `run_id` | yes | Service run id |
| `trace_id` | when known | Joins the line to a span |
| `span_id` | when known | Current span |
| `tenant_id` | when known | Organization |
| `principal_id` | when known | Caller identity id, not a credential |
| `invocation_id` | when known | Agent invocation |
| `agent_id` | when known | Pinned agent |
| `agent_version` | when known | Pinned version |
| `state` | when known | Run state |
| `error_code` | on failure | Typed code from the v1 contract |
| `attempt` | when non-zero | Attempt number |
| `attrs` | optional | Redacted bag |

## Span fields

| JSON field | Required | Meaning |
| --- | --- | --- |
| `name` | yes | Span name. Scrubbed as free text |
| `run_id` | yes | Service run id |
| `trace_id` | yes | Trace id |
| `span_id` | yes | Span id |
| `parent_span_id` | when known | Parent span |
| remaining identity fields | when known | Same names as a log line |
| `attrs` | optional | Redacted bag |

A representative failure line has this shape. The token below is a placeholder, and the encoded form does not contain it:

```json
{
  "timestamp": "2026-10-24T18:00:00Z",
  "level": "error",
  "message": "run failed: Bearer [redacted]",
  "run_id": "run_123",
  "trace_id": "trace_123",
  "span_id": "span_123",
  "tenant_id": "org_123",
  "principal_id": "user_123",
  "invocation_id": "inv_123",
  "agent_id": "orchestrator",
  "agent_version": "1.0.0",
  "state": "FAILED",
  "error_code": "TIMEOUT",
  "attempt": 1,
  "attrs": {
    "api_key": "[redacted]",
    "detail": "postgres://[redacted]@127.0.0.1/nmc3"
  }
}
```

`prompt` and other restricted keys are absent, not renamed.

## Redaction

Sensitive keys are replaced with `[redacted]`. The key list includes `password`, `secret`, `token`, `api_key`, `authorization`, `credential`, `private_key`, `database_url`, `connection_string`, `service_password`, and `supabase_secret_key`, plus names ending in `_token`, `_secret`, `_password`, `_api_key`, or `_credential`. Matching ignores case and treats `-` as `_`.

Restricted keys are omitted: `prompt`, `instructions`, `raw_prompt`, `system_prompt`, `hidden_prompt`, `retrieved_text`, `retrieved_context`, `document_body`, `message_content`, and `raw_input`.

Free text also drops PEM blocks, URL userinfo (`scheme://[redacted]@host`, so the host can stay), `Bearer` tokens, and assignments such as `password=`. A `run_id=` token in the same string stays.

Values whose Go type is not a scalar, string, map, slice, or JSON message are replaced with `[redacted]` so a struct cannot be stringified into a log. The input map is not modified.

`run_id` is not a secret field. Callers must pass the service run id, not a credential, in that field.

## Token and cost accounting

`metrics.Usage` adds `input_tokens` and `output_tokens` from caller-supplied observations. Cost is recorded only when every observation includes a provider-reported `cost_micro_usd`. If any observation omits cost, the total's `cost_status` is `unset` and `cost_micro_usd` is omitted. A missing cost is not stored as zero. The package does not convert tokens into a price.

## Service targets

`metrics.ServiceTargets` returns latency, throughput, recovery, availability, and cost. Each status is `unset`, the reason is `unset until a representative workload is measured`, and the JSON has no numeric objective.

The organization concurrency cap is a caller-supplied control on `metrics.Limiter`. It is not one of those targets. Counters from `Limiter.Stats` are process-local admitted and rejected acquires, not a throughput SLO.

## Retention

No retention interval is configured. Encoding a redacted log does not delete the Postgres row for the run.
