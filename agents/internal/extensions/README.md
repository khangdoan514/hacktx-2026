# Extensions (Pass 6)

Developers register governed tools and run custom Go tools outside the coordinator process. This package is the service implementation. `cmd/nmc3-agent` mounts `NewAPI` on `/v1/extension-tools`. The console page at `frontend/src/app/(console)/tools/` registers a draft and publishes it through `/agent-api/extension-tools`.

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

`NewAPI` returns an `*API`. `API.Mount` registers that handler on an `*http.ServeMux` at `/v1/extension-tools` and `/v1/extension-tools/`. Invoke returns HTTP 200 with the invocation record, including denials, crashes, and timeouts. The handler does not accept credential values and does not authenticate callers. Mount it only behind the service authenticator.

```go
reg := extensions.NewRegistry()
gw := extensions.NewGateway(reg, extensions.NewMemoryCredentials(), extensions.Options{})
extensions.NewAPI(reg, gw).Mount(mux)
```

`cmd/nmc3-agent/main.go` calls `Mount`. The handler does not authenticate callers. The process has no separate authenticator in front of this route.

The import sits next to the other service imports:

```go
	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
	"github.com/khangdoan514/hacktx-2026/agents/internal/extensions"
	"github.com/khangdoan514/hacktx-2026/agents/internal/httpapi"
	"github.com/khangdoan514/hacktx-2026/agents/internal/opencode"
	"github.com/khangdoan514/hacktx-2026/agents/internal/store"
```

The server construction is:

```go
	addr := envDefault("NMC3_ADDR", "127.0.0.1:8080")
	mux := http.NewServeMux()
	mux.Handle("/", httpapi.New(coordinator, runs, events, definitions))
	reg := extensions.NewRegistry()
	gw := extensions.NewGateway(reg, extensions.NewMemoryCredentials(), extensions.Options{})
	extensions.NewAPI(reg, gw).Mount(mux)
	server := http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
```

Line 81 stays the listen log. `net/http` is already imported at line 13. The `/v1/extension-tools` patterns are more specific than `/`, so the agent routes keep their handler.

## Tests

From `agents/`:

```text
go test ./internal/extensions/ ./cmd/extension-worker/
```

The tests use loopback fakes only. They do not call a live MCP server, a public HTTP API, or Supabase.

## Limits

- The console page is `frontend/src/app/(console)/tools/`. The shared shell is unchanged, so `/tools` is not in the primary nav. The route is mounted and does not authenticate callers.
- No cgroup, seccomp, or network-namespace sandbox. A worker that ignores `Dial` and `ReadFile` can use the process network and filesystem. This package does not claim cgroup or network-namespace isolation.
- Write idempotency is in process memory. A coordinator restart drops the ledger. It is not restart-safe. There is no extensions SQL migration.
- Read-only retries have no backoff.
- v1 run events are not emitted here. The coordinator should append `Metadata` after `Invoke`.
