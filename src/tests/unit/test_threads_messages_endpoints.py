from __future__ import annotations

from datetime import datetime, timezone
import json
import uuid

import anyio
from fastapi.testclient import TestClient

from packages.auth import BcryptPasswordHasher
from packages.data.credentials import UserCredential
from packages.data.identity import OrgMembership, User
from packages.data.threads import Message, Thread, ThreadNotFoundError
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
        self._by_login: dict[str, UserCredential] = {}

    async def create(self, *, user_id: uuid.UUID, login: str, password_hash: str) -> UserCredential:
        credential = UserCredential(
            id=uuid.uuid4(),
            user_id=user_id,
            login=login,
            password_hash=password_hash,
            created_at=datetime.now(timezone.utc),
        )
        self._by_login[login] = credential
        return credential

    async def get_by_login(self, login: str) -> UserCredential | None:
        return self._by_login.get(login)

    async def get_by_user_id(self, user_id: uuid.UUID) -> UserCredential | None:
        for credential in self._by_login.values():
            if credential.user_id == user_id:
                return credential
        return None


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
        for membership in self._memberships:
            if membership.org_id == org_id and membership.user_id == user_id:
                return membership
        return None

    async def get_default_for_user(self, *, user_id: uuid.UUID) -> OrgMembership | None:
        for membership in sorted(self._memberships, key=lambda item: item.created_at):
            if membership.user_id == user_id:
                return membership
        return None


class InMemoryThreadRepository:
    def __init__(self) -> None:
        self._threads: dict[uuid.UUID, Thread] = {}

    async def create(
        self,
        *,
        org_id: uuid.UUID,
        created_by_user_id: uuid.UUID | None = None,
        title: str | None = None,
    ) -> Thread:
        thread = Thread(
            id=uuid.uuid4(),
            org_id=org_id,
            created_by_user_id=created_by_user_id,
            title=title,
            created_at=datetime.now(timezone.utc),
        )
        self._threads[thread.id] = thread
        return thread

    async def get_by_id(self, thread_id: uuid.UUID) -> Thread | None:
        return self._threads.get(thread_id)


class InMemoryMessageRepository:
    def __init__(self, thread_repo: InMemoryThreadRepository) -> None:
        self._thread_repo = thread_repo
        self._messages: list[Message] = []

    async def create(
        self,
        *,
        org_id: uuid.UUID,
        thread_id: uuid.UUID,
        role: str,
        content: str,
        created_by_user_id: uuid.UUID | None = None,
    ) -> Message:
        thread = await self._thread_repo.get_by_id(thread_id)
        if thread is None or thread.org_id != org_id:
            raise ThreadNotFoundError(thread_id=thread_id)

        message = Message(
            id=uuid.uuid4(),
            org_id=org_id,
            thread_id=thread_id,
            created_by_user_id=created_by_user_id,
            role=role,
            content=content,
            created_at=datetime.now(timezone.utc),
        )
        self._messages.append(message)
        return message

    async def list_by_thread(
        self,
        *,
        org_id: uuid.UUID,
        thread_id: uuid.UUID,
        limit: int = 200,
    ) -> list[Message]:
        items = [m for m in self._messages if m.org_id == org_id and m.thread_id == thread_id]
        items.sort(key=lambda m: (m.created_at, m.id))
        return items[:limit]


def _assert_policy_error_has_trace_id(response) -> None:
    assert response.status_code == 403
    assert TRACE_ID_HEADER in response.headers
    assert response.headers[TRACE_ID_HEADER]
    payload = response.json()
    assert payload["trace_id"] == response.headers[TRACE_ID_HEADER]
    assert payload["code"].startswith("policy.")


