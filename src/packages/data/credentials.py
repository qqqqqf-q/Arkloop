from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass
from datetime import datetime
import uuid

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql
from sqlalchemy.ext.asyncio import AsyncSession

_metadata = sa.MetaData()

_user_credentials = sa.Table(
    "user_credentials",
    _metadata,
    sa.Column(
        "id",
        postgresql.UUID(as_uuid=True),
        primary_key=True,
        server_default=sa.text("gen_random_uuid()"),
    ),
    sa.Column(
        "user_id",
        postgresql.UUID(as_uuid=True),
        sa.ForeignKey("users.id", ondelete="CASCADE"),
        nullable=False,
    ),
    sa.Column("login", sa.Text(), nullable=False),
    sa.Column("password_hash", sa.Text(), nullable=False),
    sa.Column(
        "created_at",
        sa.TIMESTAMP(timezone=True),
        nullable=False,
        server_default=sa.text("now()"),
    ),
    sa.UniqueConstraint("user_id", name="uq_user_credentials_user_id"),
    sa.UniqueConstraint("login", name="uq_user_credentials_login"),
)


@dataclass(frozen=True, slots=True)
class UserCredential:
    id: uuid.UUID
    user_id: uuid.UUID
    login: str
    password_hash: str
    created_at: datetime


class UserCredentialRepository(ABC):
    @abstractmethod
    async def create(self, *, user_id: uuid.UUID, login: str, password_hash: str) -> UserCredential: ...

    @abstractmethod
    async def get_by_login(self, login: str) -> UserCredential | None: ...

    @abstractmethod
    async def get_by_user_id(self, user_id: uuid.UUID) -> UserCredential | None: ...


class SqlAlchemyUserCredentialRepository(UserCredentialRepository):
    def __init__(self, session: AsyncSession) -> None:
        self._session = session

    async def create(self, *, user_id: uuid.UUID, login: str, password_hash: str) -> UserCredential:
        stmt = (
            sa.insert(_user_credentials)
            .values(user_id=user_id, login=login, password_hash=password_hash)
            .returning(
                _user_credentials.c.id,
                _user_credentials.c.user_id,
                _user_credentials.c.login,
                _user_credentials.c.password_hash,
                _user_credentials.c.created_at,
            )
        )
        row = (await self._session.execute(stmt)).mappings().one()
        return UserCredential(**row)

    async def get_by_login(self, login: str) -> UserCredential | None:
        stmt = (
            sa.select(
                _user_credentials.c.id,
                _user_credentials.c.user_id,
                _user_credentials.c.login,
                _user_credentials.c.password_hash,
                _user_credentials.c.created_at,
            )
            .where(_user_credentials.c.login == login)
            .limit(1)
        )
        row = (await self._session.execute(stmt)).mappings().one_or_none()
        return None if row is None else UserCredential(**row)

    async def get_by_user_id(self, user_id: uuid.UUID) -> UserCredential | None:
        stmt = (
            sa.select(
                _user_credentials.c.id,
                _user_credentials.c.user_id,
                _user_credentials.c.login,
                _user_credentials.c.password_hash,
                _user_credentials.c.created_at,
            )
            .where(_user_credentials.c.user_id == user_id)
            .limit(1)
        )
        row = (await self._session.execute(stmt)).mappings().one_or_none()
        return None if row is None else UserCredential(**row)


__all__ = [
    "SqlAlchemyUserCredentialRepository",
    "UserCredential",
    "UserCredentialRepository",
]

