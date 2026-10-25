# Project agent context

The application API stores durable agent notes, reports, and completed run outputs in `app.project_agent_context`. The `app.project_agent_shares` table records which agents a project member has made available to the project. Both tables use PostgreSQL row level security tied to active `app.delivery_grants` and active NMC2 employees. A private entry is visible only to its owner; a project entry is visible to active project members. Removing a project grant immediately removes read access. Entries are soft deleted so historical run references remain intact.

The application API exposes:

- `GET /api/v1/agent-projects` — projects the employee can access.
- `GET|POST /api/v1/projects/{id}/agent-context` — list or create notes and reports. POST accepts `agent_id`, `kind` (`memory` or `report`), `title`, `body`, and `visibility` (`private` or `project`).
- `DELETE /api/v1/projects/{id}/agent-context/{entry}` — delete an entry owned by the caller.
- `GET|POST /api/v1/projects/{id}/agents` and `DELETE /api/v1/projects/{id}/agents/{agent}` — list, share, or unshare agents for the project.

The Agents page shows these controls for project members. The private orchestration API accepts `project_id` on `POST /agent-api/agent-runs`. The application proxy checks project membership, discards any caller supplied `actor_id` or `shared_context`, and sends up to 12 visible context entries to the agent. The agent prompt labels them as untrusted data. A successful project run calls back to the application API and saves its output as a `run_output` entry. It is project visible when the agent is shared with the project and private otherwise. The next project run can read it.

The callback uses `NMC3_INTERNAL_TOKEN` and `NMC3_APP_API_URL`. The application checks the project grant again before saving, so an employee removed during a run cannot publish a late result. The callback is idempotent by `(project_id, source_run_id)`; failures are logged by the agent process. This is a best effort callback, not a durable retry queue.

The migration is `supabase/migrations/20261025054535_project_agent_context.sql`. Local API startup applies it automatically. For Docker, use the repository root `.env` plus `agents/.env` and `frontend/.env`; `compose.yaml` sets the callback URL to `http://api:8000`. The business API and agent service retain separate databases.

The connected Supabase project currently has the standalone `nmc3` agent schema but no `app` schema. This migration therefore runs against the Docker application database. Deploying the business API to Supabase requires a separate compatible application database; the existing agent database cannot receive the application migrations because both define different `nmc3.runs` tables.

Large reports are saved up to 64 KiB; only the first 8 KiB of an entry is eligible for prompt context. The current implementation loads the most recent eligible entries and favors the running agent's own entries. It does not yet schedule weekly runs or fetch GitHub data; those are separate tool and scheduling concerns.
