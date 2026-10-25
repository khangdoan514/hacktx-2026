# Local install and rollback

This is a workstation procedure for a disposable database. It is not a production deployment. Apply it only to a Postgres instance or Supabase project you can throw away. `agents/ops/env.example` lists the environment names with every secret left blank. Put real values in a gitignored file or a shell you control, never in this tree.

The operations package does not change the `/v1` API or the checked-in schema. Supported request and response shapes remain the fixtures in `agents/fixtures/api/v1`.

## Prerequisites

- Go 1.23 or newer.
- A disposable Postgres database, or a disposable Supabase project, when you want to start the API.
- The `psql`, `pg_dump`, and `pg_restore` clients when you use direct Postgres. The backup and rollback commands below are written for that path.
- A local OpenCode process only if you start `cmd/nmc3-agent`. The metrics tests do not need it.

Record whether the database is local Postgres, managed Supabase, or self-hosted Supabase before you use it. Self-hosted Supabase is not provisioned by these steps. Managed and self-hosted backups are different products; these steps use `pg_dump` of the `nmc3` schema.

## Fresh install

Run commands from `agents/` unless a step says otherwise.

1. Confirm the operations package on its own:

   ```sh
   go test ./ops/metrics
   ```

2. Create a gitignored environment from [env.example](env.example). For the dump and restore steps, set `NMC3_STORE=postgres` and set `DATABASE_URL` to the disposable database. Leave `SUPABASE_SECRET_KEY` and `OPENCODE_SERVICE_PASSWORD` empty in any file that could be committed.

3. Apply the schema in filename order. Stop on the first error.

   ```sh
   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f db/migrations/202610240001_nmc3_pass1.sql
   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f db/migrations/202610240002_nmc3_data_api.sql
   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f db/migrations/202610240003_nmc3_event_source.sql
   ```

   On a disposable Supabase project, run those same three files in that order in the SQL editor. Prefer direct Postgres when you need the `pg_dump` path below. The SQL editor path does not by itself give you this rollback procedure. Re-run the three files in this order if you need to reapply them. Do not re-run `202610240002` by itself after `202610240003`: both files replace `nmc3_append_event`, and the later file is the one that writes `source`.

4. Start the API only when you intend to exercise it. From `agents/`:

   ```sh
   go run ./cmd/nmc3-agent
   ```

   The process listens on `127.0.0.1:8080` unless `NMC3_ADDR` is set. It reads definitions from `examples/definitions.json` unless `NMC3_DEFINITIONS_FILE` is set. OpenCode password resolution checks `OPENCODE_SERVICE_PASSWORD`, then `OPENCODE_SERVICE_FILE`, then `~/.config/opencode/service.json`. Startup fails when none of those is present. That password stays on the machine that already runs OpenCode.

5. Readiness for a local API is a successful `GET /v1/agents` against `http://127.0.0.1:8080`. A run that has already reached a terminal state is readable with `GET /v1/agent-runs/{run_id}` and `GET /v1/agent-runs/{run_id}/events`.

`go test ./...` from `agents/` is the module check. The Postgres integration test skips unless `NMC3_TEST_DATABASE_URL` is set. Do not point that variable at a database you cannot replace.

## What the schema change is

| Order | File | Effect on an existing local database |
| --- | --- | --- |
| 1 | `db/migrations/202610240001_nmc3_pass1.sql` | Creates the private `nmc3` schema and run, invocation, turn, tool, and event tables when they are absent |
| 2 | `db/migrations/202610240002_nmc3_data_api.sql` | Grants `service_role` and creates or replaces the service RPCs |
| 3 | `db/migrations/202610240003_nmc3_event_source.sql` | Adds `nmc3.run_events.source` when it is absent and replaces the event RPC |

There is no down migration. A failed `psql` invocation with `ON_ERROR_STOP` leaves the statements that already committed applied. Tables and the `source` column are created with `if not exists`, and the functions use `create or replace`. Reapplying the three files in order is the local repair. Dropping `nmc3` is not a repair.

## Backup

