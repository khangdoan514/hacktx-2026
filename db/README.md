# db

Business schema history lives only in [`supabase/migrations`](../supabase/migrations), in the `app` schema. Do not add a second business migration sequence under `migrations/`.

`migrations/001_init.sql` is the original starter. It is not applied, and it is not the NMC3 agent schema. Agent tables, if added later, belong in a separate non-exposed schema such as `nmc3` and are owned by the agent store work. That schema is not created here. See [agents/docs/storage.md](../agents/docs/storage.md).

Local PostgreSQL without the Supabase CLI needs [`supabase/bootstrap_local.sql`](../supabase/bootstrap_local.sql) first, because that file creates a minimal `auth.users` table. A normal `supabase start` already has one. A plain Postgres run does not prove Supabase Auth, session refresh, or Storage policies.
