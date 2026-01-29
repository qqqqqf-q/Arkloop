"""API 服务的 composition root：集中做依赖注入与启动配置。"""

from __future__ import annotations

from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from typing import Dict

from fastapi import APIRouter, FastAPI

from packages.data import Database, DatabaseConfig
from packages.observability.logging import configure_json_logging

from .db import install_database
from .error_envelope import install_error_handlers, install_unhandled_exception_middleware
from .run_executor import configure_run_executor
from .sse import configure_sse
from .trace import install_trace_id_middleware
from .v1 import configure_auth, v1_router

_health_router = APIRouter()


@_health_router.get("/healthz")
async def healthz() -> Dict[str, str]:
    return {"status": "ok"}


@asynccontextmanager
async def _lifespan(app: FastAPI) -> AsyncIterator[None]:
    run_executor = getattr(app.state, "run_executor", None)
    if run_executor is not None:
        await run_executor.start()
    yield
    if run_executor is not None:
        await run_executor.stop()
    database = getattr(app.state, "database", None)
    if isinstance(database, Database):
        await database.dispose()


def configure_logging() -> None:
    configure_json_logging(component="api")


def configure_database(app: FastAPI) -> None:
    config = DatabaseConfig.from_env(allow_fallback=False)
    if config is None:
        return

    database = Database.from_config(config)
    install_database(app, database)


def create_app() -> FastAPI:
    app = FastAPI(title="Arkloop API", lifespan=_lifespan)
    install_unhandled_exception_middleware(app)
    install_error_handlers(app)
    install_trace_id_middleware(app)
    app.include_router(_health_router)
    app.include_router(v1_router)
    return app


def configure_app() -> FastAPI:
    configure_logging()
    app = create_app()
    configure_database(app)
    configure_run_executor(app)
    configure_auth(app)
    configure_sse(app)
    return app
