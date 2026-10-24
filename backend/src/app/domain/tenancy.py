import hashlib
import secrets
from datetime import datetime, timedelta, timezone
from uuid import uuid4

from app.core.errors import AppError
from app.core.serialize import dump
from app.domain.common import emit, ensure_open, limit_of, one, require_project, require_workspace, update_version

FUNCTIONAL_ROLES = (
    ("management", "Management"),
    ("ba_junior", "Junior BA"),
    ("ba_senior", "Senior BA"),
    ("transformation_lead", "Transformation lead"),
    ("pm", "Project manager"),
    ("po", "Product owner"),
    ("engineer", "Engineer"),
    ("dba", "DBA"),
    ("partner_representative", "Partner representative"),
)

INTERNAL_PROJECT_ROLES = {"owner", "contributor", "reviewer", "viewer"}


def _public_project(row, internal: dict | None) -> dict:
    payload = dump(dict(row))
    payload.pop("access_role", None)
    if internal is None:
        return payload
    payload["internal_notes"] = internal["internal_notes"]
    payload["internal_cost"] = dump(internal["internal_cost"])
    return payload


def create_workspace(conn, actor, name: str, timezone_name: str) -> dict:
    cleaned = name.strip()
    if not cleaned:
        raise AppError(422, "validation", "Workspace name is required", {"name": "required"})
    workspace_id = uuid4()
    # The id is chosen here so membership can be inserted before any RETURNING
    # read. A new workspace is not visible until the owner membership exists.
    conn.execute(
        """
        INSERT INTO app.workspaces (id, name, timezone)
        VALUES (%s, %s, %s)
        """,
        (workspace_id, cleaned, timezone_name or "UTC"),
    )
    conn.execute(
        """
        INSERT INTO app.workspace_memberships (workspace_id, user_id, access_role, status)
        VALUES (%s, %s, 'owner', 'active')
        """,
        (workspace_id, actor.user_id),
    )
    for code, label in FUNCTIONAL_ROLES:
        conn.execute(
            "INSERT INTO app.functional_roles (workspace_id, code, name) VALUES (%s, %s, %s)",
            (workspace_id, code, label),
        )
    row = one(conn, "SELECT * FROM app.workspaces WHERE id = %s", (workspace_id,), "Workspace not found")
    emit(
        conn,
        actor_id=actor.user_id,
        action="workspace.created",
        workspace_id=row["id"],
        project_id=None,
        resource_type="workspace",
        resource_id=row["id"],
        revision=row["version"],
        recipients=[],
        preview=f"Workspace {cleaned} created",
    )
    return dump(dict(row))


