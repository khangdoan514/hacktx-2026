# Extensions (Pass 6)

Developers register governed tools and run custom Go tools outside the coordinator process. This package is the service implementation. It does not mount itself into `cmd/nmc3-agent` and it does not add a website screen.

## Decisions

**Worker protocol.** Version `1` (`NMC3-Extension-Protocol: 1`). A worker listens on loopback HTTP. `GET /v1/health` returns `{protocol, tools}` for registration. `POST /v1/invoke` carries the pinned tool id and version, tenant, run, invocation, principal id, JSON input, network allow list, filesystem root, output limit, and at most one credential. The response is `{protocol, output}` or `{protocol, error}`. A connection reset, unexpected EOF, or process exit is `WORKER_CRASH`. A handled handler panic stays in the worker and returns `INTERNAL`.

**Isolation.** The supported deployment is a separate process on loopback. The coordinator dials only IP addresses on the tool's allow list and ignores `HTTP_PROXY`. The SDK `Dial`, `ReadFile`, and `CredentialFromContext` functions enforce that same network list, a filesystem root, and the one credential on the call. Endpoints must be IP URLs, so a tool cannot trigger DNS. OS cgroups and network namespaces are not enforced here.

**Credentials.** Registry records store an `auth_ref` name. Values live only in `MemoryCredentials` and are passed as an in-memory argument on one call. The management API rejects a credential field. URLs must not contain userinfo. Resolved values are removed from stored output and error text. A worker has no credential store and cannot ask for another tenant's secret.

**Versions.** `Pin` copies the latest published record for a run. The first pin wins. Publish, retire, and edits of the returned copy do not change that snapshot. Writes do not retry. A write needs an unexpired approval and an idempotency key. A timeout or lost response after dispatch is stored as uncertain and the same key does not dispatch again. Read-only tools may retry up to five times.

**Internal HTTP.** Internal services and external HTTP tools share `HTTPAdapter`. Set `endpoint.internal` for a private service. MCP is a separate stateless client: JSON-RPC `initialize` and `tools/call`, with `result.structuredContent` as the output.

**Shared contract.** `ToolRecord` and `Invocation` add endpoint, auth reference, side effect, attempt, and worker metadata around v1. `ToolRecord.V1` and `Invocation.V1` project back to `contracts.ToolDefinition` and `contracts.ToolInvocation`. Event readers should use `Invocation.Metadata`, which omits input, output, and secrets. The integrator owns folding these fields into `agents/internal/contracts` and the API fixtures. This package does not change that contract.

## Register and call

```go
reg := extensions.NewRegistry()
creds := extensions.NewMemoryCredentials()
gw := extensions.NewGateway(reg, creds, extensions.Options{})

_ = reg.PutDraft(extensions.ToolRecord{ /* tenant, id, version, class, endpoint, schemas */ })
_, _ = reg.Publish(tenant, id, version)
_, _ = reg.Pin(runID, tenant, []string{id})
invocation, err := gw.Invoke(ctx, runID, extensions.InvokeRequest{
    Principal: principal,
    Agent:     agent, // AllowedTools is required
    Invocation: contracts.AgentInvocation{ID: invocationID, RunID: runID},
    ToolID: id,
    Input:  input,
})
```

`Authorize` is the permission check for MCP, HTTP, and worker tools. The gateway calls it before any adapter dials. A draft cannot be pinned. A retired version can still finish a run that already pinned it.

Custom tools use `ServiceTool` and `Serve`. The sample process is `agents/cmd/extension-worker`:

```text
extension-worker -listen 127.0.0.1:0 -mode echo
```

It prints the bound address, then serves `extensions.worker.echo` `1.0.0`. `-mode crash` exits on invoke so the coordinator can record the crash. Discover the process with `WorkerAdapter.Discover`, then `Registry.ImportDiscovered` and publish.

## Management API

`NewAPI` exposes draft, publish, retire, pin, list, and invoke under `/v1/extension-tools`. Invoke returns HTTP 200 with the invocation record, including denials, crashes, and timeouts. The handler does not accept credential values and does not authenticate callers. Mount it only behind the service authenticator.

## Tests

From `agents/`:

```text
go test ./internal/extensions/ ./cmd/extension-worker/
```

The tests use loopback fakes only. They do not call a live MCP server, a public HTTP API, or Supabase.

## Limits

- No website tool-management screen. The HTTP API above is the service test surface.
- No cgroup, seccomp, or network-namespace sandbox. A worker that ignores `Dial` and `ReadFile` can use the process network and filesystem.
- Write idempotency is in process memory. A coordinator restart needs the Pass 4 durable store before it can keep that guarantee.
- Read-only retries have no backoff.
- v1 run events are not emitted here. The coordinator should append `Metadata` after `Invoke`.
