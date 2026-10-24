from collections.abc import Iterator
from contextlib import contextmanager

import psycopg
from psycopg.rows import dict_row

from app.core.config import settings
from app.core.errors import AppError

_KNOWN = {
    "last_owner": (409, "last_owner", "The workspace needs one active owner"),
    "project_cycle": (422, "project_cycle", "A project cannot contain itself"),
    "parent_workspace_mismatch": (422, "invalid_parent", "Parent project is in another workspace"),
    "workspace_mismatch": (422, "workspace_mismatch", "Record does not belong to that workspace"),
    "forbidden": (403, "forbidden", "You cannot do that"),
    "not_found": (404, "not_found", "Record not found"),
}


def _map_db_error(exc: psycopg.Error) -> AppError:
    text = str(exc)
    for needle, (status, code, message) in _KNOWN.items():
        if needle in text:
            return AppError(status, code, message)
    if getattr(exc, "sqlstate", None) == "23505":
        return AppError(409, "conflict", "That record already exists")
    return AppError(409, "constraint", "The change violates a data rule")


@contextmanager
def admin_connection() -> Iterator[psycopg.Connection]:
    if not settings.database_url:
        raise AppError(503, "unavailable", "Database is not configured")
    conn = psycopg.connect(settings.database_url, row_factory=dict_row)
    try:
        with conn.transaction():
            yield conn
    except psycopg.Error as exc:
        raise _map_db_error(exc) from exc
    finally:
        conn.close()


@contextmanager
def user_connection(user_id: str, email: str) -> Iterator[psycopg.Connection]:
    if not settings.database_url:
        raise AppError(503, "unavailable", "Database is not configured")
    conn = psycopg.connect(settings.database_url, row_factory=dict_row)
    try:
        with conn.transaction():
            conn.execute("SET LOCAL ROLE app_runtime")
            conn.execute("SELECT set_config('app.user_id', %s, true)", (str(user_id),))
            conn.execute("SELECT set_config('app.user_email', %s, true)", (email or "",))
            yield conn
    except psycopg.Error as exc:
        raise _map_db_error(exc) from exc
    finally:
        conn.close()
