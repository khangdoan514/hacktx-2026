# Implemented collaboration behavior

This note describes requests, requirements, tasks, handoffs, comments, documents, notifications, and audit behavior worth keeping. Dashboard revisions are out of scope.

## Requests

`app.requests` moves through explicit commands, not a generic status write. The allowed edges in `REQUEST_NEXT` are:

- `draft` to `submitted`
- `submitted` to `triaging`
- `triaging` to `needs_information`, `accepted`, or `rejected`
- `needs_information` back to `submitted`

`accepted` and `rejected` have no further request transition. An accepted request can be converted once into a project requirement bundle. Conversion is idempotent: the same key and body replay the original result, and a different body for that key is rejected.

Updates compare `expected_version` and increment `version` in the same statement.

## Requirements

Requirement states are `draft`, `in_review`, `accepted`, and `changes_requested`.

- `draft` to `in_review`
- `in_review` to `accepted` or `changes_requested`
- `changes_requested` to `draft`

Editing the body, acceptance criteria, or priority inserts a new `app.requirement_revisions` row. Older revision rows are not rewritten. Acceptance records `accepted_revision` only when the latest revision is the one under review.

## Tasks

Task states are `todo`, `in_progress`, `in_review`, `changes_requested`, `done`, and `cancelled`. Completion requires acceptance criteria and an evidence string. Dependencies reject cycles. A task moving to `done` through the approval function is a separate path from the task transition command. Marking a task done does not by itself satisfy an approval that is still pending.

## Handoffs

Handoff states are `draft`, `sent`, `acknowledged`, `needs_information`, `completed`, and `cancelled`. Sending stores an immutable snapshot of the objective and requested output. Later transitions append `app.handoff_events`. The recipient must be a project member.

## Approvals

An approval is pending until `approved`, `changes_requested`, or `rejected`. The author cannot decide it. The assigned reviewer must match the caller. The decision's `target_revision` and `expected_version` must match the row. The target update, the approval update, the decision row, and the idempotency record commit together. The conditional update must change one row. Concurrent decisions produce one winner. The other gets a conflict.

The target update itself is the scope defect described in `access.md`.

## Comments, notifications, audit

Comments are `internal` or `shared`. Shared comments are visible to partner members. Internal comments are not.

Commands that should be visible to other people call `emit`, which writes `app.audit_events` and `app.outbox_events` in the same transaction. Audit rows are append-only from the application. Notifications are per user and can be marked read one at a time or for a workspace.

`my_work` lists the caller's open tasks, reviews, and handoffs in one workspace.

## Documents

Upload is initiate, write bytes, then finalize. Finalize stores a checksum and size. Download reads the private file for a project member. Bytes live on local disk under `STORAGE_DIR` (`var/storage` by default). That directory is not Supabase Storage, and these tests do not prove Storage policies.
