from __future__ import annotations

from dataclasses import dataclass

from sqlalchemy import text
from sqlalchemy.ext.asyncio import (
    AsyncEngine,
    AsyncSession,
    async_sessionmaker,
    create_async_engine,
)

from .config import DatabaseConfig


@dataclass(frozen=True)
class Database:
    engine: AsyncEngine
    sessionmaker: async_sessionmaker[AsyncSession]

    @classmethod
    def from_config(cls, config: DatabaseConfig) -> "Database":
        engine = create_async_engine(config.url, pool_pre_ping=True)
        sessionmaker = async_sessionmaker(engine, expire_on_commit=False)
        return cls(engine=engine, sessionmaker=sessionmaker)

    async def dispose(self) -> None:
        await self.engine.dispose()

    async def select_one(self) -> int:
        async with self.sessionmaker() as session:
            result = await session.execute(text("SELECT 1"))
            return int(result.scalar_one())

