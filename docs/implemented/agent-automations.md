# Agent automations MVP

Automations run a published agent definition at a scheduled time. They use the same private agent run endpoint as manual runs, so the agent's allowed tools and delegates remain defined by its published version. A project automation also receives project context at dispatch time, including notes saved since the schedule was created.

## Use

Open **Automations** in the console. Choose a published agent that accepts a `task` or `question` input, optionally choose a project, enter the prompt, and choose once, interval, daily, or weekly. Daily and weekly times use the IANA time zone shown in the form. The list shows the next run and recent run history. Pause, resume, and **Run now** are available for active schedules. A paused schedule cancels queued occurrences; an occurrence already dispatched may finish.

The API supports `POST /api/v1/agent-automations`, `GET /api/v1/agent-automations`, `PATCH /api/v1/agent-automations/{id}`, `POST /api/v1/agent-automations/{id}/run-now`, and `GET /api/v1/agent-automations/{id}/runs`. Creation and control require the `ai_engineer`, `operator`, or `administrator` role. Schedules and history are owner-scoped. Project access is checked on creation and again when a run starts.

## Operation

Apply `supabase/migrations/20261025071222_agent_automations.sql` to an existing database. In local/test mode the API applies migrations on startup. The API process starts an automation worker in every environment. It polls every five seconds, uses row locks and a unique `(automation_id, scheduled_for)` occurrence to avoid competing workers enqueuing the same due time, and leases dispatch work for recovery after a restart. Started agent runs are polled until they succeed, fail, or are cancelled.

Recurring schedules skip elapsed times after downtime and resume at the next future occurrence. An interval accepts 1 minute through 30 days. A local time that does not exist during a daylight-saving transition is skipped. The worker retries transient dispatch errors up to three times. Dispatch is **at least once**: a process failure after the agent accepts a start request but before its run ID is saved can start the same occurrence again. Tool actions should be idempotent when possible.

The agent service must be reachable through `AGENT_API_URL` and share `NMC3_INTERNAL_TOKEN` with the API. A scheduled agent's definition and any required tool connection must already be available to the agent service. An invalid or removed definition produces a failed occurrence visible in run history.
