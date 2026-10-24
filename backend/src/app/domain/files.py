import hashlib
from pathlib import Path

from app.core.config import settings
from app.core.errors import AppError
from app.core.serialize import dump
from app.domain.common import emit, ensure_open, one, require_project

MAX_BYTES = 10_000_000
ALLOWED_MIME = {
    "text/plain",
    "text/csv",
    "application/pdf",
    "image/png",
    "image/jpeg",
}


def _root() -> Path:
    path = Path(settings.storage_dir)
    path.mkdir(parents=True, exist_ok=True)
    return path


def _path_for(storage_key: str) -> Path:
    if "/" in storage_key or "\\" in storage_key or storage_key.startswith("."):
        raise AppError(422, "validation", "Invalid storage key")
    root = _root().resolve()
    target = (root / storage_key).resolve()
    if root not in target.parents and target != root:
        raise AppError(422, "validation", "Invalid storage key")
    return target


def initiate(conn, actor, project_id: str, body: dict) -> dict:
    project = require_project(conn, project_id)
    ensure_open(project)
    visibility = body.get("visibility") or "internal"
    if project["access_role"] == "partner":
        visibility = "shared"
    mime = body.get("mime_type") or "application/octet-stream"
    if mime not in ALLOWED_MIME:
        raise AppError(422, "validation", "This file type is not allowed", {"mime_type": mime})
    document = conn.execute(
        """
        INSERT INTO app.documents (workspace_id, project_id, title, visibility, created_by)
        VALUES (%s, %s, %s, %s, %s)
        RETURNING *
        """,
        (project["workspace_id"], project_id, body["title"].strip(), visibility, actor.user_id),
    ).fetchone()
    version = conn.execute(
        """
        INSERT INTO app.document_versions (
            document_id, workspace_id, project_id, revision, storage_key, filename, mime_type, actor_user_id
        ) VALUES (%s, %s, %s, 1, gen_random_uuid()::text, %s, %s, %s)
        RETURNING *
        """,
        (
            document["id"],
            project["workspace_id"],
            project_id,
            body["filename"].strip(),
            mime,
            actor.user_id,
        ),
    ).fetchone()
    payload = dump(dict(document))
    payload["version"] = dump(dict(version))
    return payload


def store_bytes(conn, actor, version_id: str, data: bytes) -> dict:
    if len(data) > MAX_BYTES:
        raise AppError(422, "validation", "File is too large")
    if not data:
        raise AppError(422, "validation", "File is empty")
    version = one(
        conn,
        "SELECT * FROM app.document_versions WHERE id = %s",
        (version_id,),
        "Document version not found",
    )
    if version["state"] == "ready":
        raise AppError(409, "invalid_state", "This version is already finalized")
    digest = hashlib.sha256(data).hexdigest()
    _path_for(version["storage_key"]).write_bytes(data)
    row = conn.execute(
        """
        UPDATE app.document_versions
        SET byte_size = %s, checksum = %s
        WHERE id = %s
        RETURNING *
        """,
        (len(data), digest, version_id),
    ).fetchone()
    _ = actor
    return dump(dict(row))


def finalize(conn, actor, version_id: str) -> dict:
    version = one(
        conn,
        "SELECT * FROM app.document_versions WHERE id = %s",
        (version_id,),
        "Document version not found",
    )
    path = _path_for(version["storage_key"])
    if not path.exists() or version["byte_size"] <= 0 or not version["checksum"]:
        conn.execute(
            "UPDATE app.document_versions SET state = 'failed' WHERE id = %s",
            (version_id,),
        )
        raise AppError(422, "upload_incomplete", "Upload the file before finalizing")
    if path.stat().st_size != version["byte_size"]:
        raise AppError(422, "upload_incomplete", "Stored file does not match the upload")
    row = conn.execute(
        """
        UPDATE app.document_versions
        SET state = 'ready'
        WHERE id = %s AND state = 'pending'
        RETURNING *
        """,
        (version_id,),
    ).fetchone()
    if not row:
        raise AppError(409, "invalid_state", "This version cannot be finalized")
    emit(
        conn,
        actor_id=actor.user_id,
        action="document.ready",
        workspace_id=version["workspace_id"],
        project_id=version["project_id"],
        resource_type="document",
        resource_id=version["document_id"],
        revision=version["revision"],
        recipients=[],
        preview=version["filename"],
    )
    return dump(dict(row))


def open_bytes(conn, version_id: str) -> tuple[dict, bytes]:
    version = one(
        conn,
        """
        SELECT v.*, d.visibility
        FROM app.document_versions v
        JOIN app.documents d ON d.id = v.document_id
        WHERE v.id = %s AND v.state = 'ready'
        """,
        (version_id,),
        "Document not found",
    )
    path = _path_for(version["storage_key"])
    if not path.exists():
        raise AppError(404, "not_found", "File bytes are missing")
    return dump(dict(version)), path.read_bytes()


def list_documents(conn, project_id: str) -> dict:
    require_project(conn, project_id)
    rows = conn.execute(
        """
        SELECT d.*, v.id AS latest_version_id, v.state AS latest_state, v.filename
        FROM app.documents d
        LEFT JOIN LATERAL (
            SELECT id, state, filename
            FROM app.document_versions
            WHERE document_id = d.id
            ORDER BY revision DESC
            LIMIT 1
        ) v ON true
        WHERE d.project_id = %s
        ORDER BY d.created_at, d.id
        """,
        (project_id,),
    ).fetchall()
    return {"items": dump([dict(row) for row in rows])}
