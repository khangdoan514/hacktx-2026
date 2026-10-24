from uuid import uuid4

from psycopg.types.json import Json

from app.core.errors import AppError
from app.core.serialize import dump
from app.domain.common import (
    emit,
    ensure_open,
    idempotency_begin,
    idempotency_finish,
    limit_of,
    one,
    require_project,
    require_workspace,
    update_version,
)

REQUEST_NEXT = {
    "draft": {"submitted"},
    "submitted": {"triaging"},
    "triaging": {"needs_information", "accepted", "rejected"},
    "needs_information": {"submitted"},
    "accepted": set(),
    "rejected": set(),
}
REQUIREMENT_NEXT = {
    "draft": {"in_review"},
    "in_review": {"accepted", "changes_requested"},
    "changes_requested": {"draft"},
    "accepted": set(),
}
TASK_NEXT = {
    "todo": {"in_progress", "cancelled"},
    "in_progress": {"in_review", "cancelled"},
    "in_review": {"done", "changes_requested", "cancelled"},
    "changes_requested": {"in_progress", "cancelled"},
    "done": {"in_progress"},
    "cancelled": set(),
}
HANDOFF_NEXT = {
    "draft": {"sent", "cancelled"},
    "sent": {"acknowledged", "needs_information", "cancelled"},
    "acknowledged": {"completed", "needs_information", "cancelled"},
    "needs_information": {"sent", "cancelled"},
    "completed": set(),
    "cancelled": set(),
}


def _transition(current: str, target: str, table: dict, label: str) -> None:
    if target not in table.get(current, set()):
        raise AppError(409, "invalid_state", f"Cannot move {label} from {current} to {target}")


def _member_ids(conn, project_id: str) -> list[str]:
    rows = conn.execute(
        """
        SELECT user_id FROM app.project_memberships
        WHERE project_id = %s AND status = 'active'
        """,
        (project_id,),
    ).fetchall()
    return [str(row["user_id"]) for row in rows]


def create_request(conn, actor, workspace_id: str, body: dict) -> dict:
    require_workspace(conn, workspace_id)
    row = conn.execute(
        """
        INSERT INTO app.requests (workspace_id, project_id, requester_id, title, state)
        VALUES (%s, %s, %s, %s, 'draft')
        RETURNING *
        """,
        (workspace_id, body.get("project_id"), actor.user_id, body["title"].strip()),
    ).fetchone()
    revision = conn.execute(
        """
        INSERT INTO app.request_revisions (
            request_id, workspace_id, revision, outcome, constraints, due_on, actor_user_id
        ) VALUES (%s, %s, 1, %s, %s, %s, %s)
        RETURNING *
        """,
        (
            row["id"],
            workspace_id,
            body.get("outcome") or "",
            body.get("constraints") or "",
            body.get("due_on"),
            actor.user_id,
        ),
    ).fetchone()
    emit(
        conn,
        actor_id=actor.user_id,
        action="request.drafted",
        workspace_id=workspace_id,
        project_id=body.get("project_id"),
        resource_type="request",
        resource_id=row["id"],
        revision=1,
        recipients=[],
        preview=row["title"],
    )
    payload = dump(dict(row))
    payload["revision"] = dump(dict(revision))
    return payload


