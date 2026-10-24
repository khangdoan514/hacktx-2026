from fastapi import APIRouter

from app.api.routes import health, hello, ready
from app.api.routes.v1 import router as v1_router

api_router = APIRouter(prefix="/api")
api_router.include_router(health.router)
api_router.include_router(ready.router)
api_router.include_router(hello.router)
api_router.include_router(v1_router)
