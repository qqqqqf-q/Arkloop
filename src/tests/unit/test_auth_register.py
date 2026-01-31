from __future__ import annotations

from datetime import datetime, timezone
import uuid

import anyio
from fastapi.testclient import TestClient

from packages.auth import BcryptPasswordHasher
from packages.data.credentials import UserCredential
from packages.data.identity import Org, OrgMembership, User
from services.api.main import configure_app
from services.api.trace import TRACE_ID_HEADER
import services.api.v1 as api_v1


class InMemoryUserRepository:
    def __init__(self) -> None:
        self._users: dict[uuid.UUID, User] = {}

    async def create(self, *, display_name: str) -> User:
        user = User(id=uuid.uuid4(), display_name=display_name, created_at=datetime.now(timezone.utc))
        self._users[user.id] = user
        return user

    async def get_by_id(self, user_id: uuid.UUID) -> User | None:
        return self._users.get(user_id)


class InMemoryUserCredentialRepository:
    def __init__(self) -> None:
        self._by_id: dict[uuid.UUID, UserCredential] = {}
        self._by_login: dict[str, UserCredential] = {}
        self._by_user_id: dict[uuid.UUID, UserCredential] = {}

    async def create(self, *, user_id: uuid.UUID, login: str, password_hash: str) -> UserCredential:
        credential = UserCredential(
            id=uuid.uuid4(),
            user_id=user_id,
            login=login,
            password_hash=password_hash,
            created_at=datetime.now(timezone.utc),
        )
        self._by_id[credential.id] = credential
        self._by_login[credential.login] = credential
        self._by_user_id[credential.user_id] = credential
        return credential

    async def get_by_login(self, login: str) -> UserCredential | None:
        return self._by_login.get(login)

    async def get_by_user_id(self, user_id: uuid.UUID) -> UserCredential | None:
        return self._by_user_id.get(user_id)


class InMemoryOrgRepository:
    def __init__(self) -> None:
        self._by_id: dict[uuid.UUID, Org] = {}
        self._by_slug: dict[str, Org] = {}

    async def create(self, *, slug: str, name: str) -> Org:
        org = Org(id=uuid.uuid4(), slug=slug, name=name, created_at=datetime.now(timezone.utc))
        self._by_id[org.id] = org
        self._by_slug[org.slug] = org
        return org

    async def get_by_id(self, org_id: uuid.UUID) -> Org | None:
        return self._by_id.get(org_id)

    async def get_by_slug(self, slug: str) -> Org | None:
        return self._by_slug.get(slug)


class InMemoryOrgMembershipRepository:
    def __init__(self) -> None:
        self._memberships: list[OrgMembership] = []

    async def create(self, *, org_id: uuid.UUID, user_id: uuid.UUID, role: str = "member") -> OrgMembership:
        membership = OrgMembership(
            id=uuid.uuid4(),
            org_id=org_id,
            user_id=user_id,
            role=role,
            created_at=datetime.now(timezone.utc),
        )
        self._memberships.append(membership)
        return membership

    async def get_by_org_and_user(self, *, org_id: uuid.UUID, user_id: uuid.UUID) -> OrgMembership | None:
        for m in self._memberships:
            if m.org_id == org_id and m.user_id == user_id:
                return m
        return None

    async def get_default_for_user(self, *, user_id: uuid.UUID) -> OrgMembership | None:
        for m in self._memberships:
            if m.user_id == user_id:
                return m
        return None


class FakeAsyncSession:
    """模拟 AsyncSession，仅用于 commit 调用"""
    async def commit(self) -> None:
        pass

    async def rollback(self) -> None:
        pass


def test_register_creates_user_and_returns_token(monkeypatch) -> None:
    monkeypatch.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")

    app = configure_app()

    user_repo = InMemoryUserRepository()
    credential_repo = InMemoryUserCredentialRepository()
    org_repo = InMemoryOrgRepository()
    org_membership_repo = InMemoryOrgMembershipRepository()
    fake_session = FakeAsyncSession()

    async def _override_user_repo():
        return user_repo

    async def _override_credential_repo():
        return credential_repo

    async def _override_org_repo():
        return org_repo

    async def _override_org_membership_repo():
        return org_membership_repo

    async def _override_session():
        return fake_session

    app.dependency_overrides[api_v1._get_user_repo] = _override_user_repo
    app.dependency_overrides[api_v1._get_credential_repo] = _override_credential_repo
    app.dependency_overrides[api_v1._get_org_repo] = _override_org_repo
    app.dependency_overrides[api_v1._get_org_membership_repo] = _override_org_membership_repo
    app.dependency_overrides[api_v1.get_db_session] = _override_session

    client = TestClient(app)

    # 注册新用户
    register_response = client.post(
        "/v1/auth/register",
        json={
            "login": "newuser",
            "password": "password123",
            "display_name": "New User",
        },
    )
    assert register_response.status_code == 201, register_response.json()
    register_payload = register_response.json()
    assert register_payload["token_type"] == "bearer"
    assert register_payload["access_token"]
    assert register_payload["user_id"]

    # 使用返回的 token 访问 /me
    me_response = client.get(
        "/v1/me",
        headers={"Authorization": f"Bearer {register_payload['access_token']}"},
    )
    assert me_response.status_code == 200
    me_payload = me_response.json()
    assert me_payload["id"] == register_payload["user_id"]
    assert me_payload["display_name"] == "New User"


def test_register_rejects_duplicate_login(monkeypatch) -> None:
    monkeypatch.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")

    app = configure_app()

    user_repo = InMemoryUserRepository()
    credential_repo = InMemoryUserCredentialRepository()
    org_repo = InMemoryOrgRepository()
    org_membership_repo = InMemoryOrgMembershipRepository()
    fake_session = FakeAsyncSession()

    # 预先创建一个用户
    hasher = BcryptPasswordHasher()

    async def _seed():
        user = await user_repo.create(display_name="Existing")
        await credential_repo.create(
            user_id=user.id,
            login="existing",
            password_hash=hasher.hash_password("password"),
        )

    anyio.run(_seed)

    async def _override_user_repo():
        return user_repo

    async def _override_credential_repo():
        return credential_repo

    async def _override_org_repo():
        return org_repo

    async def _override_org_membership_repo():
        return org_membership_repo

    async def _override_session():
        return fake_session

    app.dependency_overrides[api_v1._get_user_repo] = _override_user_repo
    app.dependency_overrides[api_v1._get_credential_repo] = _override_credential_repo
    app.dependency_overrides[api_v1._get_org_repo] = _override_org_repo
    app.dependency_overrides[api_v1._get_org_membership_repo] = _override_org_membership_repo
    app.dependency_overrides[api_v1.get_db_session] = _override_session

    client = TestClient(app)

    # 尝试使用已存在的 login 注册
    register_response = client.post(
        "/v1/auth/register",
        json={
            "login": "existing",
            "password": "password123",
            "display_name": "Another User",
        },
    )
    assert register_response.status_code == 409
    payload = register_response.json()
    assert payload["code"] == "auth.login_exists"


def test_register_rejects_short_password(monkeypatch) -> None:
    monkeypatch.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")

    app = configure_app()
    client = TestClient(app)

    # 密码太短（少于8位）
    register_response = client.post(
        "/v1/auth/register",
        json={
            "login": "newuser",
            "password": "short",
            "display_name": "New User",
        },
    )
    assert register_response.status_code == 422  # Validation error
