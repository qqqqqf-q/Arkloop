from __future__ import annotations

import json
from pathlib import Path
import re
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
from services.api.trace import TRACE_ID_HEADER

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
                user_id=user.id, login=login, password_hash=hasher.hash_password(password)
            )
            await membership_repo.create(org_id=org.id, user_id=user.id, role="member")
            await session.commit()
    finally:
        await database.dispose()


async def _list_events(sqlalchemy_url: str, run_id: uuid.UUID) -> list[tuple[int, str]]:
    database = Database.from_config(DatabaseConfig(url=sqlalchemy_url))
    try:
        async with database.sessionmaker() as session:
            repo = SqlAlchemyRunEventRepository(session)
            events = await repo.list_events(run_id=run_id, after_seq=0)
            return [(event.seq, event.type) for event in events]
    finally:
        await database.dispose()


async def _append_events(sqlalchemy_url: str, run_id: uuid.UUID) -> None:
    database = Database.from_config(DatabaseConfig(url=sqlalchemy_url))
    try:
        async with database.sessionmaker() as session:
            repo = SqlAlchemyRunEventRepository(session)
            await repo.append_event(
                run_id=run_id, type="message.delta", data_json={"content_delta": "hi"}
            )
            await repo.append_event(run_id=run_id, type="run.completed", data_json={})
            await session.commit()
    finally:
        await database.dispose()


def _collect_sse_json_events(
    response, *, expected: int, timeout_seconds: float = 5.0
) -> list[dict]:
    deadline = time.monotonic() + timeout_seconds
    events: list[dict] = []
    buffer: list[str] = []

    for line in response.iter_lines():
        if time.monotonic() > deadline:
            raise AssertionError("读取 SSE 超时")

        if line.startswith(":"):
            continue
        if line == "":
            data_lines = [
                item[len("data:") :].lstrip() for item in buffer if item.startswith("data:")
            ]
            buffer.clear()
            if not data_lines:
                continue
            events.append(json.loads("\n".join(data_lines)))
            if len(events) >= expected:
                break
            continue

        buffer.append(line)

    return events


def test_create_run_persists_run_started_event(monkeypatch) -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    repo_root = _repo_root()
    alembic_cfg = Config(str(repo_root / "alembic.ini"))

    database = f"arkloop_runs_api_{uuid.uuid4().hex}"
    sqlalchemy_url = config.url
    admin_dsn = _replace_database(_to_asyncpg_dsn(sqlalchemy_url), "postgres")
    test_sqlalchemy_url = _replace_database(sqlalchemy_url, database)

    anyio.run(_create_database, admin_dsn, database)
    try:
        with monkeypatch.context() as m:
            m.setenv("DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")
            command.upgrade(alembic_cfg, "head")

            login = "alice"
            password = "pwdpwdpwd"
            anyio.run(_seed_auth, test_sqlalchemy_url, login, password)

            app = configure_app()
            with TestClient(app) as client:
                auth = client.post("/v1/auth/login", json={"login": login, "password": password})
                assert auth.status_code == 200
                token = auth.json()["access_token"]

                thread_resp = client.post(
                    "/v1/threads",
                    json={"title": "t"},
                    headers={"Authorization": f"Bearer {token}"},
                )
                assert thread_resp.status_code == 201
                thread_id = thread_resp.json()["id"]

                run_resp = client.post(
                    f"/v1/threads/{thread_id}/runs",
                    headers={"Authorization": f"Bearer {token}"},
                )
                assert run_resp.status_code == 201
                assert TRACE_ID_HEADER in run_resp.headers
                assert run_resp.headers[TRACE_ID_HEADER]
                payload = run_resp.json()
                assert payload["trace_id"] == run_resp.headers[TRACE_ID_HEADER]

                run_id = uuid.UUID(payload["run_id"])
                events = anyio.run(_list_events, test_sqlalchemy_url, run_id)
                assert events == [(1, "run.started")]
    finally:
        anyio.run(_drop_database, admin_dsn, database)


def test_run_events_sse_streams_and_resumes(monkeypatch) -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    repo_root = _repo_root()
    alembic_cfg = Config(str(repo_root / "alembic.ini"))

    database = f"arkloop_runs_sse_{uuid.uuid4().hex}"
    sqlalchemy_url = config.url
    admin_dsn = _replace_database(_to_asyncpg_dsn(sqlalchemy_url), "postgres")
    test_sqlalchemy_url = _replace_database(sqlalchemy_url, database)

    anyio.run(_create_database, admin_dsn, database)
    try:
        with monkeypatch.context() as m:
            m.setenv("DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")
            m.setenv("ARKLOOP_SSE_POLL_SECONDS", "0.01")
            m.setenv("ARKLOOP_SSE_HEARTBEAT_SECONDS", "0.05")
            command.upgrade(alembic_cfg, "head")

            login = "alice"
            password = "pwdpwdpwd"
            anyio.run(_seed_auth, test_sqlalchemy_url, login, password)

            app = configure_app()
            with TestClient(app) as client:
                auth = client.post("/v1/auth/login", json={"login": login, "password": password})
                assert auth.status_code == 200
                token = auth.json()["access_token"]
                headers = {"Authorization": f"Bearer {token}"}

                thread_resp = client.post("/v1/threads", json={"title": "t"}, headers=headers)
                assert thread_resp.status_code == 201
                thread_id = thread_resp.json()["id"]

                run_resp = client.post(f"/v1/threads/{thread_id}/runs", headers=headers)
                assert run_resp.status_code == 201
                run_id = uuid.UUID(run_resp.json()["run_id"])

                anyio.run(_append_events, test_sqlalchemy_url, run_id)

                unauth = client.get(f"/v1/runs/{run_id}/events?after_seq=0&follow=0")
                assert unauth.status_code == 401

                with client.stream(
                    "GET",
                    f"/v1/runs/{run_id}/events?after_seq=0&follow=0",
                    headers=headers,
                ) as resp:
                    assert resp.status_code == 200
                    assert resp.headers["content-type"].startswith("text/event-stream")
                    assert TRACE_ID_HEADER in resp.headers
                    assert resp.headers[TRACE_ID_HEADER]
                    events = _collect_sse_json_events(resp, expected=3)

                assert [event["seq"] for event in events] == [1, 2, 3]
                assert [event["type"] for event in events] == [
                    "run.started",
                    "message.delta",
                    "run.completed",
                ]
                for event in events:
                    assert event["run_id"] == str(run_id)
                    assert event["event_id"]
                    assert event["ts"]
                    assert "data" in event

                with client.stream(
                    "GET",
                    f"/v1/runs/{run_id}/events?after_seq=1&follow=0",
                    headers=headers,
                ) as resp:
                    assert resp.status_code == 200
                    resumed = _collect_sse_json_events(resp, expected=2)

                assert [event["seq"] for event in resumed] == [2, 3]
                assert [event["type"] for event in resumed] == ["message.delta", "run.completed"]
    finally:
        anyio.run(_drop_database, admin_dsn, database)
