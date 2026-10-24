from fastapi import APIRouter
from fastapi.responses import JSONResponse

from app.core.config import settings
from app.core.database import admin_connection

router = APIRouter(tags=["health"])


@router.get("/ready")
def ready():
    if not settings.database_url:
        return JSONResponse(status_code=503, content={"status": "not_ready"})
    try:
        with admin_connection() as conn:
            conn.execute("SELECT 1")
    except Exception:
        return JSONResponse(status_code=503, content={"status": "not_ready"})
    return {"status": "ready"}
