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
from packages.llm_gateway import (
    LlmGatewayRequest,
    LlmStreamMessageDelta,
    LlmStreamRunCompleted,
    LlmStreamToolCall,
)
from services.api.main import configure_app
from services.api.provider_routed_runner import EnvProviderGatewayFactory
from services.api.trace import TRACE_ID_HEADER
from services.worker import Worker

pytestmark = pytest.mark.integration


class _P52ScriptedGateway:
    def __init__(self) -> None:
        self.requests: list[LlmGatewayRequest] = []

    async def stream(self, *, request: LlmGatewayRequest):
        self.requests.append(request)
        if len(self.requests) == 1:
            yield LlmStreamToolCall(
                tool_call_id="p52_call_1",
                tool_name="echo",
                arguments_json={"text": "hello"},
            )
            yield LlmStreamRunCompleted()
            return

        yield LlmStreamMessageDelta(content_delta="tool loop done", role="assistant")
        yield LlmStreamRunCompleted()


class _P54ScriptedGateway:
    def __init__(self) -> None:
        self.requests: list[LlmGatewayRequest] = []

    async def stream(self, *, request: LlmGatewayRequest):
        self.requests.append(request)
        if len(self.requests) == 1:
            assert any(tool.name == "echo" for tool in request.tools)
            yield LlmStreamToolCall(
                tool_call_id="p54_echo_1",
                tool_name="echo",
                arguments_json={"text": "hello"},
            )
            yield LlmStreamRunCompleted()
            return

        tool_messages = [item for item in request.messages if item.role == "tool"]
        assert tool_messages
        payload = json.loads(tool_messages[-1].content[0].text)
        assert payload["tool_call_id"] == "p54_echo_1"
        assert payload["tool_name"] == "echo"
        assert payload["result"]["text"] == "hello"

        yield LlmStreamMessageDelta(content_delta="echo ok", role="assistant")
        yield LlmStreamRunCompleted()


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