def list_requests(conn, workspace_id: str, limit: int | None) -> dict:
    require_workspace(conn, workspace_id)
    rows = conn.execute(
        """
        SELECT * FROM app.requests
        WHERE workspace_id = %s
        ORDER BY created_at, id
        LIMIT %s
        """,
        (workspace_id, limit_of(limit)),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def get_request(conn, request_id: str) -> dict:
    row = one(conn, "SELECT * FROM app.requests WHERE id = %s", (request_id,), "Request not found")
    revisions = conn.execute(
        """
        SELECT * FROM app.request_revisions
        WHERE request_id = %s
        ORDER BY revision
        """,
        (request_id,),
    ).fetchall()
    payload = dump(dict(row))
    payload["revisions"] = dump([dict(item) for item in revisions])
    return payload


def transition_request(conn, actor, request_id: str, expected: int, target: str, reason: str = "") -> dict:
    current = one(conn, "SELECT * FROM app.requests WHERE id = %s", (request_id,), "Request not found")
    workspace = require_workspace(conn, str(current["workspace_id"]))
    if target == "submitted" and str(current["requester_id"]) != actor.user_id:
        raise AppError(403, "forbidden", "Only the requester can submit this request")
    if target != "submitted" and workspace["access_role"] not in {"owner", "administrator", "member"}:
        raise AppError(403, "forbidden", "You cannot triage this request")
    _transition(current["state"], target, REQUEST_NEXT, "request")
    if target == "rejected" and not reason.strip():
        raise AppError(422, "validation", "A rejection needs a reason", {"reason": "required"})
    row = update_version(conn, "requests", request_id, expected, {"state": target})
    latest = conn.execute(
        "SELECT COALESCE(MAX(revision), 0) AS revision FROM app.request_revisions WHERE request_id = %s",
        (request_id,),
    ).fetchone()
    conn.execute(
        """
        INSERT INTO app.request_revisions (
            request_id, workspace_id, revision, outcome, constraints, actor_user_id
        )
        SELECT request_id, workspace_id, %s, outcome, %s, %s
        FROM app.request_revisions
        WHERE request_id = %s
        ORDER BY revision DESC
        LIMIT 1
        """,
        (latest["revision"] + 1, reason or "", actor.user_id, request_id),
    )
    emit(
        conn,
        actor_id=actor.user_id,
        action=f"request.{target}",
        workspace_id=current["workspace_id"],
        project_id=current["project_id"],
        resource_type="request",
        resource_id=request_id,
        revision=row["version"],
        recipients=[str(current["requester_id"])],
        preview=f"Request {target}",
    )
    return get_request(conn, request_id)


def convert_request(conn, actor, request_id: str, idempotency_key: str | None) -> dict:
    begun = idempotency_begin(conn, actor.user_id, "request-convert", idempotency_key, {"request_id": request_id})
    if begun and begun.get("replay"):
        return begun
    current = conn.execute(
        "SELECT * FROM app.requests WHERE id = %s FOR UPDATE",
        (request_id,),
    ).fetchone()
    if not current:
        raise AppError(404, "not_found", "Request not found")
    workspace = require_workspace(conn, str(current["workspace_id"]), {"owner", "administrator", "member"})
    if current["converted_project_id"]:
        body = {
            "request_id": str(current["id"]),
            "project_id": str(current["converted_project_id"]),
            "requirement_id": str(current["converted_requirement_id"]) if current["converted_requirement_id"] else None,
        }
        return {"replay": True, "status": 200, "body": body}
    if current["state"] != "accepted":
        raise AppError(409, "invalid_state", "Only an accepted request can be converted")
    revision = conn.execute(
        """
        SELECT * FROM app.request_revisions
        WHERE request_id = %s
        ORDER BY revision DESC
        LIMIT 1
        """,
        (request_id,),
    ).fetchone()
    project_id = uuid4()
    conn.execute(
        """
        INSERT INTO app.projects (
            id, workspace_id, name, project_type, objective, constraints, owner_id
        ) VALUES (%s, %s, %s, 'internal', %s, %s, %s)
        """,
        (
            project_id,
            current["workspace_id"],
            current["title"],
            revision["outcome"] if revision else "",
            revision["constraints"] if revision else "",
            actor.user_id,
        ),
    )
    project = {"id": project_id}
    conn.execute(
        """
        INSERT INTO app.project_memberships (project_id, user_id, workspace_id, access_role, status)
        VALUES (%s, %s, %s, 'owner', 'active')
        """,
        (project_id, actor.user_id, current["workspace_id"]),
    )
    conn.execute(
        """
        INSERT INTO app.project_internal (project_id, workspace_id)
        VALUES (%s, %s)
        """,
        (project["id"], current["workspace_id"]),
    )
    if str(current["requester_id"]) != actor.user_id:
        requester_role = conn.execute(
            """
            SELECT access_role FROM app.workspace_memberships
            WHERE workspace_id = %s AND user_id = %s AND status = 'active'
            """,
            (current["workspace_id"], current["requester_id"]),
        ).fetchone()
        project_role = "partner" if requester_role and requester_role["access_role"] == "guest" else "contributor"
        conn.execute(
            """
            INSERT INTO app.project_memberships (project_id, user_id, workspace_id, access_role, status)
            VALUES (%s, %s, %s, %s, 'active')
            ON CONFLICT DO NOTHING
            """,
            (project["id"], current["requester_id"], current["workspace_id"], project_role),
        )
    requirement = conn.execute(
        """
        INSERT INTO app.requirements (workspace_id, project_id, request_id, title, state, accepted_revision)
        VALUES (%s, %s, %s, %s, 'accepted', 1)
        RETURNING *
        """,
        (current["workspace_id"], project["id"], request_id, current["title"]),
    ).fetchone()
    conn.execute(
        """
        INSERT INTO app.requirement_revisions (
            requirement_id, workspace_id, project_id, revision, body, acceptance_criteria, actor_user_id
        ) VALUES (%s, %s, %s, 1, %s, '', %s)
        """,
        (
            requirement["id"],
            current["workspace_id"],
            project["id"],
            revision["outcome"] if revision else current["title"],
            actor.user_id,
        ),
    )
    conn.execute(
        """
        UPDATE app.requests
        SET converted_project_id = %s,
            converted_requirement_id = %s,
            version = version + 1,
            updated_at = now()
        WHERE id = %s
        """,
        (project["id"], requirement["id"], request_id),
    )
    body = {
        "request_id": str(request_id),
        "project_id": str(project["id"]),
        "requirement_id": str(requirement["id"]),
    }
    idempotency_finish(conn, actor.user_id, begun, 200, body)
    emit(
        conn,
        actor_id=actor.user_id,
        action="request.converted",
        workspace_id=current["workspace_id"],
        project_id=project["id"],
        resource_type="request",
        resource_id=request_id,
        revision=current["version"] + 1,
        recipients=[str(current["requester_id"])],
        preview="Request converted to project work",
    )
    _ = workspace
    return {"replay": False, "status": 200, "body": body}


def create_requirement(conn, actor, project_id: str, body: dict) -> dict:
    project = require_project(conn, project_id, {"owner", "contributor"})
    ensure_open(project)
    row = conn.execute(
        """
        INSERT INTO app.requirements (workspace_id, project_id, request_id, title)
        VALUES (%s, %s, %s, %s)
        RETURNING *
        """,
        (project["workspace_id"], project_id, body.get("request_id"), body["title"].strip()),
    ).fetchone()
    conn.execute(
        """
        INSERT INTO app.requirement_revisions (
            requirement_id, workspace_id, project_id, revision, body, acceptance_criteria, priority, actor_user_id
        ) VALUES (%s, %s, %s, 1, %s, %s, %s, %s)
        """,
        (
            row["id"],
            project["workspace_id"],
            project_id,
            body.get("body") or "",
            body.get("acceptance_criteria") or "",
            body.get("priority") or "medium",
            actor.user_id,
        ),
    )
    emit(
        conn,
        actor_id=actor.user_id,
        action="requirement.drafted",
        workspace_id=project["workspace_id"],
        project_id=project_id,
        resource_type="requirement",
        resource_id=row["id"],
        revision=1,
        recipients=[],
        preview=row["title"],
    )
    return get_requirement(conn, str(row["id"]))


def get_requirement(conn, requirement_id: str) -> dict:
    row = one(conn, "SELECT * FROM app.requirements WHERE id = %s", (requirement_id,), "Requirement not found")
    revisions = conn.execute(
        "SELECT * FROM app.requirement_revisions WHERE requirement_id = %s ORDER BY revision",
        (requirement_id,),
    ).fetchall()
    payload = dump(dict(row))
    payload["revisions"] = dump([dict(item) for item in revisions])
    return payload


def list_requirements(conn, project_id: str) -> dict:
    require_project(conn, project_id, {"owner", "contributor", "reviewer", "viewer"})
    rows = conn.execute(
        "SELECT * FROM app.requirements WHERE project_id = %s ORDER BY created_at, id",
        (project_id,),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def transition_requirement(conn, actor, requirement_id: str, expected: int, target: str) -> dict:
    current = one(conn, "SELECT * FROM app.requirements WHERE id = %s", (requirement_id,), "Requirement not found")
    require_project(conn, str(current["project_id"]), {"owner", "contributor", "reviewer"})
    _transition(current["state"], target, REQUIREMENT_NEXT, "requirement")
    if target == "accepted":
        latest = conn.execute(
            "SELECT MAX(revision) AS revision FROM app.requirement_revisions WHERE requirement_id = %s",
            (requirement_id,),
        ).fetchone()
        row = update_version(
            conn,
            "requirements",
            requirement_id,
            expected,
            {"state": target, "accepted_revision": latest["revision"]},
        )
    else:
        row = update_version(conn, "requirements", requirement_id, expected, {"state": target})
    emit(
        conn,
        actor_id=actor.user_id,
        action=f"requirement.{target}",
        workspace_id=current["workspace_id"],
        project_id=current["project_id"],
        resource_type="requirement",
        resource_id=requirement_id,
        revision=row["version"],
        recipients=[],
        preview=f"Requirement {target}",
    )
    return get_requirement(conn, requirement_id)


def add_requirement_revision(conn, actor, requirement_id: str, body: dict) -> dict:
    current = one(conn, "SELECT * FROM app.requirements WHERE id = %s", (requirement_id,), "Requirement not found")
    project = require_project(conn, str(current["project_id"]), {"owner", "contributor"})
    ensure_open(project)
    if current["state"] == "accepted":
        raise AppError(409, "invalid_state", "Accepted requirements need a new draft revision via changes")
    latest = conn.execute(
        "SELECT COALESCE(MAX(revision), 0) AS revision FROM app.requirement_revisions WHERE requirement_id = %s",
        (requirement_id,),
    ).fetchone()
    revision_no = latest["revision"] + 1
    conn.execute(
        """
        INSERT INTO app.requirement_revisions (
            requirement_id, workspace_id, project_id, revision, body, acceptance_criteria, priority, actor_user_id
        ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s)
        """,
        (
            requirement_id,
            current["workspace_id"],
            current["project_id"],
            revision_no,
            body.get("body") or "",
            body.get("acceptance_criteria") or "",
            body.get("priority") or "medium",
            actor.user_id,
        ),
    )
    if current["state"] == "changes_requested":
        update_version(conn, "requirements", requirement_id, body["expected_version"], {"state": "draft"})
    return get_requirement(conn, requirement_id)


def create_task(conn, actor, project_id: str, body: dict) -> dict:
    project = require_project(conn, project_id, {"owner", "contributor"})
    ensure_open(project)
    assignee = body.get("assignee_id")
    if assignee:
        _assert_project_member(conn, project_id, assignee)
    if body.get("parent_id"):
        parent = one(conn, "SELECT project_id FROM app.tasks WHERE id = %s", (body["parent_id"],), "Parent task not found")
        if str(parent["project_id"]) != str(project_id):
            raise AppError(422, "validation", "Parent task is in another project")
    row = conn.execute(
        """
        INSERT INTO app.tasks (
            workspace_id, project_id, parent_id, requirement_id, title, brief,
            acceptance_criteria, assignee_id, priority, due_on
        ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
        RETURNING *
        """,
        (
            project["workspace_id"],
            project_id,
            body.get("parent_id"),
            body.get("requirement_id"),
            body["title"].strip(),
            body.get("brief") or "",
            body.get("acceptance_criteria") or "",
            assignee,
            body.get("priority") or 3,
            body.get("due_on"),
        ),
    ).fetchone()
    emit(
        conn,
        actor_id=actor.user_id,
        action="task.created",
        workspace_id=project["workspace_id"],
        project_id=project_id,
        resource_type="task",
        resource_id=row["id"],
        revision=row["version"],
        recipients=[assignee] if assignee else [],
        preview=row["title"],
    )
    return dump(dict(row))


def list_tasks(conn, project_id: str, limit: int | None) -> dict:
    require_project(conn, project_id)
    rows = conn.execute(
        """
        SELECT * FROM app.tasks
        WHERE project_id = %s
        ORDER BY priority, created_at, id
        LIMIT %s
        """,
        (project_id, limit_of(limit)),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def get_task(conn, task_id: str) -> dict:
    return dump(dict(one(conn, "SELECT * FROM app.tasks WHERE id = %s", (task_id,), "Task not found")))


def update_task(conn, actor, task_id: str, expected: int, fields: dict) -> dict:
    current = one(conn, "SELECT * FROM app.tasks WHERE id = %s", (task_id,), "Task not found")
    project = require_project(conn, str(current["project_id"]), {"owner", "contributor"})
    ensure_open(project)
    if fields.get("assignee_id"):
        _assert_project_member(conn, str(current["project_id"]), fields["assignee_id"])
    assignments = {key: value for key, value in fields.items() if key != "expected_version" and value is not None}
    assignments.pop("state", None)
    row = update_version(conn, "tasks", task_id, expected, assignments)
    emit(
        conn,
        actor_id=actor.user_id,
        action="task.updated",
        workspace_id=current["workspace_id"],
        project_id=current["project_id"],
        resource_type="task",
        resource_id=task_id,
        revision=row["version"],
        recipients=[str(row["assignee_id"])] if row["assignee_id"] else [],
        preview=row["title"],
    )
    return dump(dict(row))


def transition_task(conn, actor, task_id: str, expected: int, target: str, reason: str = "", evidence: str = "") -> dict:
    current = one(conn, "SELECT * FROM app.tasks WHERE id = %s", (task_id,), "Task not found")
    require_project(conn, str(current["project_id"]))
    project = require_project(conn, str(current["project_id"]), {"owner", "contributor", "reviewer"})
    ensure_open(project)
    _transition(current["state"], target, TASK_NEXT, "task")
    if target == "cancelled" and not reason.strip():
        raise AppError(422, "validation", "Cancellation needs a reason")
    if target == "done":
        if not current["acceptance_criteria"].strip():
            raise AppError(422, "validation", "Completion needs acceptance criteria")
        if not evidence.strip():
            raise AppError(422, "validation", "Completion needs evidence")
    if target == "in_progress" and current["state"] == "done" and not reason.strip():
        raise AppError(422, "validation", "Reopening needs a reason")
    blocked = reason if target in {"cancelled", "in_progress"} and reason else current["blocked_reason"]
    if target == "in_progress" and current["state"] == "done":
        blocked = reason
    row = update_version(conn, "tasks", task_id, expected, {"state": target, "blocked_reason": blocked if target != "done" else current["blocked_reason"]})
    emit(
        conn,
        actor_id=actor.user_id,
        action=f"task.{target}",
        workspace_id=current["workspace_id"],
        project_id=current["project_id"],
        resource_type="task",
        resource_id=task_id,
        revision=row["version"],
        recipients=[str(current["assignee_id"])] if current["assignee_id"] else [],
        preview=evidence or reason or f"Task {target}",
    )
    return dump(dict(row))


def add_dependency(conn, actor, task_id: str, depends_on: str) -> dict:
    current = one(conn, "SELECT * FROM app.tasks WHERE id = %s", (task_id,), "Task not found")
    other = one(conn, "SELECT * FROM app.tasks WHERE id = %s", (depends_on,), "Dependency not found")
    require_project(conn, str(current["project_id"]), {"owner", "contributor"})
    if str(current["project_id"]) != str(other["project_id"]):
        raise AppError(422, "validation", "Dependencies must stay in the same project")
    if task_id == depends_on:
        raise AppError(422, "task_cycle", "A task cannot depend on itself")
    cycle = conn.execute(
        """
        WITH RECURSIVE chain AS (
            SELECT %s::uuid AS id
            UNION
            SELECT d.depends_on
            FROM app.task_dependencies d
            JOIN chain c ON d.task_id = c.id
        )
        SELECT EXISTS (SELECT 1 FROM chain WHERE id = %s) AS cycle
        """,
        (depends_on, task_id),
    ).fetchone()
    if cycle["cycle"]:
        raise AppError(422, "task_cycle", "That dependency would create a cycle")
    conn.execute(
        """
        INSERT INTO app.task_dependencies (task_id, depends_on, workspace_id, project_id)
        VALUES (%s, %s, %s, %s)
        """,
        (task_id, depends_on, current["workspace_id"], current["project_id"]),
    )
    _ = actor
    return {"task_id": task_id, "depends_on": depends_on}


def _assert_project_member(conn, project_id: str, user_id: str) -> None:
    row = conn.execute(
        """
        SELECT 1 FROM app.project_memberships
        WHERE project_id = %s AND user_id = %s AND status = 'active'
        """,
        (project_id, user_id),
    ).fetchone()
    if not row:
        raise AppError(422, "invalid_assignee", "That person is not a project member")


def create_handoff(conn, actor, project_id: str, body: dict) -> dict:
    project = require_project(conn, project_id, {"owner", "contributor"})
    ensure_open(project)
    row = conn.execute(
        """
        INSERT INTO app.handoffs (workspace_id, project_id, created_by, due_on)
        VALUES (%s, %s, %s, %s)
        RETURNING *
        """,
        (project["workspace_id"], project_id, actor.user_id, body.get("due_on")),
    ).fetchone()
    return dump(dict(row))


def send_handoff(conn, actor, handoff_id: str, expected: int, body: dict) -> dict:
    current = one(conn, "SELECT * FROM app.handoffs WHERE id = %s", (handoff_id,), "Handoff not found")
    project = require_project(conn, str(current["project_id"]), {"owner", "contributor"})
    ensure_open(project)
    target = "sent"
    _transition(current["state"], target, HANDOFF_NEXT, "handoff")
    for field in ("objective", "requested_output", "recipient_id"):
        if not body.get(field):
            raise AppError(422, "validation", f"{field} is required", {field: "required"})
    _assert_project_member(conn, str(current["project_id"]), body["recipient_id"])
    accountable = body.get("accountable_id") or body["recipient_id"]
    _assert_project_member(conn, str(current["project_id"]), accountable)
    revision_no = current["current_revision"] + 1
    snapshot = {
        "objective": body["objective"],
        "requested_output": body["requested_output"],
        "recipient_id": body["recipient_id"],
        "accountable_id": accountable,
        "due_on": body.get("due_on"),
        "assumptions": body.get("assumptions") or "",
        "constraints": body.get("constraints") or "",
        "findings": body.get("findings") or "",
        "open_questions": body.get("open_questions") or [],
        "source_references": body.get("source_references") or [],
    }
    conn.execute(
        """
        INSERT INTO app.handoff_revisions (handoff_id, revision, snapshot, actor_user_id)
        VALUES (%s, %s, %s, %s)
        """,
        (handoff_id, revision_no, Json(snapshot), actor.user_id),
    )
    row = update_version(
        conn,
        "handoffs",
        handoff_id,
        expected,
        {
            "state": "sent",
            "recipient_id": body["recipient_id"],
            "accountable_id": accountable,
            "due_on": body.get("due_on") or current["due_on"],
            "current_revision": revision_no,
        },
    )
    _handoff_event(conn, current, actor.user_id, "sent", body["objective"])
    emit(
        conn,
        actor_id=actor.user_id,
        action="handoff.sent",
        workspace_id=current["workspace_id"],
        project_id=current["project_id"],
        resource_type="handoff",
        resource_id=handoff_id,
        revision=revision_no,
        recipients=[body["recipient_id"]],
        preview=body["objective"],
    )
    return _handoff_payload(conn, row)


def transition_handoff(conn, actor, handoff_id: str, expected: int, target: str, note: str = "", recipient_id: str | None = None) -> dict:
    current = one(conn, "SELECT * FROM app.handoffs WHERE id = %s", (handoff_id,), "Handoff not found")
    require_project(conn, str(current["project_id"]))
    _transition(current["state"], target, HANDOFF_NEXT, "handoff")
    if target == "needs_information" and not note.strip():
        raise AppError(422, "validation", "Say what information is missing")
    assignments = {"state": target}
    recipients = [str(current["created_by"])]
    if target == "sent" and recipient_id:
        _assert_project_member(conn, str(current["project_id"]), recipient_id)
        assignments["recipient_id"] = recipient_id
        assignments["accountable_id"] = recipient_id
        recipients = [recipient_id]
    row = update_version(conn, "handoffs", handoff_id, expected, assignments)
    _handoff_event(conn, current, actor.user_id, target, note)
    emit(
        conn,
        actor_id=actor.user_id,
        action=f"handoff.{target}",
        workspace_id=current["workspace_id"],
        project_id=current["project_id"],
        resource_type="handoff",
        resource_id=handoff_id,
        revision=row["current_revision"],
        recipients=recipients,
        preview=note or f"Handoff {target}",
    )
    return _handoff_payload(conn, row)


def get_handoff(conn, handoff_id: str) -> dict:
    row = one(conn, "SELECT * FROM app.handoffs WHERE id = %s", (handoff_id,), "Handoff not found")
    return _handoff_payload(conn, row)


def list_handoffs(conn, project_id: str) -> dict:
    require_project(conn, project_id)
    rows = conn.execute(
        "SELECT * FROM app.handoffs WHERE project_id = %s ORDER BY created_at, id",
        (project_id,),
    ).fetchall()
    return {"items": [_handoff_payload(conn, row) for row in rows]}


def _handoff_payload(conn, row) -> dict:
    revisions = conn.execute(
        "SELECT * FROM app.handoff_revisions WHERE handoff_id = %s ORDER BY revision",
        (row["id"],),
    ).fetchall()
    events = conn.execute(
        "SELECT * FROM app.handoff_events WHERE handoff_id = %s ORDER BY created_at",
        (row["id"],),
    ).fetchall()
    payload = dump(dict(row))
    payload["revisions"] = dump([dict(item) for item in revisions])
    payload["events"] = dump([dict(item) for item in events])
    return payload


def _handoff_event(conn, handoff, actor_id: str, event_type: str, note: str) -> None:
    conn.execute(
        """
        INSERT INTO app.handoff_events (
            handoff_id, workspace_id, project_id, event_type, actor_user_id, note
        ) VALUES (%s, %s, %s, %s, %s, %s)
        """,
        (handoff["id"], handoff["workspace_id"], handoff["project_id"], event_type, actor_id, note),
    )


def create_approval(conn, actor, project_id: str, body: dict) -> dict:
    project = require_project(conn, project_id, {"owner", "contributor", "reviewer"})
    ensure_open(project)
    reviewer = body["reviewer_id"]
    if reviewer == actor.user_id:
        raise AppError(403, "self_approval", "You cannot assign yourself to approve your own submission")
    _assert_project_member(conn, project_id, reviewer)
    role = conn.execute(
        """
        SELECT access_role FROM app.project_memberships
        WHERE project_id = %s AND user_id = %s AND status = 'active'
        """,
        (project_id, reviewer),
    ).fetchone()
    if not role or role["access_role"] == "partner":
        raise AppError(422, "validation", "The reviewer must be an internal project member")
    row = conn.execute(
        """
        INSERT INTO app.approval_requests (
            workspace_id, project_id, target_type, target_id, target_revision, author_id, reviewer_id
        ) VALUES (%s, %s, %s, %s, %s, %s, %s)
        RETURNING *
        """,
        (
            project["workspace_id"],
            project_id,
            body["target_type"],
            body["target_id"],
            body["target_revision"],
            actor.user_id,
            reviewer,
        ),
    ).fetchone()
    emit(
        conn,
        actor_id=actor.user_id,
        action="approval.requested",
        workspace_id=project["workspace_id"],
        project_id=project_id,
        resource_type="approval",
        resource_id=row["id"],
        revision=row["version"],
        recipients=[reviewer],
        preview=f"Review requested for {body['target_type']}",
    )
    return dump(dict(row))


TASK_DECISIONS = {
    "approved": "done",
    "changes_requested": "changes_requested",
    "rejected": "changes_requested",
}
REQUIREMENT_DECISIONS = {
    "approved": "accepted",
    "changes_requested": "changes_requested",
    "rejected": "changes_requested",
}


def _apply_target(conn, approval, decision: str) -> None:
    if decision not in TASK_DECISIONS:
        raise AppError(409, "invalid_state", "That decision is not allowed")
    conn.execute(
        "SELECT app.apply_approval_target(%s, %s)",
        (approval["id"], decision),
    )


def decide_approval(conn, actor, approval_id: str, expected: int, decision: str, comment: str, idempotency_key: str | None, target_revision: int) -> dict:
    body = {"decision": decision, "comment": comment, "expected_version": expected, "target_revision": target_revision}
    begun = idempotency_begin(conn, actor.user_id, f"approval:{approval_id}", idempotency_key, body)
    if begun and begun.get("replay"):
        return begun
    current = conn.execute(
        "SELECT * FROM app.approval_requests WHERE id = %s FOR UPDATE",
        (approval_id,),
    ).fetchone()
    if not current:
        raise AppError(404, "not_found", "Approval not found")
    if str(current["author_id"]) == actor.user_id:
        raise AppError(403, "self_approval", "The author cannot resolve this approval")
    if str(current["reviewer_id"]) != actor.user_id:
        raise AppError(403, "forbidden", "You are not the assigned reviewer")
    if current["state"] != "pending":
        raise AppError(409, "invalid_state", f"Approval is already {current['state']}")
    if int(current["target_revision"]) != int(target_revision):
        raise AppError(409, "conflict", "The approval targets a different revision", {"target_revision": current["target_revision"]})
    if int(current["version"]) != int(expected):
        raise AppError(409, "conflict", "The record changed. Reload and try again.", {"version": current["version"]})
    _apply_target(conn, current, decision)
    row = conn.execute(
        """
        UPDATE app.approval_requests
        SET state = %s, version = version + 1, updated_at = now()
        WHERE id = %s AND state = 'pending' AND version = %s
        RETURNING *
        """,
        (decision, approval_id, expected),
    ).fetchone()
    if not row:
        raise AppError(409, "conflict", "The approval was resolved by someone else")
    conn.execute(
        """
        INSERT INTO app.approval_decisions (
            approval_id, actor_user_id, decision, comment, target_revision
        ) VALUES (%s, %s, %s, %s, %s)
        """,
        (approval_id, actor.user_id, decision, comment or "", target_revision),
    )
    payload = dump(dict(row))
    idempotency_finish(conn, actor.user_id, begun, 200, payload)
    emit(
        conn,
        actor_id=actor.user_id,
        action=f"approval.{decision}",
        workspace_id=current["workspace_id"],
        project_id=current["project_id"],
        resource_type="approval",
        resource_id=approval_id,
        revision=row["version"],
        recipients=[str(current["author_id"])],
        preview=comment or f"Approval {decision}",
    )
    if begun:
        return {"replay": False, "status": 200, "body": payload}
    return {"replay": False, "status": 200, "body": payload}


def list_approvals(conn, workspace_id: str) -> dict:
    require_workspace(conn, workspace_id)
    rows = conn.execute(
        """
        SELECT * FROM app.approval_requests
        WHERE workspace_id = %s
        ORDER BY created_at, id
        """,
        (workspace_id,),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def get_approval(conn, approval_id: str) -> dict:
    row = one(conn, "SELECT * FROM app.approval_requests WHERE id = %s", (approval_id,), "Approval not found")
    decisions = conn.execute(
        "SELECT * FROM app.approval_decisions WHERE approval_id = %s ORDER BY created_at",
        (approval_id,),
    ).fetchall()
    payload = dump(dict(row))
    payload["decisions"] = dump([dict(item) for item in decisions])
    return payload


def create_comment(conn, actor, project_id: str, body: dict) -> dict:
    project = require_project(conn, project_id)
    if project["access_role"] == "partner" and body.get("visibility", "shared") != "shared":
        raise AppError(403, "forbidden", "Partners can only post shared comments")
    visibility = "shared" if project["access_role"] == "partner" else body.get("visibility", "internal")
    mentions = body.get("mentions") or []
    for user_id in mentions:
        _assert_project_member(conn, project_id, user_id)
    row = conn.execute(
        """
        INSERT INTO app.comments (
            workspace_id, project_id, target_type, target_id, visibility, body, author_id
        ) VALUES (%s, %s, %s, %s, %s, %s, %s)
        RETURNING *
        """,
        (
            project["workspace_id"],
            project_id,
            body["target_type"],
            body["target_id"],
            visibility,
            body["body"].strip(),
            actor.user_id,
        ),
    ).fetchone()
    emit(
        conn,
        actor_id=actor.user_id,
        action="comment.created",
        workspace_id=project["workspace_id"],
        project_id=project_id,
        resource_type="comment",
        resource_id=row["id"],
        revision=None,
        recipients=mentions,
        preview=row["body"][:180],
    )
    return dump(dict(row))


def list_comments(conn, project_id: str, target_type: str | None, target_id: str | None) -> dict:
    require_project(conn, project_id)
    rows = conn.execute(
        """
        SELECT * FROM app.comments
        WHERE project_id = %s
          AND (%s::text IS NULL OR target_type = %s)
          AND (%s::uuid IS NULL OR target_id = %s)
        ORDER BY created_at, id
        """,
        (project_id, target_type, target_type, target_id, target_id),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def list_notifications(conn, workspace_id: str, limit: int | None) -> dict:
    require_workspace(conn, workspace_id)
    rows = conn.execute(
        """
        SELECT * FROM app.notifications
        WHERE workspace_id = %s AND recipient_id = app.current_user_id()
        ORDER BY created_at DESC, id DESC
        LIMIT %s
        """,
        (workspace_id, limit_of(limit)),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def mark_notification_read(conn, notification_id: str) -> dict:
    row = conn.execute(
        """
        UPDATE app.notifications
        SET read_at = COALESCE(read_at, now())
        WHERE id = %s AND recipient_id = app.current_user_id()
        RETURNING *
        """,
        (notification_id,),
    ).fetchone()
    if not row:
        raise AppError(404, "not_found", "Notification not found")
    return dump(dict(row))


def mark_all_read(conn, workspace_id: str) -> dict:
    require_workspace(conn, workspace_id)
    conn.execute(
        """
        UPDATE app.notifications
        SET read_at = COALESCE(read_at, now())
        WHERE workspace_id = %s AND recipient_id = app.current_user_id() AND read_at IS NULL
        """,
        (workspace_id,),
    )
    return {"ok": True}


def list_audit(conn, workspace_id: str, project_id: str | None, limit: int | None) -> dict:
    if project_id:
        require_project(conn, project_id, {"owner", "contributor", "reviewer", "viewer"})
    else:
        require_workspace(conn, workspace_id, {"owner", "administrator"})
    rows = conn.execute(
        """
        SELECT * FROM app.audit_events
        WHERE workspace_id = %s
          AND (%s::uuid IS NULL OR project_id = %s)
        ORDER BY created_at, id
        LIMIT %s
        """,
        (workspace_id, project_id, project_id, limit_of(limit)),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def my_work(conn, workspace_id: str) -> dict:
    require_workspace(conn, workspace_id)
    tasks = conn.execute(
        """
        SELECT id, title, state, project_id, due_on
        FROM app.tasks
        WHERE workspace_id = %s
          AND assignee_id = app.current_user_id()
          AND state NOT IN ('done', 'cancelled')
        ORDER BY due_on NULLS LAST, created_at
        """,
        (workspace_id,),
    ).fetchall()
    approvals = conn.execute(
        """
        SELECT id, target_type, target_id, state, project_id
        FROM app.approval_requests
        WHERE workspace_id = %s
          AND reviewer_id = app.current_user_id()
          AND state = 'pending'
        ORDER BY created_at
        """,
        (workspace_id,),
    ).fetchall()
    handoffs = conn.execute(
        """
        SELECT id, state, project_id, due_on
        FROM app.handoffs
        WHERE workspace_id = %s
          AND (recipient_id = app.current_user_id() OR accountable_id = app.current_user_id())
          AND state IN ('sent', 'acknowledged', 'needs_information')
        ORDER BY due_on NULLS LAST, created_at
        """,
        (workspace_id,),
    ).fetchall()
    questions = conn.execute(
        """
        SELECT id, question, state
        FROM app.data_questions
        WHERE workspace_id = %s
          AND assignee_id = app.current_user_id()
          AND state = 'open'
        ORDER BY created_at
        """,
        (workspace_id,),
    ).fetchall()
    return {
        "tasks": dump([dict(row) for row in tasks]),
        "approvals": dump([dict(row) for row in approvals]),
        "handoffs": dump([dict(row) for row in handoffs]),
        "questions": dump([dict(row) for row in questions]),
    }

