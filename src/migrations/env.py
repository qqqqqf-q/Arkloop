from __future__ import annotations

import asyncio
import os
from urllib.parse import SplitResult, urlsplit, urlunsplit

from alembic import context
from sqlalchemy import create_engine, pool
from sqlalchemy.engine import Connection
from sqlalchemy.ext.asyncio import create_async_engine

from packages.config import load_dotenv_if_enabled
from packages.observability.context import new_trace_id, set_trace_id
from packages.observability.logging import configure_json_logging

config = context.config
target_metadata = None


def _replace_scheme(parsed: SplitResult, scheme: str) -> str:
    return urlunsplit((scheme, parsed.netloc, parsed.path, parsed.query, parsed.fragment))


def _normalize_database_url(raw: str) -> tuple[str, bool]:
    cleaned = raw.strip()
    if not cleaned:
        raise ValueError("DATABASE_URL 不能为空")

    parsed = urlsplit(cleaned)
    scheme = parsed.scheme.casefold()

    if scheme in {"postgres", "postgresql"}:
        return _replace_scheme(parsed, "postgresql+asyncpg"), True

    if scheme == "postgresql+asyncpg":
        return cleaned, True

    if scheme.startswith("sqlite"):
        return cleaned, False

    if scheme.startswith("postgresql+"):
        return cleaned, False

    raise ValueError("仅支持 PostgreSQL 或 SQLite 连接串")


def _read_database_url() -> tuple[str, bool]:
    load_dotenv_if_enabled(override=False)

    raw = os.getenv("DATABASE_URL") or os.getenv("ARKLOOP_DATABASE_URL")
    if not raw:
        raise ValueError("缺少环境变量 DATABASE_URL（或 ARKLOOP_DATABASE_URL）")
    return _normalize_database_url(raw)


def _do_run_migrations(connection: Connection) -> None:
    context.configure(connection=connection, target_metadata=target_metadata)
    with context.begin_transaction():
        context.run_migrations()


async def _run_async_migrations(url: str) -> None:
    connectable = create_async_engine(url, poolclass=pool.NullPool)
    try:
        async with connectable.connect() as connection:
            await connection.run_sync(_do_run_migrations)
    finally:
        await connectable.dispose()


def _run_sync_migrations(url: str) -> None:
    connectable = create_engine(url, poolclass=pool.NullPool)
    with connectable.connect() as connection:
        _do_run_migrations(connection)


def run_migrations_offline() -> None:
    url, _ = _read_database_url()
    config.set_main_option("sqlalchemy.url", url)

    context.configure(
        url=url,
        target_metadata=target_metadata,
        literal_binds=True,
        dialect_opts={"paramstyle": "named"},
    )

    with context.begin_transaction():
        context.run_migrations()


def run_migrations_online() -> None:
    url, use_async = _read_database_url()
    config.set_main_option("sqlalchemy.url", url)

    if use_async:
        asyncio.run(_run_async_migrations(url))
        return

    _run_sync_migrations(url)


def _bootstrap_observability() -> None:
    set_trace_id(new_trace_id())
    configure_json_logging(component="alembic")


_bootstrap_observability()

if context.is_offline_mode():
    run_migrations_offline()
else:
    run_migrations_online()
