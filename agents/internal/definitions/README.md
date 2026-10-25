# Definition registry

This package is the Pass 2 definition lifecycle. It stores agent definitions as versioned records, validates them, and pins a run to the exact version it started with. It does not serve HTTP and it does not edit the website. `agents/cmd/nmc3-agent` still loads `agents/examples/definitions.json` directly; wiring `ResolveForNewRun` and `PinRun` into run creation is later integrator work. The example file format is unchanged.

## Lifecycle

| State | Meaning |
| --- | --- |
| `draft` | Editable. Reference checks are not required yet. |
| `testing` | A draft that passed validation, including delegate and tool references. |
| `published` | Immutable. New runs may start only from this state. |
| `retired` | Immutable. Hidden from new runs. Existing pins and `Get` still return it. |

`SaveDraft` creates a version or replaces a draft. `Test` checks the stored draft and records the tester. `Publish` checks again, records the publisher, and freezes the version. Publishing the same bytes again does not change the publisher or timestamp. `Revise` saves a new version and leaves the source version alone. `Retire` is allowed only from `published`; a second retire keeps the first timestamp.

A draft may name a delegate that is not published yet. `Test` accepts a delegate in `draft`, `testing`, or `published` at the same version. `Publish` requires that delegate to already be published, or to be in the same import batch. Tool ids must be registered. `catalog.search` and `serpapi.search` version `1.0.0` are seeded for the fictional catalog and live web research examples. Seeding metadata does not grant an agent permission or configure provider credentials.

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

`MarshalDefinitions` writes `{"schema_version":"1","definitions":[...]}`. `Export` writes a backup with `tools` and `versions`, including lifecycle metadata. `Parse` accepts the example array and the definitions envelope. `Import` also accepts a backup. A backup restores the recorded publisher and state; the actor argument is not written over them. Restoring into a registry that already has a different body or state for that version fails the whole import.

Schema version `1` is the only version this package reads or writes. The database column is an open string so a later version can be stored beside it.

## Model policy

`model_policy` is optional. When it is absent, an empty model id means the service default, and any id accepted by `contracts.ValidModelID` is allowed. When `allowed_models` is set, the run model must be in that list. An empty model id is then allowed only if `default_model` is set, because this package does not know the process default. The policy does not start runs; `Version.AllowsModel` is the check a later pass should call before `PinRun`.

## Run pins

`PinRun` copies a published spec onto `(run id, agent id)`. The pin row is immutable. Publishing version B, or retiring version A, does not change a pin of A. `Pinned` returns that snapshot. `ResolveForNewRun` returns only a published version; a retired version returns `RETIRED` and a missing version returns `NOT_FOUND`.

Call `PinRun` for the root agent and again for each delegate at run start. Do not retarget an existing pin when a newer version is published.

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

`INVALID_INPUT` and `NOT_FOUND` use the same strings as the v1 run codes. `RETIRED`, `IMMUTABLE`, and `CONFLICT` are not run error codes. Do not copy them into `RunError` without a contract change. A later HTTP layer can map invalid input to 400, not found to 404, and the other three to 409.

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
- YAML and the website editor are not part of this package. Add them without a second canonical form; they must hash to the same bytes as `Canonical`.

## Tests

From `agents/`:

```sh
go test ./internal/definitions/
```

`TestPostgresLifecycleAndTriggers` runs only when `NMC3_TEST_DATABASE_URL` is set. It applies the migration, then checks that a pin survives publish and retire and that SQL updates of a published spec and a pin are rejected.
