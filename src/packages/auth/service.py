from __future__ import annotations

import uuid

from packages.data.credentials import UserCredentialRepository
from packages.data.identity import User, UserRepository

from .password_hasher import PasswordHasher
from .tokens import JwtAccessTokenService


class InvalidCredentialsError(Exception):
    ...


class UserNotFoundError(LookupError):
    def __init__(self, *, user_id: uuid.UUID) -> None:
        super().__init__("用户不存在")
        self.user_id = user_id


class AuthService:
    def __init__(
        self,
        *,
        user_repo: UserRepository,
        credential_repo: UserCredentialRepository,
        password_hasher: PasswordHasher,
        token_service: JwtAccessTokenService,
    ) -> None:
        self._user_repo = user_repo
        self._credential_repo = credential_repo
        self._password_hasher = password_hasher
        self._token_service = token_service

    async def issue_access_token(self, *, login: str, password: str) -> str:
        credential = await self._credential_repo.get_by_login(login)
        if credential is None:
            raise InvalidCredentialsError("invalid_credentials")
        if not self._password_hasher.verify_password(password, credential.password_hash):
            raise InvalidCredentialsError("invalid_credentials")
        return self._token_service.issue(user_id=credential.user_id)

    def verify_access_token(self, *, token: str) -> uuid.UUID:
        return self._token_service.verify(token)

    async def get_user(self, *, user_id: uuid.UUID) -> User:
        user = await self._user_repo.get_by_id(user_id)
        if user is None:
            raise UserNotFoundError(user_id=user_id)
        return user


__all__ = ["AuthService", "InvalidCredentialsError", "UserNotFoundError"]