def test_threads_messages_create_list_and_policy_denied(monkeypatch, capsys) -> None:
    monkeypatch.setenv("ARKLOOP_AUTH_JWT_SECRET", "test-secret-should-be-long-enough-32chars")

    app = configure_app()

    user_repo = InMemoryUserRepository()
    credential_repo = InMemoryUserCredentialRepository()
    membership_repo = InMemoryOrgMembershipRepository()
    thread_repo = InMemoryThreadRepository()
    message_repo = InMemoryMessageRepository(thread_repo)

    async def _override_user_repo() -> InMemoryUserRepository:
        return user_repo

    async def _override_credential_repo() -> InMemoryUserCredentialRepository:
        return credential_repo

    async def _override_membership_repo() -> InMemoryOrgMembershipRepository:
        return membership_repo

    async def _override_thread_repo() -> InMemoryThreadRepository:
        return thread_repo

    async def _override_message_repo() -> InMemoryMessageRepository:
        return message_repo

    app.dependency_overrides[api_v1._get_user_repo] = _override_user_repo
    app.dependency_overrides[api_v1._get_credential_repo] = _override_credential_repo
    app.dependency_overrides[api_v1._get_org_membership_repo] = _override_membership_repo
    app.dependency_overrides[api_v1._get_thread_repo] = _override_thread_repo
    app.dependency_overrides[api_v1._get_message_repo] = _override_message_repo

    password = "pwdpwdpwd"
    hasher = BcryptPasswordHasher()

    async def _seed() -> tuple[uuid.UUID, User, User]:
        org_id = uuid.uuid4()

        alice = await user_repo.create(display_name="Alice")
        await credential_repo.create(
            user_id=alice.id,
            login="alice",
            password_hash=hasher.hash_password(password),
        )
        await membership_repo.create(org_id=org_id, user_id=alice.id, role="member")

        bob = await user_repo.create(display_name="Bob")
        await credential_repo.create(
            user_id=bob.id,
            login="bob",
            password_hash=hasher.hash_password(password),
        )
        await membership_repo.create(org_id=org_id, user_id=bob.id, role="member")
        return org_id, alice, bob

    org_id, alice, _bob = anyio.run(_seed)

    client = TestClient(app)

    alice_login = client.post("/v1/auth/login", json={"login": "alice", "password": password})
    assert alice_login.status_code == 200
    alice_token = alice_login.json()["access_token"]

    bob_login = client.post("/v1/auth/login", json={"login": "bob", "password": password})
    assert bob_login.status_code == 200
    bob_token = bob_login.json()["access_token"]

    create_thread_response = client.post(
        "/v1/threads",
        json={"title": "t"},
        headers={"Authorization": f"Bearer {alice_token}"},
    )
    assert create_thread_response.status_code == 201
    thread_payload = create_thread_response.json()
    assert thread_payload["org_id"] == str(org_id)
    assert thread_payload["created_by_user_id"] == str(alice.id)
    thread_id = thread_payload["id"]

    create_message_response = client.post(
        f"/v1/threads/{thread_id}/messages",
        json={"content": "hello"},
        headers={"Authorization": f"Bearer {alice_token}"},
    )
    assert create_message_response.status_code == 201
    message_payload = create_message_response.json()
    assert message_payload["org_id"] == str(org_id)
    assert message_payload["thread_id"] == thread_id
    assert message_payload["created_by_user_id"] == str(alice.id)
    assert message_payload["role"] == "user"
    assert message_payload["content"] == "hello"

    list_messages_response = client.get(
        f"/v1/threads/{thread_id}/messages",
        headers={"Authorization": f"Bearer {alice_token}"},
    )
    assert list_messages_response.status_code == 200
    list_payload = list_messages_response.json()
    assert [item["id"] for item in list_payload] == [message_payload["id"]]

    capsys.readouterr()
    denied_list_response = client.get(
        f"/v1/threads/{thread_id}/messages",
        headers={"Authorization": f"Bearer {bob_token}"},
    )
    _assert_policy_error_has_trace_id(denied_list_response)
    denied_trace_id = denied_list_response.headers[TRACE_ID_HEADER]

    audit_lines = capsys.readouterr().out.strip().splitlines()
    audit_payloads = [json.loads(line) for line in audit_lines if line.strip().startswith("{")]
    denied_audits = [
        item
        for item in audit_payloads
        if item.get("logger") == "arkloop.audit"
        and item.get("trace_id") == denied_trace_id
        and item.get("action") == "messages.list"
        and item.get("deny_reason")
    ]
    assert denied_audits

    denied_create_response = client.post(
        f"/v1/threads/{thread_id}/messages",
        json={"content": "oops"},
        headers={"Authorization": f"Bearer {bob_token}"},
    )
    _assert_policy_error_has_trace_id(denied_create_response)
