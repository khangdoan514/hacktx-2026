# db

Supabase migrations in [`supabase/migrations`](../supabase/migrations) are the only schema history.

`migrations/001_init.sql` is the original starter and is not applied. Do not add a second migration sequence here.

Local PostgreSQL (no Supabase CLI) needs [`supabase/bootstrap_local.sql`](../supabase/bootstrap_local.sql) first, because that file creates a minimal `auth.users` table. A normal `supabase start` already has one.
