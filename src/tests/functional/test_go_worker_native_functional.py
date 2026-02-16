from __future__ import annotations

import os
from pathlib import Path
import re
import subprocess
import time
from urllib.parse import urlsplit, urlunsplit
import uuid

from alembic import command
from alembic.config import Config
import anyio
import asyncpg
from fastapi.testclient import TestClient
import pytest

from packages.auth import BcryptPasswordHasher
from packages.data import Database, DatabaseConfig
from packages.data.credentials import SqlAlchemyUserCredentialRepository
from packages.data.identity import (
    SqlAlchemyOrgMembershipRepository,
    SqlAlchemyOrgRepository,
    SqlAlchemyUserRepository,
)
from packages.data.runs import SqlAlchemyRunEventRepository
from services.api.main import configure_app

pytestmark = pytest.mark.functional

_TERMINAL_EVENT_TYPES = ("run.completed", "run.failed", "run.cancelled")

_STUB_PROVIDER_ROUTING_JSON = (
    '{"default_route_id":"default","credentials":[{"id":"stub_default","scope":"platform",'
    '"provider_kind":"stub"}],"routes":[{"id":"default","model":"stub","credential_id":"stub_default"}]}'
)


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


async def _seed_auth(sqlalchemy_url: str, login: str, password: str) -> None:
    database = Database.from_config(DatabaseConfig(url=sqlalchemy_url))
    try:
        async with database.sessionmaker() as session:
            org_repo = SqlAlchemyOrgRepository(session)
            user_repo = SqlAlchemyUserRepository(session)
            membership_repo = SqlAlchemyOrgMembershipRepository(session)
            credential_repo = SqlAlchemyUserCredentialRepository(session)

            slug = f"org_{uuid.uuid4().hex}"
            org = await org_repo.create(slug=slug, name=f"Org {slug}")
            user = await user_repo.create(display_name="Alice")

            hasher = BcryptPasswordHasher()
            await credential_repo.create(
                user_id=user.id,
                login=login,
                password_hash=hasher.hash_password(password),
            )
            await membership_repo.create(org_id=org.id, user_id=user.id, role="member")
            await session.commit()
    finally:
        await database.dispose()

async def _wait_terminal_events(sqlalchemy_url: str, run_id: uuid.UUID, timeout_seconds: float) -> list[str]:
    database = Database.from_config(DatabaseConfig(url=sqlalchemy_url))
    deadline = time.monotonic() + timeout_seconds
    try:
        while time.monotonic() < deadline:
            async with database.sessionmaker() as session:
                repo = SqlAlchemyRunEventRepository(session)
                terminal = await repo.get_latest_event_type(run_id=run_id, types=_TERMINAL_EVENT_TYPES)
                if terminal is not None:
                    events = await repo.list_events(run_id=run_id, after_seq=0, limit=50)
                    return [event.type for event in events]
            await anyio.sleep(0.05)
    finally:
        await database.dispose()
    raise AssertionError("等待 run 终态超时")


def test_go_worker_executes_run_via_native_engine(monkeypatch) -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    repo_root = _repo_root()
    alembic_cfg = Config(str(repo_root / "alembic.ini"))

    database = f"arkloop_wg07_native_{uuid.uuid4().hex}"
    sqlalchemy_url = config.url
    admin_dsn = _replace_database(_to_asyncpg_dsn(sqlalchemy_url), "postgres")
    test_sqlalchemy_url = _replace_database(sqlalchemy_url, database)

    anyio.run(_create_database, admin_dsn, database)
    try:
        with monkeypatch.context() as m:
            m.setenv("DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")
            m.setenv("ARKLOOP_RUN_EXECUTOR", "worker")
            m.setenv("ARKLOOP_PROVIDER_ROUTING_JSON", _STUB_PROVIDER_ROUTING_JSON)
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_COUNT", "2")
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_INTERVAL_SECONDS", "0")

            m.setenv("ARKLOOP_WORKER_GO_TRAFFIC_PERCENT", "100")
            m.setenv("ARKLOOP_WORKER_GO_EXECUTOR", "native")

            command.upgrade(alembic_cfg, "head")

            login = "alice"
            password = "pwdpwdpwd"
            anyio.run(_seed_auth, test_sqlalchemy_url, login, password)

            app = configure_app()
            with TestClient(app) as client:
                auth = client.post("/v1/auth/login", json={"login": login, "password": password})
                assert auth.status_code == 200
                bearer = auth.json()["access_token"]
                headers = {"Authorization": f"Bearer {bearer}"}

                thread_resp = client.post("/v1/threads", json={"title": "t"}, headers=headers)
                assert thread_resp.status_code == 201
                thread_id = thread_resp.json()["id"]

                run_resp = client.post(f"/v1/threads/{thread_id}/runs", headers=headers)
                assert run_resp.status_code == 201
                run_id = uuid.UUID(run_resp.json()["run_id"])

            worker_go_root = repo_root / "src/services/worker_go"
            env = os.environ.copy()
            env["ARKLOOP_WORKER_CONCURRENCY"] = "1"
            env["ARKLOOP_WORKER_POLL_SECONDS"] = "0.05"
            env["ARKLOOP_WORKER_HEARTBEAT_SECONDS"] = "0"
            env["ARKLOOP_WORKER_QUEUE_JOB_TYPES"] = "run.execute.go_native"

            proc = subprocess.Popen(
                ["go", "run", "./cmd/worker"],
                cwd=str(worker_go_root),
                env=env,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            try:
                event_types = anyio.run(_wait_terminal_events, test_sqlalchemy_url, run_id, 30.0)
            finally:
                proc.terminate()
                try:
                    proc.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait(timeout=5)

            assert event_types == [
                "run.started",
                "worker.job.received",
                "run.route.selected",
                "message.delta",
                "message.delta",
                "run.completed",
            ]
    finally:
        anyio.run(_drop_database, admin_dsn, database)
