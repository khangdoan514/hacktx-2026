import httpx

from app.core.config import settings
from app.core.database import admin_connection
from app.core.errors import AppError


def push_convex(event: dict) -> None:
    if not settings.convex_url or not settings.convex_deploy_key:
        return
    payload = {
        "path": "hints:push",
        "format": "json",
        "args": {
            "workspaceId": str(event["workspace_id"]),
            "resourceType": event["resource_type"],
            "resourceId": str(event["resource_id"]),
            "revision": int(event["revision"] or 0),
            "updatedAt": int(event["created_at"].timestamp() * 1000) if event.get("created_at") else 0,
        },
    }
    if event.get("project_id"):
        payload["args"]["projectId"] = str(event["project_id"])
    response = httpx.post(
        settings.convex_url.rstrip("/") + "/api/mutation",
        json=payload,
        headers={"Authorization": f"Convex {settings.convex_deploy_key}"},
        timeout=3.0,
    )
    response.raise_for_status()


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
                    push_convex(dict(row))
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
