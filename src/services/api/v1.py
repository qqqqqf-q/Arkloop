from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
import uuid

from fastapi import APIRouter, Depends, FastAPI, Request
from pydantic import BaseModel, Field
from sqlalchemy.ext.asyncio import AsyncSession

from packages.auth import (
    AuthConfig,
    AuthService,
    BcryptPasswordHasher,
    InvalidCredentialsError,
    JwtAccessTokenService,
    TokenExpiredError,
    TokenInvalidError,
    UserNotFoundError,
)
from packages.data.credentials import SqlAlchemyUserCredentialRepository, UserCredentialRepository
from packages.data.identity import SqlAlchemyUserRepository, User, UserRepository

from .db import get_db_session
from .error_envelope import ApiError

_v1_router = APIRouter(prefix="/v1")


@dataclass(frozen=True, slots=True)
class _InstalledAuth:
    password_hasher: BcryptPasswordHasher
    token_service: JwtAccessTokenService


def install_auth(app: FastAPI, installed: _InstalledAuth) -> None:
    app.state.auth = installed


def configure_auth(app: FastAPI) -> None:
    config = AuthConfig.from_env(required=False)
    if config is None:
        return
    install_auth(
        app,
        _InstalledAuth(
            password_hasher=BcryptPasswordHasher(),
            token_service=JwtAccessTokenService(
                secret=config.jwt_secret,
                ttl_seconds=config.access_token_ttl_seconds,
            ),
        ),
    )


def _get_installed_auth(app: FastAPI) -> _InstalledAuth:
    installed = getattr(app.state, "auth", None)
    if isinstance(installed, _InstalledAuth):
        return installed
    raise ApiError(code="auth.not_configured", message="鉴权未配置", status_code=503)


def _parse_bearer_token(authorization: str | None) -> str:
    if not authorization:
        raise ApiError(
            code="auth.missing_token",
            message="缺少 Authorization Bearer Token",
            status_code=401,
        )

    scheme, _, token = authorization.partition(" ")
    if scheme.casefold() != "bearer" or not token.strip():
        raise ApiError(
            code="auth.invalid_authorization",
            message="Authorization 格式应为 Bearer <token>",
            status_code=401,
        )

    return token.strip()


async def _get_user_repo(session: AsyncSession = Depends(get_db_session)) -> UserRepository:
    return SqlAlchemyUserRepository(session)


async def _get_credential_repo(session: AsyncSession = Depends(get_db_session)) -> UserCredentialRepository:
    return SqlAlchemyUserCredentialRepository(session)


def _get_installed_auth_from_request(request: Request) -> _InstalledAuth:
    return _get_installed_auth(request.app)


async def _get_auth_service(
    installed: _InstalledAuth = Depends(_get_installed_auth_from_request),
    user_repo: UserRepository = Depends(_get_user_repo),
    credential_repo: UserCredentialRepository = Depends(_get_credential_repo),
) -> AuthService:
    return AuthService(
        user_repo=user_repo,
        credential_repo=credential_repo,
        password_hasher=installed.password_hasher,
        token_service=installed.token_service,
    )


async def _get_current_user(
    request: Request,
    auth_service: AuthService = Depends(_get_auth_service),
) -> User:
    token = _parse_bearer_token(request.headers.get("Authorization"))

    try:
        user_id = auth_service.verify_access_token(token=token)
        return await auth_service.get_user(user_id=user_id)
    except TokenExpiredError as exc:
        raise ApiError(code="auth.token_expired", message=str(exc), status_code=401) from exc
    except TokenInvalidError as exc:
        raise ApiError(code="auth.invalid_token", message=str(exc), status_code=401) from exc
    except UserNotFoundError as exc:
        raise ApiError(code="auth.user_not_found", message="用户不存在", status_code=401) from exc


class LoginRequest(BaseModel):
    login: str = Field(min_length=1, max_length=256)
    password: str = Field(min_length=1, max_length=1024)


class LoginResponse(BaseModel):
    access_token: str
    token_type: str = "bearer"


class MeResponse(BaseModel):
    id: uuid.UUID
    display_name: str
    created_at: datetime


@_v1_router.post("/auth/login", response_model=LoginResponse)
async def login(body: LoginRequest, auth_service: AuthService = Depends(_get_auth_service)) -> LoginResponse:
    try:
        token = await auth_service.issue_access_token(login=body.login, password=body.password)
    except InvalidCredentialsError as exc:
        raise ApiError(
            code="auth.invalid_credentials",
            message="账号或密码错误",
            status_code=401,
        ) from exc
    return LoginResponse(access_token=token, token_type="bearer")


@_v1_router.get("/me", response_model=MeResponse)
async def me(current_user: User = Depends(_get_current_user)) -> MeResponse:
    return MeResponse(
        id=current_user.id,
        display_name=current_user.display_name,
        created_at=current_user.created_at,
    )


__all__ = ["configure_auth", "install_auth", "v1_router"]

v1_router = _v1_router