async def _seed_org_with_users(
    sqlalchemy_url: str,
    users: list[tuple[str, str, str]],
) -> None:
    database = Database.from_config(DatabaseConfig(url=sqlalchemy_url))
    try:
        async with database.sessionmaker() as session:
            org_repo = SqlAlchemyOrgRepository(session)
            user_repo = SqlAlchemyUserRepository(session)
            membership_repo = SqlAlchemyOrgMembershipRepository(session)
            credential_repo = SqlAlchemyUserCredentialRepository(session)

            slug = f"org_{uuid.uuid4().hex}"
            org = await org_repo.create(slug=slug, name=f"Org {slug}")

            hasher = BcryptPasswordHasher()
            for login, password, display_name in users:
                user = await user_repo.create(display_name=display_name)
                await credential_repo.create(
                    user_id=user.id,
                    login=login,
                    password_hash=hasher.hash_password(password),
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


async def _wait_for_stub_events(
    sqlalchemy_url: str,
    run_id: uuid.UUID,
    *,
    min_deltas: int = 2,
    timeout_seconds: float = 5.0,
) -> list[tuple[int, str]]:
    database = Database.from_config(DatabaseConfig(url=sqlalchemy_url))
    try:
        deadline = time.monotonic() + timeout_seconds
        while True:
            async with database.sessionmaker() as session:
                repo = SqlAlchemyRunEventRepository(session)
                events = await repo.list_events(run_id=run_id, after_seq=0)
            pairs = [(event.seq, event.type) for event in events]
            delta_count = sum(1 for _seq, typ in pairs if typ == "message.delta")
            if pairs and pairs[-1][1] == "run.completed" and delta_count >= min_deltas:
                return pairs
            if time.monotonic() > deadline:
                raise AssertionError("等待 stub agent 事件超时")
            await anyio.sleep(0.02)
    finally:
        await database.dispose()


async def _wait_for_min_deltas(
    sqlalchemy_url: str,
    run_id: uuid.UUID,
    min_deltas: int,
    timeout_seconds: float = 5.0,
) -> list:
    database = Database.from_config(DatabaseConfig(url=sqlalchemy_url))
    try:
        deadline = time.monotonic() + timeout_seconds
        while True:
            async with database.sessionmaker() as session:
                repo = SqlAlchemyRunEventRepository(session)
                events = await repo.list_events(run_id=run_id, after_seq=0)
            delta_count = sum(1 for item in events if item.type == "message.delta")
            if delta_count >= min_deltas:
                return events
            if time.monotonic() > deadline:
                raise AssertionError("等待 message.delta 超时")
            await anyio.sleep(0.02)
    finally:
        await database.dispose()


async def _wait_for_final_event(
    sqlalchemy_url: str,
    run_id: uuid.UUID,
    final_type: str,
    timeout_seconds: float = 5.0,
) -> list:
    database = Database.from_config(DatabaseConfig(url=sqlalchemy_url))
    try:
        deadline = time.monotonic() + timeout_seconds
        while True:
            async with database.sessionmaker() as session:
                repo = SqlAlchemyRunEventRepository(session)
                events = await repo.list_events(run_id=run_id, after_seq=0)
            if events and events[-1].type == final_type:
                return events
            if time.monotonic() > deadline:
                raise AssertionError(f"等待 {final_type} 超时")
            await anyio.sleep(0.02)
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
                assert events[0] == (1, "run.started")
    finally:
        anyio.run(_drop_database, admin_dsn, database)


def test_get_run_returns_status_and_enforces_policy(monkeypatch) -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    repo_root = _repo_root()
    alembic_cfg = Config(str(repo_root / "alembic.ini"))

    database = f"arkloop_runs_get_{uuid.uuid4().hex}"
    sqlalchemy_url = config.url
    admin_dsn = _replace_database(_to_asyncpg_dsn(sqlalchemy_url), "postgres")
    test_sqlalchemy_url = _replace_database(sqlalchemy_url, database)

    anyio.run(_create_database, admin_dsn, database)
    try:
        with monkeypatch.context() as m:
            m.setenv("DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_COUNT", "2")
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_INTERVAL_SECONDS", "0.01")
            command.upgrade(alembic_cfg, "head")

            alice_login = "alice"
            alice_password = "pwdpwdpwd"
            bob_login = "bob"
            bob_password = "pwdpwdpwd"
            anyio.run(
                _seed_org_with_users,
                test_sqlalchemy_url,
                [
                    (alice_login, alice_password, "Alice"),
                    (bob_login, bob_password, "Bob"),
                ],
            )

            mallory_login = "mallory"
            mallory_password = "pwdpwdpwd"
            anyio.run(_seed_auth, test_sqlalchemy_url, mallory_login, mallory_password)

            app = configure_app()
            with TestClient(app) as client:
                alice_auth = client.post(
                    "/v1/auth/login",
                    json={"login": alice_login, "password": alice_password},
                )
                assert alice_auth.status_code == 200
                alice_token = alice_auth.json()["access_token"]
                alice_headers = {"Authorization": f"Bearer {alice_token}"}

                bob_auth = client.post(
                    "/v1/auth/login",
                    json={"login": bob_login, "password": bob_password},
                )
                assert bob_auth.status_code == 200
                bob_token = bob_auth.json()["access_token"]
                bob_headers = {"Authorization": f"Bearer {bob_token}"}

                mallory_auth = client.post(
                    "/v1/auth/login",
                    json={"login": mallory_login, "password": mallory_password},
                )
                assert mallory_auth.status_code == 200
                mallory_token = mallory_auth.json()["access_token"]
                mallory_headers = {"Authorization": f"Bearer {mallory_token}"}

                thread_resp = client.post("/v1/threads", json={"title": "t"}, headers=alice_headers)
                assert thread_resp.status_code == 201
                thread_id = thread_resp.json()["id"]

                run_resp = client.post(f"/v1/threads/{thread_id}/runs", headers=alice_headers)
                assert run_resp.status_code == 201
                run_id = uuid.UUID(run_resp.json()["run_id"])

                anyio.run(_wait_for_stub_events, test_sqlalchemy_url, run_id)

                get_resp = client.get(f"/v1/runs/{run_id}", headers=alice_headers)
                assert get_resp.status_code == 200
                assert TRACE_ID_HEADER in get_resp.headers
                assert get_resp.headers[TRACE_ID_HEADER]

                body = get_resp.json()
                assert body["trace_id"] == get_resp.headers[TRACE_ID_HEADER]
                assert body["run_id"] == str(run_id)
                assert body["thread_id"] == thread_id
                assert body["status"] == "completed"
                assert body["created_at"]

                owner_denied = client.get(f"/v1/runs/{run_id}", headers=bob_headers)
                assert owner_denied.status_code == 403
                assert TRACE_ID_HEADER in owner_denied.headers
                owner_payload = owner_denied.json()
                assert owner_payload["code"] == "policy.denied"
                assert owner_payload["trace_id"] == owner_denied.headers[TRACE_ID_HEADER]
                assert owner_payload.get("details", {}).get("action") == "runs.get"

                org_denied = client.get(f"/v1/runs/{run_id}", headers=mallory_headers)
                assert org_denied.status_code == 403
                assert TRACE_ID_HEADER in org_denied.headers
                org_payload = org_denied.json()
                assert org_payload["code"] == "policy.denied"
                assert org_payload["trace_id"] == org_denied.headers[TRACE_ID_HEADER]

                not_found_id = uuid.uuid4()
                missing = client.get(f"/v1/runs/{not_found_id}", headers=alice_headers)
                assert missing.status_code == 404
                assert TRACE_ID_HEADER in missing.headers
                missing_payload = missing.json()
                assert missing_payload["code"] == "runs.not_found"
                assert missing_payload["trace_id"] == missing.headers[TRACE_ID_HEADER]
    finally:
        anyio.run(_drop_database, admin_dsn, database)


def test_list_thread_runs_returns_stable_order_and_enforces_policy(monkeypatch) -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    repo_root = _repo_root()
    alembic_cfg = Config(str(repo_root / "alembic.ini"))

    database = f"arkloop_threads_runs_list_{uuid.uuid4().hex}"
    sqlalchemy_url = config.url
    admin_dsn = _replace_database(_to_asyncpg_dsn(sqlalchemy_url), "postgres")
    test_sqlalchemy_url = _replace_database(sqlalchemy_url, database)

    anyio.run(_create_database, admin_dsn, database)
    try:
        with monkeypatch.context() as m:
            m.setenv("DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_COUNT", "2")
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_INTERVAL_SECONDS", "0.01")
            command.upgrade(alembic_cfg, "head")

            alice_login = "alice"
            alice_password = "pwdpwdpwd"
            bob_login = "bob"
            bob_password = "pwdpwdpwd"
            anyio.run(
                _seed_org_with_users,
                test_sqlalchemy_url,
                [
                    (alice_login, alice_password, "Alice"),
                    (bob_login, bob_password, "Bob"),
                ],
            )

            mallory_login = "mallory"
            mallory_password = "pwdpwdpwd"
            anyio.run(_seed_auth, test_sqlalchemy_url, mallory_login, mallory_password)

            app = configure_app()
            with TestClient(app) as client:
                alice_auth = client.post(
                    "/v1/auth/login",
                    json={"login": alice_login, "password": alice_password},
                )
                assert alice_auth.status_code == 200
                alice_token = alice_auth.json()["access_token"]
                alice_headers = {"Authorization": f"Bearer {alice_token}"}

                bob_auth = client.post(
                    "/v1/auth/login",
                    json={"login": bob_login, "password": bob_password},
                )
                assert bob_auth.status_code == 200
                bob_token = bob_auth.json()["access_token"]
                bob_headers = {"Authorization": f"Bearer {bob_token}"}

                mallory_auth = client.post(
                    "/v1/auth/login",
                    json={"login": mallory_login, "password": mallory_password},
                )
                assert mallory_auth.status_code == 200
                mallory_token = mallory_auth.json()["access_token"]
                mallory_headers = {"Authorization": f"Bearer {mallory_token}"}

                thread1_resp = client.post("/v1/threads", json={"title": "t1"}, headers=alice_headers)
                assert thread1_resp.status_code == 201
                thread1_id = thread1_resp.json()["id"]

                thread2_resp = client.post("/v1/threads", json={"title": "t2"}, headers=alice_headers)
                assert thread2_resp.status_code == 201
                thread2_id = thread2_resp.json()["id"]

                run1_resp = client.post(f"/v1/threads/{thread1_id}/runs", headers=alice_headers)
                assert run1_resp.status_code == 201
                run1_id = uuid.UUID(run1_resp.json()["run_id"])

                time.sleep(0.01)
                run2_resp = client.post(f"/v1/threads/{thread1_id}/runs", headers=alice_headers)
                assert run2_resp.status_code == 201
                run2_id = uuid.UUID(run2_resp.json()["run_id"])

                time.sleep(0.01)
                run3_resp = client.post(f"/v1/threads/{thread2_id}/runs", headers=alice_headers)
                assert run3_resp.status_code == 201
                run3_id = uuid.UUID(run3_resp.json()["run_id"])

                anyio.run(_wait_for_stub_events, test_sqlalchemy_url, run1_id)
                anyio.run(_wait_for_stub_events, test_sqlalchemy_url, run2_id)
                anyio.run(_wait_for_stub_events, test_sqlalchemy_url, run3_id)

                list_resp = client.get(f"/v1/threads/{thread1_id}/runs?limit=50", headers=alice_headers)
                assert list_resp.status_code == 200
                items = list_resp.json()
                assert [item["run_id"] for item in items] == [str(run2_id), str(run1_id)]
                assert [item["status"] for item in items] == ["completed", "completed"]
                assert all(item.get("created_at") for item in items)

                limit_resp = client.get(f"/v1/threads/{thread1_id}/runs?limit=1", headers=alice_headers)
                assert limit_resp.status_code == 200
                assert [item["run_id"] for item in limit_resp.json()] == [str(run2_id)]

                owner_denied = client.get(f"/v1/threads/{thread1_id}/runs?limit=50", headers=bob_headers)
                assert owner_denied.status_code == 403
                assert TRACE_ID_HEADER in owner_denied.headers
                owner_payload = owner_denied.json()
                assert owner_payload["code"] == "policy.denied"
                assert owner_payload["trace_id"] == owner_denied.headers[TRACE_ID_HEADER]
                assert owner_payload.get("details", {}).get("action") == "runs.list"

                org_denied = client.get(f"/v1/threads/{thread1_id}/runs?limit=50", headers=mallory_headers)
                assert org_denied.status_code == 403
                assert TRACE_ID_HEADER in org_denied.headers
                org_payload = org_denied.json()
                assert org_payload["code"] == "policy.denied"
                assert org_payload["trace_id"] == org_denied.headers[TRACE_ID_HEADER]

                missing_thread = uuid.uuid4()
                missing = client.get(f"/v1/threads/{missing_thread}/runs?limit=50", headers=alice_headers)
                assert missing.status_code == 404
                assert TRACE_ID_HEADER in missing.headers
                missing_payload = missing.json()
                assert missing_payload["code"] == "threads.not_found"
                assert missing_payload["trace_id"] == missing.headers[TRACE_ID_HEADER]
    finally:
        anyio.run(_drop_database, admin_dsn, database)


def test_cancel_run_writes_cancel_events_and_stops_deltas(monkeypatch) -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    repo_root = _repo_root()
    alembic_cfg = Config(str(repo_root / "alembic.ini"))

    database = f"arkloop_runs_cancel_{uuid.uuid4().hex}"
    sqlalchemy_url = config.url
    admin_dsn = _replace_database(_to_asyncpg_dsn(sqlalchemy_url), "postgres")
    test_sqlalchemy_url = _replace_database(sqlalchemy_url, database)

    anyio.run(_create_database, admin_dsn, database)
    try:
        with monkeypatch.context() as m:
            m.setenv("DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_COUNT", "50")
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_INTERVAL_SECONDS", "0.05")
            command.upgrade(alembic_cfg, "head")

            alice_login = "alice"
            alice_password = "pwdpwdpwd"
            bob_login = "bob"
            bob_password = "pwdpwdpwd"
            anyio.run(
                _seed_org_with_users,
                test_sqlalchemy_url,
                [
                    (alice_login, alice_password, "Alice"),
                    (bob_login, bob_password, "Bob"),
                ],
            )

            app = configure_app()
            with TestClient(app) as client:
                alice_auth = client.post(
                    "/v1/auth/login",
                    json={"login": alice_login, "password": alice_password},
                )
                assert alice_auth.status_code == 200
                alice_token = alice_auth.json()["access_token"]
                alice_headers = {"Authorization": f"Bearer {alice_token}"}

                bob_auth = client.post(
                    "/v1/auth/login",
                    json={"login": bob_login, "password": bob_password},
                )
                assert bob_auth.status_code == 200
                bob_token = bob_auth.json()["access_token"]
                bob_headers = {"Authorization": f"Bearer {bob_token}"}

                thread_resp = client.post("/v1/threads", json={"title": "t"}, headers=alice_headers)
                assert thread_resp.status_code == 201
                thread_id = thread_resp.json()["id"]

                run_resp = client.post(f"/v1/threads/{thread_id}/runs", headers=alice_headers)
                assert run_resp.status_code == 201
                run_id = uuid.UUID(run_resp.json()["run_id"])

                anyio.run(_wait_for_min_deltas, test_sqlalchemy_url, run_id, 2)

                denied = client.post(f"/v1/runs/{run_id}:cancel", headers=bob_headers)
                assert denied.status_code == 403
                denied_payload = denied.json()
                assert denied_payload["code"] == "policy.denied"
                assert denied_payload["trace_id"] == denied.headers[TRACE_ID_HEADER]

                cancel1 = client.post(f"/v1/runs/{run_id}:cancel", headers=alice_headers)
                assert cancel1.status_code == 200
                assert cancel1.json()["ok"] is True
                assert TRACE_ID_HEADER in cancel1.headers
                assert cancel1.headers[TRACE_ID_HEADER]

                cancel2 = client.post(f"/v1/runs/{run_id}:cancel", headers=alice_headers)
                assert cancel2.status_code == 200
                assert cancel2.json()["ok"] is True

                events = anyio.run(_wait_for_final_event, test_sqlalchemy_url, run_id, "run.cancelled")
                types = [event.type for event in events]
                assert types[-1] == "run.cancelled"
                assert types.count("run.cancel_requested") == 1
                assert types.count("run.cancelled") == 1
                assert "run.completed" not in types

                cancel_requested_seq = next(
                    event.seq for event in events if event.type == "run.cancel_requested"
                )
                delta_seqs = [event.seq for event in events if event.type == "message.delta"]
                assert delta_seqs
                assert all(seq < cancel_requested_seq for seq in delta_seqs)
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
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_COUNT", "3")
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_INTERVAL_SECONDS", "0.01")
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

                expected = anyio.run(_wait_for_stub_events, test_sqlalchemy_url, run_id)

                unauth = client.get(f"/v1/runs/{run_id}/events?after_seq=0&follow=0")
                assert unauth.status_code == 401
                assert TRACE_ID_HEADER in unauth.headers
                payload = unauth.json()
                assert payload["code"] == "auth.missing_token"
                assert payload["trace_id"] == unauth.headers[TRACE_ID_HEADER]

                with client.stream(
                    "GET",
                    f"/v1/runs/{run_id}/events?after_seq=0&follow=0",
                    headers=headers,
                ) as resp:
                    assert resp.status_code == 200
                    assert resp.headers["content-type"].startswith("text/event-stream")
                    assert TRACE_ID_HEADER in resp.headers
                    assert resp.headers[TRACE_ID_HEADER]
                    events = _collect_sse_json_events(resp, expected=len(expected))

                assert [(event["seq"], event["type"]) for event in events] == expected
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
                    resumed = _collect_sse_json_events(resp, expected=len(expected) - 1)

                assert [(event["seq"], event["type"]) for event in resumed] == expected[1:]
    finally:
        anyio.run(_drop_database, admin_dsn, database)


def test_p52_tool_executor_pipeline_events_and_sse_resume(monkeypatch) -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    repo_root = _repo_root()
    alembic_cfg = Config(str(repo_root / "alembic.ini"))

    database = f"arkloop_runs_p52_{uuid.uuid4().hex}"
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
            scripted_gateway = _P52ScriptedGateway()

            def _patched_create(self, *, credential):
                _ = (self, credential)
                return scripted_gateway

            m.setattr(EnvProviderGatewayFactory, "create", _patched_create)
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

                events = anyio.run(_wait_for_final_event, test_sqlalchemy_url, run_id, "run.completed")
                types = [item.type for item in events]
                assert types == [
                    "run.started",
                    "run.route.selected",
                    "tool.call",
                    "policy.denied",
                    "tool.result",
                    "message.delta",
                    "run.completed",
                ]

                tool_call = next(item for item in events if item.type == "tool.call")
                denied = next(item for item in events if item.type == "policy.denied")
                tool_result = next(item for item in events if item.type == "tool.result")
                assert tool_call.data_json["tool_call_id"] == "p52_call_1"
                assert denied.data_json["tool_call_id"] == "p52_call_1"
                assert tool_result.data_json["tool_call_id"] == "p52_call_1"
                assert tool_result.error_class == "policy.denied"

                assert len(scripted_gateway.requests) == 2
                second_turn_tool_messages = [
                    item for item in scripted_gateway.requests[1].messages if item.role == "tool"
                ]
                assert second_turn_tool_messages
                last_tool_payload = json.loads(second_turn_tool_messages[-1].content[0].text)
                assert last_tool_payload["tool_call_id"] == "p52_call_1"
                assert last_tool_payload["error"]["error_class"] == "policy.denied"

                with client.stream(
                    "GET",
                    f"/v1/runs/{run_id}/events?after_seq=0&follow=0",
                    headers=headers,
                ) as resp:
                    assert resp.status_code == 200
                    all_sse_events = _collect_sse_json_events(resp, expected=len(events))

                assert [item["type"] for item in all_sse_events] == types
                tool_result_seq = next(
                    item["seq"] for item in all_sse_events if item["type"] == "tool.result"
                )
                expected_tail = [item for item in all_sse_events if item["seq"] > tool_result_seq]
                with client.stream(
                    "GET",
                    f"/v1/runs/{run_id}/events?after_seq={tool_result_seq}&follow=0",
                    headers=headers,
                ) as resp:
                    assert resp.status_code == 200
                    resumed = _collect_sse_json_events(resp, expected=len(expected_tail))

                assert [(item["seq"], item["type"]) for item in resumed] == [
                    (item["seq"], item["type"]) for item in expected_tail
                ]
    finally:
        anyio.run(_drop_database, admin_dsn, database)


def test_p54_builtin_echo_tool_executes_and_sse_resume(monkeypatch) -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    repo_root = _repo_root()
    alembic_cfg = Config(str(repo_root / "alembic.ini"))

    database = f"arkloop_runs_p54_{uuid.uuid4().hex}"
    sqlalchemy_url = config.url
    admin_dsn = _replace_database(_to_asyncpg_dsn(sqlalchemy_url), "postgres")
    test_sqlalchemy_url = _replace_database(sqlalchemy_url, database)

    anyio.run(_create_database, admin_dsn, database)
    try:
        with monkeypatch.context() as m:
            m.setenv("DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")
            m.setenv("ARKLOOP_LLM_DEBUG_EVENTS", "0")
            m.setenv("ARKLOOP_SSE_POLL_SECONDS", "0.01")
            m.setenv("ARKLOOP_SSE_HEARTBEAT_SECONDS", "0.05")
            m.setenv("ARKLOOP_TOOL_ALLOWLIST", "echo")
            scripted_gateway = _P54ScriptedGateway()

            def _patched_create(self, *, credential):
                _ = (self, credential)
                return scripted_gateway

            m.setattr(EnvProviderGatewayFactory, "create", _patched_create)
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

                events = anyio.run(_wait_for_final_event, test_sqlalchemy_url, run_id, "run.completed")
                types = [item.type for item in events]
                assert types == [
                    "run.started",
                    "run.route.selected",
                    "tool.call",
                    "tool.result",
                    "message.delta",
                    "run.completed",
                ]

                tool_call = next(item for item in events if item.type == "tool.call")
                tool_result = next(item for item in events if item.type == "tool.result")
                assert tool_call.tool_name == "echo"
                assert tool_call.data_json["tool_call_id"] == "p54_echo_1"
                assert tool_result.tool_name == "echo"
                assert tool_result.data_json["tool_call_id"] == "p54_echo_1"
                assert tool_result.data_json["result"] == {"text": "hello"}
                assert tool_result.error_class is None

                assert len(scripted_gateway.requests) == 2

                with client.stream(
                    "GET",
                    f"/v1/runs/{run_id}/events?after_seq=0&follow=0",
                    headers=headers,
                ) as resp:
                    assert resp.status_code == 200
                    all_sse_events = _collect_sse_json_events(resp, expected=len(events))

                assert [item["type"] for item in all_sse_events] == types
                tool_result_seq = next(
                    item["seq"] for item in all_sse_events if item["type"] == "tool.result"
                )
                expected_tail = [item for item in all_sse_events if item["seq"] > tool_result_seq]
                with client.stream(
                    "GET",
                    f"/v1/runs/{run_id}/events?after_seq={tool_result_seq}&follow=0",
                    headers=headers,
                ) as resp:
                    assert resp.status_code == 200
                    resumed = _collect_sse_json_events(resp, expected=len(expected_tail))

                assert [(item["seq"], item["type"]) for item in resumed] == [
                    (item["seq"], item["type"]) for item in expected_tail
                ]
    finally:
        anyio.run(_drop_database, admin_dsn, database)


def test_run_completed_materializes_assistant_message(monkeypatch) -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    repo_root = _repo_root()
    alembic_cfg = Config(str(repo_root / "alembic.ini"))

    database = f"arkloop_runs_materialize_messages_{uuid.uuid4().hex}"
    sqlalchemy_url = config.url
    admin_dsn = _replace_database(_to_asyncpg_dsn(sqlalchemy_url), "postgres")
    test_sqlalchemy_url = _replace_database(sqlalchemy_url, database)

    anyio.run(_create_database, admin_dsn, database)
    try:
        with monkeypatch.context() as m:
            m.setenv("DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_COUNT", "3")
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_INTERVAL_SECONDS", "0.01")
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

                message_resp = client.post(
                    f"/v1/threads/{thread_id}/messages",
                    json={"content": "hello"},
                    headers=headers,
                )
                assert message_resp.status_code == 201

                run_resp = client.post(f"/v1/threads/{thread_id}/runs", headers=headers)
                assert run_resp.status_code == 201
                run_id = uuid.UUID(run_resp.json()["run_id"])

                anyio.run(_wait_for_stub_events, test_sqlalchemy_url, run_id)

                list_resp = client.get(f"/v1/threads/{thread_id}/messages", headers=headers)
                assert list_resp.status_code == 200
                messages = list_resp.json()

                user_messages = [item for item in messages if item.get("role") == "user"]
                assert any(item.get("content") == "hello" for item in user_messages)

                assistant_messages = [item for item in messages if item.get("role") == "assistant"]
                assert len(assistant_messages) == 1
                assert assistant_messages[0]["content"] == "stub delta 1stub delta 2stub delta 3"
    finally:
        anyio.run(_drop_database, admin_dsn, database)


def test_run_records_llm_debug_events_when_enabled(monkeypatch) -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    repo_root = _repo_root()
    alembic_cfg = Config(str(repo_root / "alembic.ini"))

    database = f"arkloop_runs_llm_debug_{uuid.uuid4().hex}"
    sqlalchemy_url = config.url
    admin_dsn = _replace_database(_to_asyncpg_dsn(sqlalchemy_url), "postgres")
    test_sqlalchemy_url = _replace_database(sqlalchemy_url, database)

    anyio.run(_create_database, admin_dsn, database)
    try:
        with monkeypatch.context() as m:
            m.setenv("DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")
            m.setenv("ARKLOOP_LLM_DEBUG_EVENTS", "1")
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_COUNT", "2")
            m.setenv("ARKLOOP_STUB_AGENT_DELTA_INTERVAL_SECONDS", "0.01")
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

                message_resp = client.post(
                    f"/v1/threads/{thread_id}/messages",
                    json={"content": "hello"},
                    headers=headers,
                )
                assert message_resp.status_code == 201

                run_resp = client.post(f"/v1/threads/{thread_id}/runs", headers=headers)
                assert run_resp.status_code == 201
                run_id = uuid.UUID(run_resp.json()["run_id"])

                events = anyio.run(_wait_for_final_event, test_sqlalchemy_url, run_id, "run.completed")

                types = [event.type for event in events]
                assert "llm.request" in types
                assert "llm.response.chunk" in types

                request_events = [event for event in events if event.type == "llm.request"]
                assert len(request_events) == 1

                payload = request_events[0].data_json.get("payload")
                assert isinstance(payload, dict)
                messages = payload.get("messages")
                assert isinstance(messages, list)
                assert any(
                    isinstance(item, dict)
                    and item.get("role") == "user"
                    and isinstance(item.get("content"), list)
                    and any(
                        isinstance(part, dict) and part.get("type") == "text" and part.get("text") == "hello"
                        for part in item.get("content")
                    )
                    for item in messages
                )

                assert isinstance(request_events[0].data_json.get("trace_id"), str)
    finally:
        anyio.run(_drop_database, admin_dsn, database)


def test_worker_job_restores_trace_id_and_is_replayable_via_sse(monkeypatch) -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    repo_root = _repo_root()
    alembic_cfg = Config(str(repo_root / "alembic.ini"))

    database = f"arkloop_worker_job_{uuid.uuid4().hex}"
    sqlalchemy_url = config.url
    admin_dsn = _replace_database(_to_asyncpg_dsn(sqlalchemy_url), "postgres")
    test_sqlalchemy_url = _replace_database(sqlalchemy_url, database)

    anyio.run(_create_database, admin_dsn, database)
    try:
        with monkeypatch.context() as m:
            m.setenv("DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")
            m.setenv("ARKLOOP_STUB_AGENT_ENABLED", "0")
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
                org_id = thread_resp.json()["org_id"]

                run_resp = client.post(f"/v1/threads/{thread_id}/runs", headers=headers)
                assert run_resp.status_code == 201
                run_payload = run_resp.json()
                run_id = uuid.UUID(run_payload["run_id"])
                expected_trace_id = run_payload["trace_id"]

                job_payload = {
                    "job_id": str(uuid.uuid4()),
                    "type": "worker.ping",
                    "trace_id": expected_trace_id,
                    "org_id": org_id,
                    "run_id": str(run_id),
                    "payload": {"kind": "test"},
                }

                async def _run_worker_job() -> None:
                    database = Database.from_config(DatabaseConfig(url=test_sqlalchemy_url))
                    try:
                        worker = Worker(database=database)
                        await worker.handle_job(job_payload)
                    finally:
                        await database.dispose()

                anyio.run(_run_worker_job)

                with client.stream(
                    "GET",
                    f"/v1/runs/{run_id}/events?after_seq=0&follow=0",
                    headers=headers,
                ) as resp:
                    assert resp.status_code == 200
                    events = _collect_sse_json_events(resp, expected=2)

                assert [(event["seq"], event["type"]) for event in events] == [
                    (1, "run.started"),
                    (2, "worker.job.received"),
                ]
                assert events[1]["data"]["trace_id"] == expected_trace_id

                with client.stream(
                    "GET",
                    f"/v1/runs/{run_id}/events?after_seq=1&follow=0",
                    headers=headers,
                ) as resp:
                    assert resp.status_code == 200
                    resumed = _collect_sse_json_events(resp, expected=1)

                assert [(event["seq"], event["type"]) for event in resumed] == [(2, "worker.job.received")]
                assert resumed[0]["data"]["trace_id"] == expected_trace_id
    finally:
        anyio.run(_drop_database, admin_dsn, database)


def test_run_route_denies_byok_when_org_not_enabled(monkeypatch) -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    repo_root = _repo_root()
    alembic_cfg = Config(str(repo_root / "alembic.ini"))

    database = f"arkloop_runs_route_byok_{uuid.uuid4().hex}"
    sqlalchemy_url = config.url
    admin_dsn = _replace_database(_to_asyncpg_dsn(sqlalchemy_url), "postgres")
    test_sqlalchemy_url = _replace_database(sqlalchemy_url, database)

    routing_json = json.dumps(
        {
            "default_route_id": "stub",
            "credentials": [
                {"id": "stub_cred", "scope": "platform", "provider_kind": "stub"},
                {
                    "id": "org_byok_openai",
                    "scope": "org",
                    "provider_kind": "openai",
                    "api_key_env": "ORG_OPENAI_API_KEY",
                    "base_url": "https://example.test/v1",
                    "openai_api_mode": "chat_completions",
                },
            ],
            "routes": [
                {"id": "stub", "model": "stub", "credential_id": "stub_cred"},
                {"id": "byok", "model": "gpt-test", "credential_id": "org_byok_openai"},
            ],
        },
        ensure_ascii=False,
        separators=(",", ":"),
    )

    anyio.run(_create_database, admin_dsn, database)
    try:
        with monkeypatch.context() as m:
            m.setenv("DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_DATABASE_URL", test_sqlalchemy_url)
            m.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")
            m.setenv("ARKLOOP_PROVIDER_ROUTING_JSON", routing_json)
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

                run_resp = client.post(
                    f"/v1/threads/{thread_id}/runs",
                    headers=headers,
                    json={"route_id": "byok"},
                )
                assert run_resp.status_code == 201
                run_id = uuid.UUID(run_resp.json()["run_id"])

                events = anyio.run(
                    _wait_for_final_event,
                    test_sqlalchemy_url,
                    run_id,
                    "run.failed",
                )
                assert [event.type for event in events] == ["run.started", "run.failed"]
                failed = events[-1]
                assert failed.error_class == "policy.denied"
                assert failed.data_json["code"] == "policy.byok_disabled"
                assert failed.data_json["error_class"] == "policy.denied"
    finally:
        anyio.run(_drop_database, admin_dsn, database)
