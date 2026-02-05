"""API 服务的 composition root：集中做依赖注入与启动配置。"""

from __future__ import annotations

from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
import logging
from pathlib import Path
from typing import Dict

from alembic.config import Config
from alembic.runtime.migration import MigrationContext
from alembic.script import ScriptDirectory
from fastapi import APIRouter, FastAPI
from sqlalchemy.ext.asyncio import AsyncEngine

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

def _alembic_script_dir() -> ScriptDirectory:
    src_root = Path(__file__).resolve().parents[2]
    cfg = Config()
    cfg.set_main_option("script_location", str(src_root / "migrations"))
    return ScriptDirectory.from_config(cfg)


async def _alembic_current_revision(engine: AsyncEngine) -> str | None:
    async with engine.connect() as conn:
        def _get_revision(sync_conn) -> str | None:
            return MigrationContext.configure(sync_conn).get_current_revision()

        return await conn.run_sync(_get_revision)


async def _ensure_database_is_up_to_date(engine: AsyncEngine) -> None:
    logger = logging.getLogger("arkloop.api")
    head = _alembic_script_dir().get_current_head()
    current = await _alembic_current_revision(engine)
    if not head or current == head:
        return

    logger.error(
        "database schema out of date",
        extra={"current_revision": current, "head_revision": head},
    )
    raise RuntimeError(
        f"数据库迁移未执行：当前 {current or 'none'}，需要 {head}。"
        "请先运行: python -m alembic upgrade head"
    )


@asynccontextmanager
async def _lifespan(app: FastAPI) -> AsyncIterator[None]:
    database = getattr(app.state, "database", None)
    if isinstance(database, Database):
        await _ensure_database_is_up_to_date(database.engine)

    run_executor = getattr(app.state, "run_executor", None)
    if run_executor is not None:
        await run_executor.start()
    yield
    if run_executor is not None:
        await run_executor.stop()
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
