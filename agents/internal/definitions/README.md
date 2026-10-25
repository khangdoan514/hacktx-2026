# Definition registry

This package is the Pass 2 definition lifecycle. It stores agent definitions as versioned records, validates them, serves the draft/test/publish/revise/retire/get/export HTTP API, and pins a run to the exact version it started with. Canonical form is JSON. `agents/cmd/nmc3-agent` still loads `agents/examples/definitions.json` for OpenCode prompts and does not call `ResolveAndPinRun`. It imports that file into the registry and mounts `Handler` when `NMC3_DATABASE_URL` is set. The example file format is unchanged.

## Lifecycle

| State | Meaning |
| --- | --- |
| `draft` | Editable. Reference checks are not required yet. |
| `testing` | A draft that passed validation, including delegate and tool references. |
| `published` | Immutable. New runs may start only from this state. |
| `retired` | Immutable. Hidden from new runs. Existing pins and `Get` still return it. |

`SaveDraft` creates a version or replaces a draft. `Test` checks the stored draft and records the tester. `Publish` checks again, records the publisher, and freezes the version. Publishing the same bytes again does not change the publisher or timestamp. `Revise` saves a new version and leaves the source version alone. `Retire` is allowed only from `published`; a second retire keeps the first timestamp.

A draft may name a delegate that is not published yet. `Test` accepts a delegate in `draft`, `testing`, or `published` at the same version. `Publish` requires that delegate to already be published, or to be in the same import batch. Tool ids must be registered. `catalog.search` `1.0.0` is seeded and is the tool used by the example file.

Specialists may list delegates. `denial-specialist` does, and the registry accepts that reference. Depth is still enforced by the runtime, not here.

## Canonical JSON

`Canonical` is the definition identity. The content hash is the hex SHA-256 of those bytes. Lifecycle metadata, publisher, and timestamps are stored beside the spec and are not part of the hash.

Rules:

- UTF-8 JSON, no insignificant whitespace.
- Object keys are sorted at every level, including schemas.
- Arrays keep their order. Tool and delegate order is significant.
- `allowed_models` is sorted so equivalent policies hash the same.
- Empty tool and delegate lists are `[]`.
- `model_policy` is omitted when it has no default and no allow list. That is why `examples/definitions.json` still unmarshals into the v1 `AgentDefinition`.
- Schema documents use the v1 subset: `type`, `properties`, `required`, `items`, `enum`, and boolean `additionalProperties`. Other keywords fail with a field path.
- JSON numbers keep their spelling. `1` and `1.0` are different bytes and hash differently. The website sends the shortest JSON number (`1`, not `1.0`).

`MarshalDefinitions` writes `{"schema_version":"1","definitions":[...]}`. `Export` writes a backup with `tools` and `versions`, including lifecycle metadata. `Parse` accepts the example array and the definitions envelope. `Import` also accepts a backup. A backup restores the recorded publisher and state; the actor argument is not written over them. Restoring into a registry that already has a different body or state for that version fails the whole import.

Schema version `1` is the only version this package reads or writes. The database column is an open string so a later version can be stored beside it.

## Model policy

`model_policy` is optional. When it is absent, an empty model id means the service default, and any id accepted by `contracts.ValidModelID` is allowed. When `allowed_models` is set, the run model must be in that list. An empty model id is then allowed only if `default_model` is set, because this package does not know the process default. The policy does not start runs; `Version.AllowsModel` is the check a later pass should call before `PinRun`.

## Run pins

`PinRun` copies a published spec onto `(run id, agent id)`. The pin row is immutable. Publishing version B, or retiring version A, does not change a pin of A. `Pinned` returns that snapshot. `ResolveForNewRun` returns only a published version; a retired version returns `RETIRED` and a missing version returns `NOT_FOUND`.

Call `ResolveAndPinRun` once at run start, when the run id exists and before any model turn. It resolves the published root and pins that agent plus each direct `allowed_delegates` entry at the same version. A missing, unpublished, or retired root or delegate returns an error and writes no new pins. Do not retarget an existing pin when a newer version is published. `PinRun` still pins one agent; use it only when the caller already resolved that version.

## HTTP

`Handler` and `Mount` serve one tenant, the registry's tenant. Mutations require the `X-NMC3-Actor` header. Bodies are JSON. Draft and publish take one definition object, the same object `SaveDraftJSON` and `PublishJSON` accept. There is no YAML body.

| Method | Path | Body |
| --- | --- | --- |
| `GET` | `/v1/definitions` | List. `?id=&version=` returns one version, including retired. |
| `POST` | `/v1/definitions/draft` | Definition object. |
| `POST` | `/v1/definitions/test` | `{"id","version"}` |
| `POST` | `/v1/definitions/publish` | Definition object. |
| `POST` | `/v1/definitions/revise` | `{"id","from_version","spec"}`. `spec.version` is the new version. |
| `POST` | `/v1/definitions/retire` | `{"id","version"}` |
| `GET` | `/v1/definitions/export` | Canonical backup from `Export`. |

A success body is `{"definition":...}` or `{"definitions":[...]}`. Export writes the backup bytes as stored.

Invalid publish returns 400 `INVALID_INPUT` and writes nothing:

```json
{"error":{"code":"INVALID_INPUT","message":"definition is invalid","fields":[{"field":"name","message":"is required"}]}}
```

`fields[].field` is the JSON path (`limits.max_tokens`, `allowed_delegates[0]`). Not found is 404. Retired, immutable, and conflict are 409. A published body cannot be replaced; `revise` creates the next version and leaves the source version in place.

