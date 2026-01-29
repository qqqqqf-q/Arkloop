from __future__ import annotations

from collections.abc import AsyncIterator

from fastapi import FastAPI, Request
from sqlalchemy.ext.asyncio import AsyncSession

from packages.data import Database

from .error_envelope import ApiError


def install_database(app: FastAPI, database: Database) -> None:
    app.state.database = database


def _get_database(app: FastAPI) -> Database:
    database = getattr(app.state, "database", None)
    if isinstance(database, Database):
        return database
    raise ApiError(code="database.not_configured", message="数据库未配置", status_code=503)


async def get_db_session(request: Request) -> AsyncIterator[AsyncSession]:
    database = _get_database(request.app)
    async with database.sessionmaker() as session:
        try:
            yield session
            await session.commit()
        except Exception:
            await session.rollback()
            raise
