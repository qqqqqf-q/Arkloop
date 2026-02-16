from __future__ import annotations

import asyncio
from pathlib import Path
import re
from urllib.parse import urlsplit, urlunsplit
import uuid

from alembic import command
from alembic.config import Config
import anyio
import asyncpg
import pytest
import sqlalchemy as sa

from packages.data import Database, DatabaseConfig
from packages.job_queue import (
    JOB_STATUS_DEAD,
    JOB_STATUS_DONE,
    RUN_EXECUTE_JOB_TYPE,
    RUN_EXECUTE_QUEUE_JOB_TYPE_GO_BRIDGE,
    SqlAlchemyPgJobQueue,
)
from services.worker import WorkerJobPayload
from services.worker.job_queue import get_job_queue as get_worker_job_queue

pytestmark = pytest.mark.integration


def _repo_root() -> Path:
    current = Path(__file__).resolve()
    for parent in current.parents:
        if (parent / "pyproject.toml").exists():
            return parent
    raise AssertionError("未找到仓库根目录（pyproject.toml）")


def _replace_database(url: str, database: str) -> str:
    parsed = urlsplit(url)
    path = f"/{database}"
    return urlunsplit((parsed.scheme, parsed.netloc, path, parsed.query, parsed.fragment))


def _to_asyncpg_dsn(sqlalchemy_url: str) -> str:
    parsed = urlsplit(sqlalchemy_url)
    scheme = "postgresql" if parsed.scheme == "postgresql+asyncpg" else parsed.scheme
    return urlunsplit((scheme, parsed.netloc, parsed.path, parsed.query, parsed.fragment))


def _safe_identifier(name: str) -> str:
    if not re.fullmatch(r"[A-Za-z0-9_]+", name):
        raise ValueError("非法标识符")
    return f'"{name}"'


async def _create_database(admin_dsn: str, database: str) -> None:
    conn = await asyncpg.connect(admin_dsn)
    try:
        await conn.execute(f"CREATE DATABASE {_safe_identifier(database)}")
    finally:
        await conn.close()


async def _drop_database(admin_dsn: str, database: str) -> None:
    conn = await asyncpg.connect(admin_dsn)
    try:
        ident = _safe_identifier(database)
        try:
            await conn.execute(f"DROP DATABASE {ident} WITH (FORCE)")
        except asyncpg.PostgresError:
            await conn.execute(
                "SELECT pg_terminate_backend(pid) FROM pg_stat_activity "
                "WHERE datname = $1 AND pid <> pg_backend_pid()",
                database,
            )
            await conn.execute(f"DROP DATABASE {ident}")
    finally:
        await conn.close()


@pytest.fixture()
def migrated_database_url(monkeypatch) -> str:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    repo_root = _repo_root()
    alembic_cfg = Config(str(repo_root / "alembic.ini"))

    database = f"arkloop_job_queue_{uuid.uuid4().hex}"
    sqlalchemy_url = config.url
    admin_dsn = _replace_database(_to_asyncpg_dsn(sqlalchemy_url), "postgres")
    test_sqlalchemy_url = _replace_database(sqlalchemy_url, database)

    anyio.run(_create_database, admin_dsn, database)
    try:
        with monkeypatch.context() as m:
            m.setenv("DATABASE_URL", test_sqlalchemy_url)
            command.upgrade(alembic_cfg, "head")
        yield test_sqlalchemy_url
    finally:
        anyio.run(_drop_database, admin_dsn, database)


def test_pg_job_queue_lease_is_mutually_exclusive(migrated_database_url: str) -> None:
    database = Database.from_config(DatabaseConfig(url=migrated_database_url))

    async def _run() -> None:
        try:
            org_id = uuid.uuid4()
            run_id = uuid.uuid4()
            trace_id = uuid.uuid4().hex

            async with database.sessionmaker() as session:
                queue = SqlAlchemyPgJobQueue(session)
                job_id = await queue.enqueue_run(
                    org_id=org_id,
                    run_id=run_id,
                    trace_id=trace_id,
                    payload={"source": "integration_test"},
                )
                await session.commit()

            start = asyncio.Event()
            ready: asyncio.Queue[str] = asyncio.Queue()

            async def _lease_once(tag: str):
                async with database.sessionmaker() as session:
                    queue = get_worker_job_queue(session)
                    await ready.put(tag)
                    await start.wait()
                    lease = await queue.lease(lease_seconds=60)
                    await session.commit()
                    return lease

            t1 = asyncio.create_task(_lease_once("a"))
            t2 = asyncio.create_task(_lease_once("b"))
            await ready.get()
            await ready.get()
            start.set()
            lease1, lease2 = await asyncio.gather(t1, t2)

            leases = [item for item in (lease1, lease2) if item is not None]
            assert len(leases) == 1
            assert leases[0].job_id == job_id
            assert leases[0].job_type == RUN_EXECUTE_JOB_TYPE

            async with database.sessionmaker() as session:
                queue = get_worker_job_queue(session)
                await queue.nack(lease=leases[0], delay_seconds=0)
                await session.commit()

            async with database.sessionmaker() as session:
                queue = get_worker_job_queue(session)
                lease = await queue.lease(lease_seconds=60)
                assert lease is not None
                await queue.ack(lease=lease)
                await session.commit()

            async with database.sessionmaker() as session:
                row = (
                    await session.execute(
                        sa.text("SELECT status FROM jobs WHERE id = :job_id"),
                        {"job_id": job_id},
                    )
                ).one_or_none()
                assert row is not None
                assert row[0] == JOB_STATUS_DONE
        finally:
            await database.dispose()

    anyio.run(_run)


