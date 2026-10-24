import hashlib
import hmac
import json
from datetime import datetime, timezone

import httpx

from app.core.config import settings
from app.core.database import admin_connection
from app.core.errors import AppError


def _sign(secret: str, timestamp: str, body: str) -> str:
    message = f"{timestamp}.{body}".encode()
    return hmac.new(secret.encode(), message, hashlib.sha256).hexdigest()


def _post_hint(path: str, payload: dict) -> None:
    if not settings.convex_http_url or not settings.convex_hint_secret:
        return
    body = json.dumps(payload, separators=(",", ":"), sort_keys=True)
    timestamp = datetime.now(timezone.utc).isoformat()
    signature = _sign(settings.convex_hint_secret, timestamp, body)
    response = httpx.post(
        settings.convex_http_url.rstrip("/") + path,
        content=body,
        headers={
            "Content-Type": "application/json",
            "X-Hint-Timestamp": timestamp,
            "X-Hint-Signature": signature,
        },
        timeout=3.0,
    )
    response.raise_for_status()


def allowed_recipient(conn, user_id: str, workspace_id: str, project_id: str | None) -> bool:
    if project_id:
        row = conn.execute(
            """
            SELECT 1
            FROM app.project_memberships
            WHERE project_id = %s AND user_id = %s AND status = 'active'
            """,
            (project_id, user_id),
        ).fetchone()
        return row is not None
    row = conn.execute(
        """
        SELECT 1
        FROM app.workspace_memberships
        WHERE workspace_id = %s AND user_id = %s AND status = 'active'
        """,
        (workspace_id, user_id),
    ).fetchone()
    return row is not None


def push_convex(conn, event: dict) -> None:
    if not settings.convex_http_url or not settings.convex_hint_secret:
        return
    payload = event.get("payload") or {}
    if event["kind"] == "access.revoked":
        for recipient in payload.get("recipients") or []:
            _post_hint(
                "/hints/revoke",
                {
                    "eventId": str(event["id"]),
                    "userId": str(recipient),
                    "workspaceId": str(event["workspace_id"]),
                    "projectId": str(event["project_id"]) if event.get("project_id") else None,
                    "scope": (payload.get("extra") or {}).get("scope") or "workspace",
                },
            )
        return
    recipients = [
        str(recipient)
        for recipient in (payload.get("recipients") or [])
        if allowed_recipient(conn, str(recipient), str(event["workspace_id"]), str(event["project_id"]) if event.get("project_id") else None)
    ]
    if not recipients:
        return
    hint = {
        "eventId": str(event["id"]),
        "recipients": recipients,
        "workspaceId": str(event["workspace_id"]),
        "resourceType": event["resource_type"],
        "resourceId": str(event["resource_id"]),
        "revision": int(event["revision"] or 0),
        "updatedAt": int(event["created_at"].timestamp() * 1000) if event.get("created_at") else 0,
    }
    if event.get("project_id"):
        hint["projectId"] = str(event["project_id"])
    _post_hint("/hints", hint)


def dispatch_outbox(limit: int = 100) -> int:
    if not settings.database_url:
        return 0
    delivered = 0
    try:
        with admin_connection() as conn:
            rows = conn.execute(
                """
                SELECT * FROM app.outbox_events
                WHERE dispatched_at IS NULL
                ORDER BY created_at, id
                LIMIT %s
                FOR UPDATE SKIP LOCKED
                """,
                (limit,),
            ).fetchall()
            for row in rows:
                recipients = (row["payload"] or {}).get("recipients") or []
                for recipient in recipients:
                    if not allowed_recipient(
                        conn,
                        str(recipient),
                        str(row["workspace_id"]),
                        str(row["project_id"]) if row.get("project_id") else None,
                    ):
                        continue
                    conn.execute(
                        """
                        INSERT INTO app.notifications (
                            workspace_id, recipient_id, source_event_id, kind,
                            target_type, target_id, preview
                        ) VALUES (%s, %s, %s, %s, %s, %s, %s)
                        ON CONFLICT (source_event_id, recipient_id) DO NOTHING
                        """,
                        (
                            row["workspace_id"],
                            recipient,
                            row["id"],
                            row["kind"],
                            (row["payload"] or {}).get("target_type") or row["resource_type"],
                            (row["payload"] or {}).get("target_id") or row["resource_id"],
                            (row["payload"] or {}).get("preview") or "",
                        ),
                    )
                try:
                    push_convex(conn, dict(row))
                except Exception:
                    continue
                conn.execute(
                    "UPDATE app.outbox_events SET dispatched_at = now() WHERE id = %s",
                    (row["id"],),
                )
                delivered += 1
    except AppError:
        return delivered
    return delivered
