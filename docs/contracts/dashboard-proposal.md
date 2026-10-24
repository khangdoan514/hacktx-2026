# Dashboard proposal contract

This is an application contract. It is not the Go agent API and it does not accept external callbacks.

## Shape

A dashboard definition has `schema_version` 1, a title, an allowlisted widget list (`metric_number`, `capacity_table`, `margin_table`), optional filters, and source refs. Widgets name metric or dataset version ids. They do not contain SQL, HTML, or script.

Pydantic checks that shape only. It does not decide whether a source is visible.

## Authorization

Create, revise, and publish each load the project and every referenced metric version and dataset version. The caller must be a current project contributor or owner. The source must belong to the same workspace and be accepted or validated. A `restrictions` field on the request is ignored and cannot grant access. `base_revision` must equal the current revision.

## Provenance

The server writes provenance. A human session stores `actor_type: human` and the authenticated user id. Request JSON cannot set `actor_type`, including `integration`. `integration` is reserved for a future server credential and has no route today.

`actor_user_id` remains the revision author. `applied_by` is set when the revision is written. `published_by` is set when that person publishes. Those two columns are nullable only so older rows can migrate. New revisions always store attribution.

## Context manifest

The server builds a manifest from the sources it just authorized: project, workspace, target revision, those source ids, and the restriction `current_project_membership`. The manifest is not an access grant.

## Later agent service

A future Go `POST /v1/agent-runs` client is out of scope. That first contract has no authentication. This application does not call it and does not store agent run rows.