The website at `frontend/src/app/(console)/definitions/page.tsx` posts these routes through `/agent-api/definitions/*`. Next rewrites that prefix to the application API, which forwards it to `/v1/*` on this service.

## Wiring main.go

`agents/cmd/nmc3-agent` mounts this handler when `NMC3_DATABASE_URL` is set. Without that variable the process still returns 404 for `/v1/definitions`. `TestHTTPWebsiteFlow` serves `Handler` and performs the page's draft, test, and publish requests, including the pin check.

`Mount` registers absolute paths. Put it on the process mux before the catch-all run API so `/v1/definitions` is not swallowed:

```go
reg, err := definitions.Open(ctx, os.Getenv("DATABASE_URL"))
if err != nil {
    return err
}
defer reg.Close()

mux := http.NewServeMux()
definitions.Mount(mux, reg)
mux.Handle("/", httpapi.New(coordinator, runs, events, fileDefinitions))
server := http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
```

Import the example file once before serving, using the existing `Import` call in this README. The file-loaded slice can stay in the OpenCode constructor until run creation reads the registry.

At run start, after the run id is chosen and before the coordinator starts work:

```go
pins, err := reg.ResolveAndPinRun(ctx, runID, req.AgentID, req.AgentVersion)
if err != nil {
    return err
}
```

`req` is a `contracts.StartRunRequest`. `pins` contains the root and its direct delegates. Do not call this again to follow a newer publish.

To try the page without editing `main.go`, from `agents/`:

```sh
NMC3_DEFINITIONS_ADDR=127.0.0.1:8091 go test ./internal/definitions/ -run TestDefinitionsListen -count=1
```

That process is an in-memory registry. From `frontend/`, with `AGENT_API_URL=http://127.0.0.1:8091`, open `/definitions`. The console rail links Definitions.

## Storage

Apply `agents/db/migrations/202610240006_nmc3_definitions.sql` after the Pass 1 migrations. Tables live in schema `nmc3`:

- `agent_definitions` — one row per agent id. `lifecycle_state` summarizes versions: published wins over testing, then draft, then retired. The unique name is the name from the latest write.
- `agent_versions` — one immutable published body per `(tenant, agent, version)`.
- `tool_definitions` and `tool_versions` — tool identity plus immutable versions.
- `run_definition_pins` — the spec a run used.

Triggers reject changes to a published spec, any change to a retired version, and any update or delete of a tool version or pin. Draft and testing rows can still be replaced. The database role that owns the tables can bypass grants; on Supabase, `service_role` receives DML and `anon` / `authenticated` do not. There is no browser API on these tables.

Every registry is one tenant. The default tenant is `local`. Pass 4 should keep the column and add the caller's tenant instead of dropping it.

`Open` uses `DATABASE_URL` (or `NMC3_TEST_DATABASE_URL` in tests). `NewMemory` is the same lifecycle without Postgres.

## Operator import

From the `agents` module, after the migration:

```go
reg, err := definitions.Open(ctx, os.Getenv("DATABASE_URL"))
body, err := os.ReadFile("examples/definitions.json")
result, err := reg.Import(ctx, "migration", body, definitions.ImportPublish)
```

Re-importing the same file is a no-op (`Unchanged`). A changed body for a published version fails the whole batch and leaves the previous rows in place.

## Errors

| Code | When |
| --- | --- |
| `INVALID_INPUT` | The spec, actor, or transition is rejected. `Fields` lists JSON paths. |
| `NOT_FOUND` | The version or pin is missing. |
| `RETIRED` | A new run or pin targets a retired version. |
| `IMMUTABLE` | A published body, tool version, or pin would change. |
| `CONFLICT` | The name is taken, or a backup does not match the stored row. |

`INVALID_INPUT` and `NOT_FOUND` use the same strings as the v1 run codes. `RETIRED`, `IMMUTABLE`, and `CONFLICT` are not run error codes. Do not copy them into `RunError` without a contract change. The HTTP handler maps invalid input to 400, not found to 404, and the other three to 409. The error object includes `fields` when a definition was rejected.

## What later passes must not break

- `examples/definitions.json` stays a JSON array of v1 definitions, with no required lifecycle or model-policy fields. `denial-specialist` may still delegate to `catalog-specialist`.
- Published specs, content hashes, publishers, and `published_at` do not change. A new body is a new version string.
- Retire does not rewrite the spec or existing pins. `Get` of a retired version still returns the frozen spec.
- Pins stay immutable. A run created on version A still resolves to A's hash after B is published and after A is retired.
- `catalog.search` version `1.0.0` stays the seeded read-only tool. Register a new tool version instead of updating that row.
- Delegate references use the same version string as the caller. Changing that requires a new schema version.
- New SQL columns need defaults. Update the immutability triggers in the same migration if a published row would otherwise be rewritten.
- Do not grant `anon` or `authenticated` access to these tables.
- Do not shrink the current limits: ids 256, versions 128, names 200, instructions 100000.
- Canonical form stays JSON. Do not add YAML or a second hash input. The website posts the same definition object as `PublishJSON`, and the stored hash is `Canonical`.

## Tests

From `agents/`:

```sh
go test ./internal/definitions/
```

`TestPostgresLifecycleAndTriggers` runs only when `NMC3_TEST_DATABASE_URL` is set. It applies the migration, then checks that a pin survives publish and retire and that SQL updates of a published spec and a pin are rejected.