def test_pg_job_queue_payload_is_compatible_with_worker_job_payload(
    migrated_database_url: str,
) -> None:
    database = Database.from_config(DatabaseConfig(url=migrated_database_url))

    async def _run() -> None:
        try:
            org_id = uuid.uuid4()
            run_id = uuid.uuid4()
            trace_id = uuid.uuid4().hex

            async with database.sessionmaker() as session:
                queue = SqlAlchemyPgJobQueue(session)
                job_id = await queue.enqueue_run(
                    org_id=org_id,
                    run_id=run_id,
                    trace_id=trace_id,
                    payload={"note": "compat"},
                )
                await session.commit()

            async with database.sessionmaker() as session:
                queue = get_worker_job_queue(session)
                lease = await queue.lease(lease_seconds=60)
                await session.commit()

            assert lease is not None
            job = WorkerJobPayload.from_json(lease.payload_json)
            assert job.job_id == job_id
            assert job.job_type == RUN_EXECUTE_JOB_TYPE
            assert job.org_id == org_id
            assert job.run_id == run_id
            assert job.trace_id is not None
        finally:
            await database.dispose()

    anyio.run(_run)


def test_pg_job_queue_dead_letters_after_max_attempts(migrated_database_url: str) -> None:
    database = Database.from_config(DatabaseConfig(url=migrated_database_url))

    async def _run() -> None:
        try:
            org_id = uuid.uuid4()
            run_id = uuid.uuid4()
            trace_id = uuid.uuid4().hex

            async with database.sessionmaker() as session:
                queue = SqlAlchemyPgJobQueue(session, max_attempts=2)
                job_id = await queue.enqueue_run(
                    org_id=org_id,
                    run_id=run_id,
                    trace_id=trace_id,
                    payload={"note": "dead_letter"},
                )
                await session.commit()

                lease1 = await queue.lease(lease_seconds=60)
                assert lease1 is not None
                assert lease1.attempts == 1
                await queue.nack(lease=lease1, delay_seconds=0)
                await session.commit()

                lease2 = await queue.lease(lease_seconds=60)
                assert lease2 is not None
                assert lease2.attempts == 2
                await queue.nack(lease=lease2, delay_seconds=0)
                await session.commit()

                lease3 = await queue.lease(lease_seconds=60)
                assert lease3 is None

                row = (
                    await session.execute(
                        sa.text("SELECT status, attempts FROM jobs WHERE id = :job_id"),
                        {"job_id": job_id},
                    )
                ).one_or_none()
                assert row is not None
                assert row[0] == JOB_STATUS_DEAD
                assert int(row[1]) == 2
        finally:
            await database.dispose()

    anyio.run(_run)


def test_pg_job_queue_lease_can_filter_by_job_type(migrated_database_url: str) -> None:
    database = Database.from_config(DatabaseConfig(url=migrated_database_url))

    async def _run() -> None:
        try:
            org_id = uuid.uuid4()
            run_id = uuid.uuid4()
            trace_id = uuid.uuid4().hex

            async with database.sessionmaker() as session:
                queue = SqlAlchemyPgJobQueue(session)
                python_job_id = await queue.enqueue_run(
                    org_id=org_id,
                    run_id=run_id,
                    trace_id=trace_id,
                    payload={"note": "python"},
                )
                go_job_id = await queue.enqueue_run(
                    org_id=org_id,
                    run_id=run_id,
                    trace_id=trace_id,
                    queue_job_type=RUN_EXECUTE_QUEUE_JOB_TYPE_GO_BRIDGE,
                    payload={"note": "go_bridge"},
                )
                await session.commit()

            async with database.sessionmaker() as session:
                queue = get_worker_job_queue(session)
                lease = await queue.lease(
                    lease_seconds=60,
                    job_types=(RUN_EXECUTE_QUEUE_JOB_TYPE_GO_BRIDGE,),
                )
                assert lease is not None
                assert lease.job_id == go_job_id
                await queue.ack(lease=lease)
                await session.commit()

            async with database.sessionmaker() as session:
                queue = get_worker_job_queue(session)
                lease = await queue.lease(lease_seconds=60, job_types=(RUN_EXECUTE_JOB_TYPE,))
                assert lease is not None
                assert lease.job_id == python_job_id
                await queue.ack(lease=lease)
                await session.commit()

            async with database.sessionmaker() as session:
                python_status = (
                    await session.execute(
                        sa.text("SELECT status FROM jobs WHERE id = :job_id"),
                        {"job_id": python_job_id},
                    )
                ).one_or_none()
                go_status = (
                    await session.execute(
                        sa.text("SELECT status FROM jobs WHERE id = :job_id"),
                        {"job_id": go_job_id},
                    )
                ).one_or_none()
                assert python_status is not None
                assert go_status is not None
                assert python_status[0] == JOB_STATUS_DONE
                assert go_status[0] == JOB_STATUS_DONE
        finally:
            await database.dispose()

    anyio.run(_run)