Take a backup before you change a database that already holds runs you care about. This dump contains the `nmc3` schema, including committed run rows. It does not contain Supabase Storage object bytes. The Pass 1 tables store run snapshots in Postgres; they do not store object-bucket payloads. If a later environment adds a private bucket, back that bucket up as a separate copy. A database restore does not bring those bytes back.

```sh
pg_dump --format=custom --schema=nmc3 --file="$HOME/nmc3-local.dump" "$DATABASE_URL"
```

Keep the dump on the workstation. Do not commit it. A dump can contain run inputs, so treat the file as private data even when the original task used fictional content.

## Roll back a failed deployment

Use this path when a new process or a bad local edit fails and the database still has the runs that were committed before the failure. Rolling back the process keeps those rows. Deleting the schema does not.

1. Stop the new process. Work that existed only in that process is not resumed. Runs already written to `nmc3.runs` stay written.
2. Start the last known-good binary against the same `DATABASE_URL` or the same Supabase project. Do not drop tables, schemas, or RPCs as part of this step.
3. Read one run that had reached `SUCCEEDED`, `FAILED`, or `CANCELLED` before the failure:

   ```sh
   curl -sS "http://127.0.0.1:8080/v1/agent-runs/RUN_ID"
   ```

   That response is the committed run. The previous binary must still show it.
4. Read `GET /v1/agent-runs/RUN_ID/events` and confirm the ordered events for that run are still present.
5. Leave a timed-out protected write alone. A process restart is not evidence that the write failed, and it is not a reason to send it again. See [runbook.md](runbook.md).

The checked-in migrations are forward-only. `nmc3.run_events.source` is a nullable column. Leave it in place when you roll the process back. Dropping the column or the schema is not process rollback. An exercise that boots an older binary against this schema was not run as part of this package.

## Restore a local backup

Use a restore when the disposable database itself was damaged and the dump is the copy you mean to return to. `pg_restore --clean` drops objects in the dump before recreating them. Run it only against a database whose current contents you intend to replace with that dump. The dump is what holds the committed runs from the time it was taken. Runs committed after the dump are not in the dump.

1. Stop the API process.
2. Restore into the disposable database:

   ```sh
   pg_restore --schema=nmc3 --clean --if-exists --dbname="$DATABASE_URL" "$HOME/nmc3-local.dump"
   ```

3. Start the known-good binary.
4. `GET` a run id that was committed before the dump was taken.
5. If the environment also had a private object bucket, restore that bucket from its own backup. This schema dump does not do that.

`DROP SCHEMA nmc3 CASCADE` deletes committed runs. It is not a rollback, and it is not a restore.

## Secret rotation

Rotate a local credential without writing the value into a log, a ticket, or git.

1. Issue the replacement in the database, Supabase, or OpenCode service you already control.
2. Update the gitignored environment. The process reads `SUPABASE_SECRET_KEY`, `OPENCODE_SERVICE_PASSWORD`, and `DATABASE_URL` from the environment at startup. `DATABASE_URL` can embed a password; prefer a local socket or a password file the database client already supports when you have a choice.
3. Stop the old process so it cannot keep using the previous value, then start it again.
4. `GET` a known committed run. Then revoke the previous credential.
5. If a shell transcript or a log file might contain the previous value, treat that file as exposed and remove it. `metrics.LogLine` and `metrics.Span` redact structured records passed through them. The current `cmd/nmc3-agent` logger does not call that package.

## Compatibility

- Supported API: the `/v1` shapes in `agents/internal/contracts` and `agents/fixtures/api/v1`. This package does not add a version and does not change those shapes.
- Supported schema: the three migrations above, applied in name order. A later migration has to keep committed run rows readable by the previous binary, or it has to ship a documented forward repair that does not drop those rows.
- Organization concurrency is an in-process control in `metrics.Limiter`. The API process does not read a concurrency environment variable. Two OS processes do not share one cap. Do not start extra copies and expect the cap to cover the sum.
- Upgrade, backup, and restore against a live shared environment are operator exercises. They are not executed by the package tests.