def list_workspaces(conn, limit: int | None) -> dict:
    rows = conn.execute(
        """
        SELECT w.*, m.access_role
        FROM app.workspaces w
        JOIN app.workspace_memberships m
          ON m.workspace_id = w.id
         AND m.user_id = app.current_user_id()
         AND m.status = 'active'
        ORDER BY w.created_at, w.id
        LIMIT %s
        """,
        (limit_of(limit),),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def get_workspace(conn, workspace_id: str) -> dict:
    return dump(dict(require_workspace(conn, workspace_id)))


def update_workspace(conn, actor, workspace_id: str, expected: int, fields: dict) -> dict:
    require_workspace(conn, workspace_id, {"owner", "administrator"})
    assignments = {key: value for key, value in fields.items() if value is not None and key != "expected_version"}
    row = update_version(conn, "workspaces", workspace_id, expected, assignments)
    emit(
        conn,
        actor_id=actor.user_id,
        action="workspace.updated",
        workspace_id=workspace_id,
        project_id=None,
        resource_type="workspace",
        resource_id=workspace_id,
        revision=row["version"],
        recipients=[],
        preview="Workspace settings changed",
    )
    return dump(dict(row))


def list_members(conn, workspace_id: str, limit: int | None) -> dict:
    require_workspace(conn, workspace_id)
    rows = conn.execute(
        """
        SELECT m.workspace_id, m.user_id, m.access_role, m.status, p.display_name
        FROM app.workspace_memberships m
        JOIN app.profiles p ON p.id = m.user_id
        WHERE m.workspace_id = %s
        ORDER BY p.display_name, m.user_id
        LIMIT %s
        """,
        (workspace_id, limit_of(limit)),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def update_member(conn, actor, workspace_id: str, user_id: str, access_role: str | None, status: str | None) -> dict:
    current = require_workspace(conn, workspace_id, {"owner", "administrator"})
    if current["access_role"] != "owner" and access_role == "owner":
        raise AppError(403, "forbidden", "Only an owner can assign ownership")
    if access_role == "owner" and user_id == actor.user_id and current["access_role"] != "owner":
        raise AppError(403, "forbidden", "You cannot grant yourself ownership")
    assignments = []
    values: list = []
    if access_role:
        assignments.append("access_role = %s")
        values.append(access_role)
    if status:
        assignments.append("status = %s")
        values.append(status)
    if not assignments:
        raise AppError(422, "validation", "Nothing to change")
    assignments.append("updated_at = now()")
    row = conn.execute(
        f"""
        UPDATE app.workspace_memberships
        SET {", ".join(assignments)}
        WHERE workspace_id = %s AND user_id = %s
        RETURNING *
        """,
        (*values, workspace_id, user_id),
    ).fetchone()
    if not row:
        raise AppError(404, "not_found", "Member not found")
    emit(
        conn,
        actor_id=actor.user_id,
        action="member.updated",
        workspace_id=workspace_id,
        project_id=None,
        resource_type="workspace_membership",
        resource_id=user_id,
        revision=None,
        recipients=[user_id],
        preview="Workspace membership changed",
    )
    if status == "removed":
        emit(
            conn,
            actor_id=actor.user_id,
            action="access.revoked",
            workspace_id=workspace_id,
            project_id=None,
            resource_type="workspace_membership",
            resource_id=user_id,
            revision=None,
            recipients=[user_id],
            preview="Workspace access removed",
            extra={"scope": "workspace"},
        )
    return dump(dict(row))


def add_department(conn, actor, workspace_id: str, name: str) -> dict:
    require_workspace(conn, workspace_id, {"owner", "administrator"})
    cleaned = name.strip()
    if not cleaned:
        raise AppError(422, "validation", "Department name is required")
    row = conn.execute(
        """
        INSERT INTO app.departments (workspace_id, name)
        VALUES (%s, %s)
        RETURNING *
        """,
        (workspace_id, cleaned),
    ).fetchone()
    emit(
        conn,
        actor_id=actor.user_id,
        action="department.created",
        workspace_id=workspace_id,
        project_id=None,
        resource_type="department",
        resource_id=row["id"],
        revision=None,
        recipients=[],
        preview=f"Department {cleaned} added",
    )
    return dump(dict(row))


def list_departments(conn, workspace_id: str) -> dict:
    require_workspace(conn, workspace_id)
    rows = conn.execute(
        "SELECT * FROM app.departments WHERE workspace_id = %s ORDER BY name",
        (workspace_id,),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def assign_function(conn, actor, workspace_id: str, user_id: str, code: str) -> dict:
    require_workspace(conn, workspace_id, {"owner", "administrator"})
    role = conn.execute(
        "SELECT id FROM app.functional_roles WHERE workspace_id = %s AND code = %s",
        (workspace_id, code),
    ).fetchone()
    if not role:
        raise AppError(422, "validation", "Unknown functional role", {"code": code})
    member = conn.execute(
        """
        SELECT 1 FROM app.workspace_memberships
        WHERE workspace_id = %s AND user_id = %s AND status = 'active'
        """,
        (workspace_id, user_id),
    ).fetchone()
    if not member:
        raise AppError(422, "validation", "That person is not an active workspace member")
    conn.execute(
        """
        INSERT INTO app.member_functional_roles (workspace_id, user_id, role_id)
        VALUES (%s, %s, %s)
        ON CONFLICT DO NOTHING
        """,
        (workspace_id, user_id, role["id"]),
    )
    return {"user_id": user_id, "code": code}


def create_invitation(conn, actor, workspace_id: str, body: dict) -> dict:
    require_workspace(conn, workspace_id, {"owner", "administrator"})
    email = body["email"].strip().lower()
    project_id = body.get("project_id")
    project_role = body.get("project_role")
    if project_id:
        project = require_project(conn, project_id, {"owner", "contributor", "reviewer", "viewer"})
        if str(project["workspace_id"]) != str(workspace_id):
            raise AppError(422, "validation", "Project is not in this workspace")
        if project["access_role"] != "owner":
            raise AppError(403, "forbidden", "Only a project owner can invite someone into it")
    raw = secrets.token_urlsafe(32)
    digest = hashlib.sha256(raw.encode()).hexdigest()
    conn.execute(
        """
        UPDATE app.invitations
        SET state = 'revoked', revoked_at = now()
        WHERE workspace_id = %s
          AND lower(email) = %s
          AND state = 'pending'
          AND project_id IS NOT DISTINCT FROM %s
        """,
        (workspace_id, email, project_id),
    )
    row = conn.execute(
        """
        INSERT INTO app.invitations (
            workspace_id, project_id, partner_id, email, token_hash,
            access_role, project_role, invited_by, expires_at
        ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)
        RETURNING id, workspace_id, project_id, email, access_role, project_role, state, expires_at
        """,
        (
            workspace_id,
            project_id,
            body.get("partner_id"),
            email,
            digest,
            body.get("access_role") or "guest",
            project_role,
            actor.user_id,
            datetime.now(timezone.utc) + timedelta(days=7),
        ),
    ).fetchone()
    emit(
        conn,
        actor_id=actor.user_id,
        action="invitation.created",
        workspace_id=workspace_id,
        project_id=project_id,
        resource_type="invitation",
        resource_id=row["id"],
        revision=None,
        recipients=[],
        preview="Invitation created",
    )
    payload = dump(dict(row))
    payload["token"] = raw
    return payload


def preview_invitation(token: str) -> dict:
    from app.core.database import admin_connection

    digest = hashlib.sha256(token.encode()).hexdigest()
    with admin_connection() as conn:
        row = conn.execute("SELECT * FROM app.invitation_preview(%s)", (digest,)).fetchone()
    if not row:
        raise AppError(404, "not_found", "Invitation not found")
    return dump(dict(row))


def accept_invitation(conn, actor, token: str) -> dict:
    digest = hashlib.sha256(token.encode()).hexdigest()
    row = conn.execute(
        "SELECT * FROM app.invitations WHERE token_hash = %s",
        (digest,),
    ).fetchone()
    if not row:
        raise AppError(404, "not_found", "Invitation not found")
    if row["email"].lower() != actor.email.lower():
        raise AppError(403, "identity_mismatch", "This invitation is for a different account")
    if row["state"] == "accepted" and str(row["consumed_by"]) == actor.user_id:
        return _acceptance_payload(conn, row)
    if row["state"] != "pending":
        raise AppError(409, "invitation_closed", f"Invitation is {row['state']}")
    if row["expires_at"] <= datetime.now(timezone.utc):
        conn.execute(
            "UPDATE app.invitations SET state = 'expired' WHERE id = %s AND state = 'pending'",
            (row["id"],),
        )
        raise AppError(409, "invitation_expired", "Invitation expired")
    updated = conn.execute(
        """
        UPDATE app.invitations
        SET state = 'accepted', consumed_at = now(), consumed_by = %s
        WHERE id = %s AND state = 'pending'
        RETURNING *
        """,
        (actor.user_id, row["id"]),
    ).fetchone()
    if not updated:
        again = conn.execute("SELECT * FROM app.invitations WHERE id = %s", (row["id"],)).fetchone()
        if again and again["state"] == "accepted" and str(again["consumed_by"]) == actor.user_id:
            return _acceptance_payload(conn, again)
        raise AppError(409, "invitation_closed", "Invitation is no longer pending")
    conn.execute(
        """
        INSERT INTO app.workspace_memberships (workspace_id, user_id, access_role, status)
        VALUES (%s, %s, %s, 'active')
        ON CONFLICT (workspace_id, user_id) DO NOTHING
        """,
        (updated["workspace_id"], actor.user_id, updated["access_role"]),
    )
    if updated["project_id"] and updated["project_role"]:
        conn.execute(
            """
            INSERT INTO app.project_memberships (project_id, user_id, workspace_id, access_role, status)
            VALUES (%s, %s, %s, %s, 'active')
            ON CONFLICT (project_id, user_id) DO NOTHING
            """,
            (updated["project_id"], actor.user_id, updated["workspace_id"], updated["project_role"]),
        )
    if updated["partner_id"]:
        conn.execute("SELECT app.link_partner_contact(%s)", (updated["partner_id"],))
    emit(
        conn,
        actor_id=actor.user_id,
        action="invitation.accepted",
        workspace_id=updated["workspace_id"],
        project_id=updated["project_id"],
        resource_type="invitation",
        resource_id=updated["id"],
        revision=None,
        recipients=[str(updated["invited_by"])],
        preview="Invitation accepted",
    )
    return _acceptance_payload(conn, updated)


def _acceptance_payload(conn, invitation) -> dict:
    membership = conn.execute(
        """
        SELECT workspace_id, user_id, access_role, status
        FROM app.workspace_memberships
        WHERE workspace_id = %s AND user_id = app.current_user_id()
        """,
        (invitation["workspace_id"],),
    ).fetchone()
    return {"invitation": dump(dict(invitation)), "membership": dump(dict(membership)) if membership else None}


def revoke_invitation(conn, actor, workspace_id: str, invitation_id: str) -> dict:
    require_workspace(conn, workspace_id, {"owner", "administrator"})
    row = conn.execute(
        """
        UPDATE app.invitations
        SET state = 'revoked', revoked_at = now()
        WHERE id = %s AND workspace_id = %s AND state = 'pending'
        RETURNING *
        """,
        (invitation_id, workspace_id),
    ).fetchone()
    if not row:
        raise AppError(404, "not_found", "Pending invitation not found")
    emit(
        conn,
        actor_id=actor.user_id,
        action="invitation.revoked",
        workspace_id=workspace_id,
        project_id=row["project_id"],
        resource_type="invitation",
        resource_id=row["id"],
        revision=None,
        recipients=[],
        preview="Invitation revoked",
    )
    return dump(dict(row))


def create_project(conn, actor, workspace_id: str, body: dict) -> dict:
    require_workspace(conn, workspace_id, {"owner", "administrator", "member"})
    project_type = body["project_type"]
    partner_id = body.get("partner_id")
    if project_type == "partner" and not partner_id:
        raise AppError(422, "validation", "A partner project needs a partner organization")
    if partner_id:
        one(
            conn,
            "SELECT id FROM app.partner_organizations WHERE id = %s AND workspace_id = %s",
            (partner_id, workspace_id),
            "Partner not found",
        )
    if body.get("parent_id"):
        parent = require_project(conn, body["parent_id"])
        if str(parent["workspace_id"]) != str(workspace_id):
            raise AppError(422, "validation", "Parent project is in another workspace")
    project_id = uuid4()
    conn.execute(
        """
        INSERT INTO app.projects (
            id, workspace_id, parent_id, partner_id, name, project_type,
            objective, constraints, scope, owner_id, starts_on, ends_on
        ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
        """,
        (
            project_id,
            workspace_id,
            body.get("parent_id"),
            partner_id,
            body["name"].strip(),
            project_type,
            body.get("objective") or "",
            body.get("constraints") or "",
            body.get("scope") or "",
            actor.user_id,
            body.get("starts_on"),
            body.get("ends_on"),
        ),
    )
    conn.execute(
        """
        INSERT INTO app.project_memberships (project_id, user_id, workspace_id, access_role, status)
        VALUES (%s, %s, %s, 'owner', 'active')
        """,
        (project_id, actor.user_id, workspace_id),
    )
    conn.execute(
        """
        INSERT INTO app.project_internal (project_id, workspace_id, internal_notes, internal_cost)
        VALUES (%s, %s, %s, %s)
        """,
        (project_id, workspace_id, body.get("internal_notes") or "", body.get("internal_cost")),
    )
    row = {"id": project_id, "name": body["name"].strip(), "version": 1, "workspace_id": workspace_id}
    emit(
        conn,
        actor_id=actor.user_id,
        action="project.created",
        workspace_id=workspace_id,
        project_id=row["id"],
        resource_type="project",
        resource_id=row["id"],
        revision=row["version"],
        recipients=[],
        preview=f"Project {row['name']} created",
    )
    return get_project(conn, str(row["id"]))


def list_projects(conn, workspace_id: str, limit: int | None, partner_only: bool = False) -> dict:
    require_workspace(conn, workspace_id)
    role_filter = "AND m.access_role = 'partner'" if partner_only else ""
    rows = conn.execute(
        f"""
        SELECT p.*, m.access_role
        FROM app.projects p
        JOIN app.project_memberships m
          ON m.project_id = p.id
         AND m.user_id = app.current_user_id()
         AND m.status = 'active'
        WHERE p.workspace_id = %s
        {role_filter}
        ORDER BY p.created_at, p.id
        LIMIT %s
        """,
        (workspace_id, limit_of(limit)),
    ).fetchall()
    items = []
    for row in rows:
        internal = None
        if row["access_role"] in INTERNAL_PROJECT_ROLES:
            internal = conn.execute(
                "SELECT internal_notes, internal_cost FROM app.project_internal WHERE project_id = %s",
                (row["id"],),
            ).fetchone()
        items.append(_public_project(row, internal))
    return {"items": items}


def get_project(conn, project_id: str) -> dict:
    row = require_project(conn, project_id)
    internal = None
    if row["access_role"] in INTERNAL_PROJECT_ROLES:
        internal = conn.execute(
            "SELECT internal_notes, internal_cost FROM app.project_internal WHERE project_id = %s",
            (project_id,),
        ).fetchone()
    return _public_project(row, internal)


def update_project(conn, actor, project_id: str, expected: int, fields: dict) -> dict:
    project = require_project(conn, project_id, {"owner", "contributor"})
    ensure_open(project)
    if "state" in fields and fields["state"] is not None and project["access_role"] != "owner":
        raise AppError(403, "forbidden", "Only the project owner can change lifecycle")
    assignments = {key: value for key, value in fields.items() if key != "expected_version" and value is not None}
    internal_notes = assignments.pop("internal_notes", None)
    internal_cost = assignments.pop("internal_cost", None)
    if project["access_role"] == "partner":
        raise AppError(403, "forbidden", "Partners cannot edit the project record")
    row = update_version(conn, "projects", project_id, expected, assignments) if assignments else project
    if internal_notes is not None or internal_cost is not None:
        if project["access_role"] not in {"owner", "contributor"}:
            raise AppError(403, "forbidden", "You cannot edit internal notes")
        conn.execute(
            """
            UPDATE app.project_internal
            SET internal_notes = COALESCE(%s, internal_notes),
                internal_cost = COALESCE(%s, internal_cost)
            WHERE project_id = %s
            """,
            (internal_notes, internal_cost, project_id),
        )
    emit(
        conn,
        actor_id=actor.user_id,
        action="project.updated",
        workspace_id=project["workspace_id"],
        project_id=project_id,
        resource_type="project",
        resource_id=project_id,
        revision=row["version"],
        recipients=[],
        preview="Project updated",
    )
    return get_project(conn, project_id)


def set_project_state(conn, actor, project_id: str, expected: int, state: str) -> dict:
    project = require_project(conn, project_id, {"owner"})
    if state == "archived" and project["state"] != "active":
        raise AppError(409, "invalid_state", "Only an active project can be archived")
    if state == "active" and project["state"] != "archived":
        raise AppError(409, "invalid_state", "Only an archived project can be restored")
    update_version(conn, "projects", project_id, expected, {"state": state})
    emit(
        conn,
        actor_id=actor.user_id,
        action=f"project.{state}",
        workspace_id=project["workspace_id"],
        project_id=project_id,
        resource_type="project",
        resource_id=project_id,
        revision=expected + 1,
        recipients=[],
        preview=f"Project {state}",
    )
    return get_project(conn, project_id)


def add_project_member(conn, actor, project_id: str, user_id: str, access_role: str) -> dict:
    project = require_project(conn, project_id, {"owner"})
    ensure_open(project)
    member = conn.execute(
        """
        SELECT 1 FROM app.workspace_memberships
        WHERE workspace_id = %s AND user_id = %s AND status = 'active'
        """,
        (project["workspace_id"], user_id),
    ).fetchone()
    if not member:
        raise AppError(422, "validation", "That person is not an active workspace member")
    row = conn.execute(
        """
        INSERT INTO app.project_memberships (project_id, user_id, workspace_id, access_role, status)
        VALUES (%s, %s, %s, %s, 'active')
        ON CONFLICT (project_id, user_id) DO UPDATE
        SET access_role = EXCLUDED.access_role, status = 'active', updated_at = now()
        RETURNING *
        """,
        (project_id, user_id, project["workspace_id"], access_role),
    ).fetchone()
    emit(
        conn,
        actor_id=actor.user_id,
        action="project.member_added",
        workspace_id=project["workspace_id"],
        project_id=project_id,
        resource_type="project_membership",
        resource_id=user_id,
        revision=None,
        recipients=[user_id],
        preview="Project access changed",
    )
    return dump(dict(row))


def list_project_members(conn, project_id: str) -> dict:
    require_project(conn, project_id)
    rows = conn.execute(
        """
        SELECT m.project_id, m.user_id, m.access_role, m.status, p.display_name
        FROM app.project_memberships m
        JOIN app.profiles p ON p.id = m.user_id
        WHERE m.project_id = %s AND m.status = 'active'
        ORDER BY p.display_name
        """,
        (project_id,),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def create_partner(conn, actor, workspace_id: str, name: str) -> dict:
    require_workspace(conn, workspace_id, {"owner", "administrator", "member"})
    row = conn.execute(
        """
        INSERT INTO app.partner_organizations (workspace_id, name)
        VALUES (%s, %s)
        RETURNING *
        """,
        (workspace_id, name.strip()),
    ).fetchone()
    emit(
        conn,
        actor_id=actor.user_id,
        action="partner.created",
        workspace_id=workspace_id,
        project_id=None,
        resource_type="partner",
        resource_id=row["id"],
        revision=None,
        recipients=[],
        preview=f"Partner {row['name']} recorded",
    )
    return dump(dict(row))


def list_partners(conn, workspace_id: str) -> dict:
    require_workspace(conn, workspace_id)
    rows = conn.execute(
        """
        SELECT * FROM app.partner_organizations
        WHERE workspace_id = %s
        ORDER BY name
        """,
        (workspace_id,),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}


def add_partner_contact(conn, actor, partner_id: str, name: str, email: str) -> dict:
    partner = one(conn, "SELECT * FROM app.partner_organizations WHERE id = %s", (partner_id,), "Partner not found")
    require_workspace(conn, str(partner["workspace_id"]), {"owner", "administrator", "member"})
    row = conn.execute(
        """
        INSERT INTO app.partner_contacts (partner_id, workspace_id, name, email)
        VALUES (%s, %s, %s, %s)
        RETURNING *
        """,
        (partner_id, partner["workspace_id"], name.strip(), email.strip().lower()),
    ).fetchone()
    emit(
        conn,
        actor_id=actor.user_id,
        action="partner.contact_added",
        workspace_id=partner["workspace_id"],
        project_id=None,
        resource_type="partner_contact",
        resource_id=row["id"],
        revision=None,
        recipients=[],
        preview="Partner contact added",
    )
    return dump(dict(row))


def suspend_partner(conn, actor, partner_id: str) -> dict:
    partner = one(conn, "SELECT * FROM app.partner_organizations WHERE id = %s", (partner_id,), "Partner not found")
    revoked = conn.execute(
        """
        SELECT pm.user_id, pm.project_id
        FROM app.project_memberships pm
        JOIN app.partner_contacts c ON c.user_id = pm.user_id
        WHERE c.partner_id = %s AND pm.access_role = 'partner' AND pm.status = 'active'
        """,
        (partner_id,),
    ).fetchall()
    conn.execute("SELECT app.suspend_partner(%s)", (partner_id,))
    for pair in revoked:
        emit(
            conn,
            actor_id=actor.user_id,
            action="access.revoked",
            workspace_id=partner["workspace_id"],
            project_id=pair["project_id"],
            resource_type="project_membership",
            resource_id=pair["user_id"],
            revision=None,
            recipients=[str(pair["user_id"])],
            preview="Partner access suspended",
            extra={"scope": "project"},
        )
    emit(
        conn,
        actor_id=actor.user_id,
        action="partner.suspended",
        workspace_id=partner["workspace_id"],
        project_id=None,
        resource_type="partner",
        resource_id=partner_id,
        revision=None,
        recipients=[],
        preview="Partner access suspended",
    )
    refreshed = one(conn, "SELECT * FROM app.partner_organizations WHERE id = %s", (partner_id,), "Partner not found")
    return dump(dict(refreshed))


def get_me(conn, actor) -> dict:
    profile = one(conn, "SELECT * FROM app.profiles WHERE id = %s", (actor.user_id,), "Profile not found")
    memberships = conn.execute(
        """
        SELECT m.workspace_id, m.access_role, m.status, w.name
        FROM app.workspace_memberships m
        JOIN app.workspaces w ON w.id = m.workspace_id
        WHERE m.user_id = %s AND m.status = 'active'
        ORDER BY w.name
        """,
        (actor.user_id,),
    ).fetchall()
    return {"profile": dump(dict(profile)), "memberships": dump([dict(row) for row in memberships])}


def update_me(conn, actor, display_name: str | None, timezone_name: str | None) -> dict:
    if display_name is None and timezone_name is None:
        raise AppError(422, "validation", "Nothing to change")
    row = conn.execute(
        """
        UPDATE app.profiles
        SET display_name = COALESCE(%s, display_name),
            timezone = COALESCE(%s, timezone),
            updated_at = now()
        WHERE id = %s
        RETURNING *
        """,
        (display_name.strip() if display_name else None, timezone_name, actor.user_id),
    ).fetchone()
    return dump(dict(row))
