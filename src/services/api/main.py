"""API 服务的 composition root：集中做依赖注入与启动配置。"""

from __future__ import annotations

from typing import Dict

from fastapi import APIRouter, FastAPI

from packages.data import Database, DatabaseConfig
from packages.observability.logging import configure_json_logging

from .db import install_database
from .error_envelope import install_error_handlers, install_unhandled_exception_middleware
from .trace import install_trace_id_middleware

_health_router = APIRouter()


@_health_router.get("/healthz")
async def healthz() -> Dict[str, str]:
    return {"status": "ok"}


def configure_logging() -> None:
    configure_json_logging(component="api")


def configure_database(app: FastAPI) -> None:
    config = DatabaseConfig.from_env(allow_fallback=False)
    if config is None:
        return

    database = Database.from_config(config)
    install_database(app, database)

    @app.on_event("shutdown")
    async def _shutdown_database() -> None:
        await database.dispose()


def create_app() -> FastAPI:
    app = FastAPI(title="Arkloop API")
    install_unhandled_exception_middleware(app)
    install_error_handlers(app)
    install_trace_id_middleware(app)
    app.include_router(_health_router)
    return app


def configure_app() -> FastAPI:
    configure_logging()
    app = create_app()
    configure_database(app)
    return app
