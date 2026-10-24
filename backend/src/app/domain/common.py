import hashlib
import json
from decimal import Decimal

from psycopg.types.json import Json

from app.core.errors import AppError, request_id_var
from app.core.serialize import dump

VERSIONED_COLUMNS = {
    "workspaces": {"name", "timezone", "status"},
    "projects": {
        "name",
        "objective",
        "constraints",
        "scope",
        "state",
        "owner_id",
        "starts_on",
        "ends_on",
        "parent_id",
        "partner_id",
    },
    "requests": {"title", "state", "project_id", "converted_project_id", "converted_requirement_id"},
    "requirements": {"title", "state", "accepted_revision"},
    "tasks": {
        "title",
        "brief",
        "acceptance_criteria",
        "assignee_id",
        "priority",
        "due_on",
        "state",
        "blocked_reason",
        "parent_id",
    },
    "handoffs": {"state", "recipient_id", "accountable_id", "due_on", "current_revision"},
    "approval_requests": {"state", "target_revision"},
    "documents": {"title", "visibility"},
}


def limit_of(value: int | None) -> int:
    if value is None:
        return 50
    if value < 1 or value > 100:
        raise AppError(422, "validation", "limit must be between 1 and 100", {"limit": "out of range"})
    return value


def one(conn, sql: str, params: tuple, message: str = "Record not found"):
    row = conn.execute(sql, params).fetchone()
    if not row:
        raise AppError(404, "not_found", message)
    return row


def require_workspace(conn, workspace_id: str, roles: set[str] | None = None):
    row = conn.execute(
        """
        SELECT w.*, m.access_role
        FROM app.workspaces w
        JOIN app.workspace_memberships m
          ON m.workspace_id = w.id
         AND m.user_id = app.current_user_id()
         AND m.status = 'active'
        WHERE w.id = %s
        """,
        (workspace_id,),
    ).fetchone()
    if not row:
        raise AppError(404, "not_found", "Workspace not found")
    if roles and row["access_role"] not in roles:
        raise AppError(403, "forbidden", "You cannot administer this workspace")
    return row


def require_project(conn, project_id: str, roles: set[str] | None = None):
    row = conn.execute(
        """
        SELECT p.*, m.access_role
        FROM app.projects p
        JOIN app.project_memberships m
          ON m.project_id = p.id
         AND m.user_id = app.current_user_id()
         AND m.status = 'active'
        WHERE p.id = %s
        """,
        (project_id,),
    ).fetchone()
    if not row:
        raise AppError(404, "not_found", "Project not found")
    if roles and row["access_role"] not in roles:
        raise AppError(403, "forbidden", "You cannot do that on this project")
    return row


def ensure_open(project) -> None:
    if project["state"] != "active":
        raise AppError(409, "archived", "This project is not open for changes")


def update_version(conn, table: str, record_id: str, expected: int, assignments: dict):
    allowed = VERSIONED_COLUMNS[table]
    unknown = set(assignments) - allowed
    if unknown:
        raise AppError(422, "validation", "Unknown fields", {"fields": sorted(unknown)})
    sets = []
    values: list = []
    for key, value in assignments.items():
        sets.append(f"{key} = %s")
        values.append(value)
    sets.append("version = version + 1")
    sets.append("updated_at = now()")
    row = conn.execute(
        f"UPDATE app.{table} SET {', '.join(sets)} WHERE id = %s AND version = %s RETURNING *",
        (*values, record_id, expected),
    ).fetchone()
    if row:
        return row
    current = conn.execute(f"SELECT version FROM app.{table} WHERE id = %s", (record_id,)).fetchone()
    if not current:
        raise AppError(404, "not_found", "Record not found")
    raise AppError(
        409,
        "conflict",
        "The record changed. Reload and try again.",
        {"version": current["version"]},
    )


def emit(
    conn,
    *,
    actor_id: str,
    action: str,
    workspace_id,
    project_id,
    resource_type: str,
    resource_id,
    revision: int | None,
    recipients: list[str],
    preview: str,
    extra: dict | None = None,
) -> None:
    audience = [str(item) for item in recipients if str(item) != str(actor_id)]
    conn.execute(
        """
        INSERT INTO app.audit_events (
            workspace_id, project_id, actor_user_id, action,
            resource_type, resource_id, revision, request_id
        ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s)
        """,
        (
            workspace_id,
            project_id,
            actor_id,
            action,
            resource_type,
            resource_id,
            revision,
            request_id_var.get(),
        ),
    )
    conn.execute(
        """
        INSERT INTO app.outbox_events (
            workspace_id, project_id, resource_type, resource_id, revision, kind, payload
        ) VALUES (%s, %s, %s, %s, %s, %s, %s)
        """,
        (
            workspace_id,
            project_id,
            resource_type,
            resource_id,
            revision,
            action,
            Json(
                {
                    "recipients": audience,
                    "preview": preview[:240],
                    "target_type": resource_type,
                    "target_id": str(resource_id),
                    "extra": extra or {},
                }
            ),
        ),
    )


def request_hash(body: dict) -> str:
    encoded = json.dumps(body, sort_keys=True, default=str).encode()
    return hashlib.sha256(encoded).hexdigest()


def idempotency_begin(conn, user_id: str, scope: str, key: str | None, body: dict):
    if not key:
        return None
    digest = request_hash(body)
    row = conn.execute(
        """
        SELECT status_code, response, request_hash
        FROM app.idempotency_keys
        WHERE user_id = %s AND scope = %s AND key = %s
        """,
        (user_id, scope, key),
    ).fetchone()
    if row:
        if row["request_hash"] != digest:
            raise AppError(409, "idempotency_conflict", "This key was already used for a different request")
        return {"replay": True, "status": row["status_code"], "body": dump(row["response"])}
    return {"replay": False, "digest": digest, "key": key, "scope": scope}


def idempotency_finish(conn, user_id: str, begun, status: int, body: dict) -> None:
    if not begun or begun.get("replay"):
        return
    conn.execute(
        """
        INSERT INTO app.idempotency_keys (user_id, scope, key, request_hash, status_code, response)
        VALUES (%s, %s, %s, %s, %s, %s)
        """,
        (user_id, begun["scope"], begun["key"], begun["digest"], status, Json(body)),
    )


def money(value) -> Decimal | None:
    if value is None or value == "":
        return None
    return Decimal(str(value))
