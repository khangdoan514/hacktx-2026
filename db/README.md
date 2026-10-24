# db

- `migrations/` – ordered SQL schema files (`001_init.sql`, `002_...`)
- `seeds/` – sample data for local development

The existing `001_init.sql` is starter scaffolding and is not the NMC3 agent schema. The agent system is specified to use Supabase Postgres; see [agents/docs/storage.md](../agents/docs/storage.md) for the data model and delivery sequence. Agent migrations will be created when the store is implemented. The current FastAPI backend is not yet connected to a database.
