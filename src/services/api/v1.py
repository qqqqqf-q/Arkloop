from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
import uuid

from fastapi import APIRouter, Depends, FastAPI, Query, Request
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
from packages.data.identity import (
    OrgMembershipRepository,
    SqlAlchemyOrgMembershipRepository,
    SqlAlchemyUserRepository,
    User,
    UserRepository,
)
from packages.data.runs import RunEventRepository, SqlAlchemyRunEventRepository
from packages.data.threads import (
    MessageRepository,
    SqlAlchemyMessageRepository,
    SqlAlchemyThreadRepository,
    ThreadNotFoundError,
    ThreadRepository,
)

from .authorization import Actor, Authorizer
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


async def _get_org_membership_repo(session: AsyncSession = Depends(get_db_session)) -> OrgMembershipRepository:
    return SqlAlchemyOrgMembershipRepository(session)


async def _get_thread_repo(session: AsyncSession = Depends(get_db_session)) -> ThreadRepository:
    return SqlAlchemyThreadRepository(session)


async def _get_message_repo(session: AsyncSession = Depends(get_db_session)) -> MessageRepository:
    return SqlAlchemyMessageRepository(session)

async def _get_run_event_repo(session: AsyncSession = Depends(get_db_session)) -> RunEventRepository:
    return SqlAlchemyRunEventRepository(session)


async def _get_authorizer() -> Authorizer:
    return Authorizer()


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


async def _get_current_actor(
    current_user: User = Depends(_get_current_user),
    membership_repo: OrgMembershipRepository = Depends(_get_org_membership_repo),
) -> Actor:
    membership = await membership_repo.get_default_for_user(user_id=current_user.id)
    if membership is None:
        raise ApiError(code="auth.no_org_membership", message="用户未加入任何组织", status_code=403)
    return Actor(org_id=membership.org_id, user_id=current_user.id, org_role=membership.role)


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


class CreateThreadRequest(BaseModel):
    title: str | None = Field(default=None, max_length=200)


class ThreadResponse(BaseModel):
    id: uuid.UUID
    org_id: uuid.UUID
    created_by_user_id: uuid.UUID | None
    title: str | None
    created_at: datetime


class CreateMessageRequest(BaseModel):
    content: str = Field(min_length=1, max_length=20000)


class MessageResponse(BaseModel):
    id: uuid.UUID
    org_id: uuid.UUID
    thread_id: uuid.UUID
    created_by_user_id: uuid.UUID | None
    role: str
    content: str
    created_at: datetime


class CreateRunResponse(BaseModel):
    run_id: uuid.UUID
    trace_id: str


async def _get_thread_or_404(*, thread_id: uuid.UUID, thread_repo: ThreadRepository):
    thread = await thread_repo.get_by_id(thread_id)
    if thread is None:
        raise ApiError(code="threads.not_found", message="Thread 不存在", status_code=404)
    return thread


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


@_v1_router.post("/threads", response_model=ThreadResponse, status_code=201)
async def create_thread(
    body: CreateThreadRequest,
    actor: Actor = Depends(_get_current_actor),
    thread_repo: ThreadRepository = Depends(_get_thread_repo),
) -> ThreadResponse:
    thread = await thread_repo.create(
        org_id=actor.org_id,
        created_by_user_id=actor.user_id,
        title=body.title,
    )
    return ThreadResponse(
        id=thread.id,
        org_id=thread.org_id,
        created_by_user_id=thread.created_by_user_id,
        title=thread.title,
        created_at=thread.created_at,
    )


@_v1_router.post("/threads/{thread_id}/messages", response_model=MessageResponse, status_code=201)
async def create_message(
    thread_id: uuid.UUID,
    body: CreateMessageRequest,
    actor: Actor = Depends(_get_current_actor),
    authorizer: Authorizer = Depends(_get_authorizer),
    thread_repo: ThreadRepository = Depends(_get_thread_repo),
    message_repo: MessageRepository = Depends(_get_message_repo),
) -> MessageResponse:
    thread = await _get_thread_or_404(thread_id=thread_id, thread_repo=thread_repo)
    await authorizer.authorize(
        "messages.create",
        actor=actor,
        resource_org_id=thread.org_id,
        resource_owner_user_id=thread.created_by_user_id,
    )

    try:
        message = await message_repo.create(
            org_id=actor.org_id,
            thread_id=thread_id,
            role="user",
            content=body.content,
            created_by_user_id=actor.user_id,
        )
    except ThreadNotFoundError as exc:
        raise ApiError(code="threads.not_found", message="Thread 不存在", status_code=404) from exc

    return MessageResponse(
        id=message.id,
        org_id=message.org_id,
        thread_id=message.thread_id,
        created_by_user_id=message.created_by_user_id,
        role=message.role,
        content=message.content,
        created_at=message.created_at,
    )


@_v1_router.get("/threads/{thread_id}/messages", response_model=list[MessageResponse])
async def list_messages(
    thread_id: uuid.UUID,
    actor: Actor = Depends(_get_current_actor),
    authorizer: Authorizer = Depends(_get_authorizer),
    thread_repo: ThreadRepository = Depends(_get_thread_repo),
    message_repo: MessageRepository = Depends(_get_message_repo),
    limit: int = Query(200, ge=1, le=500),
) -> list[MessageResponse]:
    thread = await _get_thread_or_404(thread_id=thread_id, thread_repo=thread_repo)
    await authorizer.authorize(
        "messages.list",
        actor=actor,
        resource_org_id=thread.org_id,
        resource_owner_user_id=thread.created_by_user_id,
    )

    messages = await message_repo.list_by_thread(org_id=actor.org_id, thread_id=thread_id, limit=limit)
    return [
        MessageResponse(
            id=item.id,
            org_id=item.org_id,
            thread_id=item.thread_id,
            created_by_user_id=item.created_by_user_id,
            role=item.role,
            content=item.content,
            created_at=item.created_at,
        )
        for item in messages
    ]


@_v1_router.post("/threads/{thread_id}/runs", response_model=CreateRunResponse, status_code=201)
async def create_run(
    thread_id: uuid.UUID,
    request: Request,
    actor: Actor = Depends(_get_current_actor),
    authorizer: Authorizer = Depends(_get_authorizer),
    thread_repo: ThreadRepository = Depends(_get_thread_repo),
    run_event_repo: RunEventRepository = Depends(_get_run_event_repo),
) -> CreateRunResponse:
    thread = await _get_thread_or_404(thread_id=thread_id, thread_repo=thread_repo)
    await authorizer.authorize(
        "runs.create",
        actor=actor,
        resource_org_id=thread.org_id,
        resource_owner_user_id=thread.created_by_user_id,
    )

    trace_id = getattr(request.state, "trace_id", None)
    if not isinstance(trace_id, str) or not trace_id:
        trace_id = uuid.uuid4().hex
        request.state.trace_id = trace_id

    run, _started = await run_event_repo.create_run_with_started_event(
        org_id=thread.org_id,
        thread_id=thread.id,
        created_by_user_id=actor.user_id,
    )
    return CreateRunResponse(run_id=run.id, trace_id=trace_id)


__all__ = ["configure_auth", "install_auth", "v1_router"]

v1_router = _v1_router
