from __future__ import annotations

import asyncio
from dataclasses import dataclass
from datetime import datetime, timezone
import json
import time
import uuid

from fastapi import APIRouter, Depends, FastAPI, Query, Request
from pydantic import BaseModel, Field
from sqlalchemy.ext.asyncio import AsyncSession
from starlette.responses import StreamingResponse

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
    OrgRepository,
    SqlAlchemyOrgMembershipRepository,
    SqlAlchemyOrgRepository,
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

from .audit import AuditLogWriter, get_audit_log_writer
from .authorization import Actor, Authorizer, Resource
from .db import get_db_session
from .error_envelope import ApiError
from .run_executor import RunExecutor, get_run_executor
from .sse import SseConfig, get_sse_config, sse_comment, sse_event

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


def _request_trace_id(request: Request) -> str:
    trace_id = getattr(request.state, "trace_id", None)
    if isinstance(trace_id, str) and trace_id:
        return trace_id
    trace_id = uuid.uuid4().hex
    request.state.trace_id = trace_id
    return trace_id

async def _authorize_or_audit(
    action: str,
    *,
    request: Request,
    authorizer: Authorizer,
    audit: AuditLogWriter,
    actor: Actor,
    resource: Resource,
    target_type: str,
    target_id: str,
) -> None:
    try:
        await authorizer.authorize(action, actor=actor, resource=resource)
    except ApiError as exc:
        if exc.status_code != 403 or exc.code != "policy.denied":
            raise

        trace_id = _request_trace_id(request)
        deny_reason = "owner_mismatch"
        if actor.org_id != resource.org_id:
            deny_reason = "org_mismatch"
        elif resource.owner_user_id is None:
            deny_reason = "no_owner"

        await audit.write_access_denied(
            trace_id=trace_id,
            actor=actor,
            action=action,
            target_type=target_type,
            target_id=target_id,
            resource_org_id=resource.org_id,
            resource_owner_user_id=resource.owner_user_id,
            deny_reason=deny_reason,
        )
        raise


async def _get_user_repo(session: AsyncSession = Depends(get_db_session)) -> UserRepository:
    return SqlAlchemyUserRepository(session)


async def _get_credential_repo(
    session: AsyncSession = Depends(get_db_session),
) -> UserCredentialRepository:
    return SqlAlchemyUserCredentialRepository(session)


async def _get_org_membership_repo(
    session: AsyncSession = Depends(get_db_session),
) -> OrgMembershipRepository:
    return SqlAlchemyOrgMembershipRepository(session)


async def _get_org_repo(session: AsyncSession = Depends(get_db_session)) -> OrgRepository:
    return SqlAlchemyOrgRepository(session)


async def _get_thread_repo(session: AsyncSession = Depends(get_db_session)) -> ThreadRepository:
    return SqlAlchemyThreadRepository(session)


async def _get_message_repo(session: AsyncSession = Depends(get_db_session)) -> MessageRepository:
    return SqlAlchemyMessageRepository(session)


async def _get_run_event_repo(
    session: AsyncSession = Depends(get_db_session),
) -> RunEventRepository:
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
        return await auth_service.authenticate_user(token=token)
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


class LogoutResponse(BaseModel):
    ok: bool = True


class RegisterRequest(BaseModel):
    login: str = Field(min_length=1, max_length=256, description="登录名（唯一）")
    password: str = Field(min_length=8, max_length=1024, description="密码（至少8位）")
    display_name: str = Field(min_length=1, max_length=256, description="显示名称")


class RegisterResponse(BaseModel):
    user_id: uuid.UUID
    access_token: str
    token_type: str = "bearer"


class MeResponse(BaseModel):
    id: uuid.UUID
    display_name: str
    created_at: datetime


class CreateThreadRequest(BaseModel):
    title: str | None = Field(default=None, max_length=200)

class UpdateThreadRequest(BaseModel):
    title: str | None = Field(..., max_length=200)


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


class RunResponse(BaseModel):
    run_id: uuid.UUID
    org_id: uuid.UUID
    thread_id: uuid.UUID
    created_by_user_id: uuid.UUID | None
    status: str
    created_at: datetime
    trace_id: str


class CreateRunRequest(BaseModel):
    route_id: str | None = Field(
        default=None,
        max_length=64,
        pattern=r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$",
    )


async def _get_thread_or_404(*, thread_id: uuid.UUID, thread_repo: ThreadRepository):
    thread = await thread_repo.get_by_id(thread_id)
    if thread is None:
        raise ApiError(code="threads.not_found", message="Thread 不存在", status_code=404)
    return thread


@_v1_router.post("/auth/login", response_model=LoginResponse)
async def login(
    request: Request,
    body: LoginRequest,
    auth_service: AuthService = Depends(_get_auth_service),
    audit: AuditLogWriter = Depends(get_audit_log_writer),
) -> LoginResponse:
    trace_id = _request_trace_id(request)
    try:
        issued = await auth_service.issue_access_token(login=body.login, password=body.password)
    except InvalidCredentialsError as exc:
        await audit.write_login_failed(trace_id=trace_id, login=body.login)
        raise ApiError(
            code="auth.invalid_credentials",
            message="账号或密码错误",
            status_code=401,
        ) from exc
    await audit.write_login_succeeded(trace_id=trace_id, user_id=issued.user_id, login=body.login)
    return LoginResponse(access_token=issued.token, token_type="bearer")


@_v1_router.post("/auth/refresh", response_model=LoginResponse)
async def refresh_token(
    request: Request,
    auth_service: AuthService = Depends(_get_auth_service),
    audit: AuditLogWriter = Depends(get_audit_log_writer),
) -> LoginResponse:
    trace_id = _request_trace_id(request)
    token = _parse_bearer_token(request.headers.get("Authorization"))

    try:
        issued = await auth_service.refresh_access_token(token=token)
    except (TokenExpiredError, TokenInvalidError, UserNotFoundError) as exc:
        raise ApiError(
            code="auth.invalid_token",
            message="token 无效或已过期",
            status_code=401,
        ) from exc

    await audit.write_token_refreshed(trace_id=trace_id, user_id=issued.user_id)
    return LoginResponse(access_token=issued.token, token_type="bearer")


@_v1_router.post("/auth/logout", response_model=LogoutResponse)
async def logout(
    request: Request,
    current_user: User = Depends(_get_current_user),
    auth_service: AuthService = Depends(_get_auth_service),
    session: AsyncSession = Depends(get_db_session),
    audit: AuditLogWriter = Depends(get_audit_log_writer),
) -> LogoutResponse:
    trace_id = _request_trace_id(request)

    await auth_service.logout(user_id=current_user.id)
    await session.commit()

    await audit.write_logout(trace_id=trace_id, user_id=current_user.id)

    return LogoutResponse()


@_v1_router.post("/auth/register", response_model=RegisterResponse, status_code=201)
async def register(
    request: Request,
    body: RegisterRequest,
    session: AsyncSession = Depends(get_db_session),
    installed: _InstalledAuth = Depends(_get_installed_auth_from_request),
    user_repo: UserRepository = Depends(_get_user_repo),
    credential_repo: UserCredentialRepository = Depends(_get_credential_repo),
    org_repo: OrgRepository = Depends(_get_org_repo),
    org_membership_repo: OrgMembershipRepository = Depends(_get_org_membership_repo),
    audit: AuditLogWriter = Depends(get_audit_log_writer),
) -> RegisterResponse:
    trace_id = _request_trace_id(request)

    # 检查 login 是否已存在
    existing = await credential_repo.get_by_login(body.login)
    if existing is not None:
        raise ApiError(
            code="auth.login_exists",
            message="该登录名已被使用",
            status_code=409,
        )

    # 创建用户
    user = await user_repo.create(display_name=body.display_name)

    # 创建凭证
    password_hash = installed.password_hasher.hash_password(body.password)
    await credential_repo.create(
        user_id=user.id,
        login=body.login,
        password_hash=password_hash,
    )

    # 创建个人组织
    org_slug = f"user-{user.id.hex[:8]}"
    org = await org_repo.create(slug=org_slug, name=f"{body.display_name} 的空间")

    # 创建组织成员关系
    await org_membership_repo.create(org_id=org.id, user_id=user.id, role="owner")

    # 提交事务
    await session.commit()

    # 签发 token
    token = installed.token_service.issue(user_id=user.id)

    await audit.write_user_registered(trace_id=trace_id, user_id=user.id, login=body.login)

    return RegisterResponse(user_id=user.id, access_token=token, token_type="bearer")


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

@_v1_router.get("/threads", response_model=list[ThreadResponse])
async def list_threads(
    actor: Actor = Depends(_get_current_actor),
    thread_repo: ThreadRepository = Depends(_get_thread_repo),
    limit: int = Query(50, ge=1, le=200),
    before_created_at: datetime | None = Query(default=None),
    before_id: uuid.UUID | None = Query(default=None),
) -> list[ThreadResponse]:
    if (before_created_at is None) != (before_id is None):
        raise ApiError(
            code="validation_error",
            message="请求参数校验失败",
            status_code=422,
            details={"reason": "cursor_incomplete", "required": ["before_created_at", "before_id"]},
        )

    threads = await thread_repo.list_by_owner(
        org_id=actor.org_id,
        owner_user_id=actor.user_id,
        limit=limit,
        before_created_at=before_created_at,
        before_id=before_id,
    )
    return [
        ThreadResponse(
            id=item.id,
            org_id=item.org_id,
            created_by_user_id=item.created_by_user_id,
            title=item.title,
            created_at=item.created_at,
        )
        for item in threads
    ]


@_v1_router.get("/threads/{thread_id}", response_model=ThreadResponse)
async def get_thread(
    thread_id: uuid.UUID,
    request: Request,
    actor: Actor = Depends(_get_current_actor),
    authorizer: Authorizer = Depends(_get_authorizer),
    audit: AuditLogWriter = Depends(get_audit_log_writer),
    thread_repo: ThreadRepository = Depends(_get_thread_repo),
) -> ThreadResponse:
    thread = await _get_thread_or_404(thread_id=thread_id, thread_repo=thread_repo)
    await _authorize_or_audit(
        "threads.get",
        request=request,
        authorizer=authorizer,
        audit=audit,
        actor=actor,
        resource=Resource(org_id=thread.org_id, owner_user_id=thread.created_by_user_id),
        target_type="thread",
        target_id=str(thread.id),
    )

    return ThreadResponse(
        id=thread.id,
        org_id=thread.org_id,
        created_by_user_id=thread.created_by_user_id,
        title=thread.title,
        created_at=thread.created_at,
    )


@_v1_router.patch("/threads/{thread_id}", response_model=ThreadResponse)
async def patch_thread(
    thread_id: uuid.UUID,
    request: Request,
    body: UpdateThreadRequest,
    actor: Actor = Depends(_get_current_actor),
    authorizer: Authorizer = Depends(_get_authorizer),
    audit: AuditLogWriter = Depends(get_audit_log_writer),
    thread_repo: ThreadRepository = Depends(_get_thread_repo),
) -> ThreadResponse:
    thread = await _get_thread_or_404(thread_id=thread_id, thread_repo=thread_repo)
    await _authorize_or_audit(
        "threads.update",
        request=request,
        authorizer=authorizer,
        audit=audit,
        actor=actor,
        resource=Resource(org_id=thread.org_id, owner_user_id=thread.created_by_user_id),
        target_type="thread",
        target_id=str(thread.id),
    )

    updated = await thread_repo.update_title(thread_id=thread.id, title=body.title)
    if updated is None:
        raise ApiError(code="threads.not_found", message="Thread 不存在", status_code=404)

    return ThreadResponse(
        id=updated.id,
        org_id=updated.org_id,
        created_by_user_id=updated.created_by_user_id,
        title=updated.title,
        created_at=updated.created_at,
    )


@_v1_router.post("/threads/{thread_id}/messages", response_model=MessageResponse, status_code=201)
async def create_message(
    thread_id: uuid.UUID,
    request: Request,
    body: CreateMessageRequest,
    actor: Actor = Depends(_get_current_actor),
    authorizer: Authorizer = Depends(_get_authorizer),
    audit: AuditLogWriter = Depends(get_audit_log_writer),
    thread_repo: ThreadRepository = Depends(_get_thread_repo),
    message_repo: MessageRepository = Depends(_get_message_repo),
) -> MessageResponse:
    thread = await _get_thread_or_404(thread_id=thread_id, thread_repo=thread_repo)
    await _authorize_or_audit(
        "messages.create",
        request=request,
        authorizer=authorizer,
        audit=audit,
        actor=actor,
        resource=Resource(org_id=thread.org_id, owner_user_id=thread.created_by_user_id),
        target_type="thread",
        target_id=str(thread.id),
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
    request: Request,
    actor: Actor = Depends(_get_current_actor),
    authorizer: Authorizer = Depends(_get_authorizer),
    audit: AuditLogWriter = Depends(get_audit_log_writer),
    thread_repo: ThreadRepository = Depends(_get_thread_repo),
    message_repo: MessageRepository = Depends(_get_message_repo),
    limit: int = Query(200, ge=1, le=500),
) -> list[MessageResponse]:
    thread = await _get_thread_or_404(thread_id=thread_id, thread_repo=thread_repo)
    await _authorize_or_audit(
        "messages.list",
        request=request,
        authorizer=authorizer,
        audit=audit,
        actor=actor,
        resource=Resource(org_id=thread.org_id, owner_user_id=thread.created_by_user_id),
        target_type="thread",
        target_id=str(thread.id),
    )

    messages = await message_repo.list_by_thread(
        org_id=actor.org_id, thread_id=thread_id, limit=limit
    )
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
    body: CreateRunRequest | None = None,
    actor: Actor = Depends(_get_current_actor),
    authorizer: Authorizer = Depends(_get_authorizer),
    audit: AuditLogWriter = Depends(get_audit_log_writer),
    thread_repo: ThreadRepository = Depends(_get_thread_repo),
    run_event_repo: RunEventRepository = Depends(_get_run_event_repo),
    session: AsyncSession = Depends(get_db_session),
    run_executor: RunExecutor = Depends(get_run_executor),
) -> CreateRunResponse:
    thread = await _get_thread_or_404(thread_id=thread_id, thread_repo=thread_repo)
    await _authorize_or_audit(
        "runs.create",
        request=request,
        authorizer=authorizer,
        audit=audit,
        actor=actor,
        resource=Resource(org_id=thread.org_id, owner_user_id=thread.created_by_user_id),
        target_type="thread",
        target_id=str(thread.id),
    )

    trace_id = getattr(request.state, "trace_id", None)
    if not isinstance(trace_id, str) or not trace_id:
        trace_id = uuid.uuid4().hex
        request.state.trace_id = trace_id

    started_data: dict[str, object] = {}
    if body is not None and body.route_id:
        started_data["route_id"] = body.route_id

    run, _started = await run_event_repo.create_run_with_started_event(
        org_id=thread.org_id,
        thread_id=thread.id,
        created_by_user_id=actor.user_id,
        started_data=started_data,
    )
    await session.commit()
    run_executor.enqueue(run_id=run.id)
    return CreateRunResponse(run_id=run.id, trace_id=trace_id)


_RUN_TERMINAL_EVENT_TYPES: tuple[str, ...] = ("run.completed", "run.failed", "run.cancelled")


def _derive_run_status(terminal_event_type: str | None) -> str:
    if terminal_event_type == "run.completed":
        return "completed"
    if terminal_event_type == "run.failed":
        return "failed"
    if terminal_event_type == "run.cancelled":
        return "cancelled"
    return "running"


@_v1_router.get("/runs/{run_id}", response_model=RunResponse)
async def get_run(
    run_id: uuid.UUID,
    request: Request,
    actor: Actor = Depends(_get_current_actor),
    authorizer: Authorizer = Depends(_get_authorizer),
    audit: AuditLogWriter = Depends(get_audit_log_writer),
    run_event_repo: RunEventRepository = Depends(_get_run_event_repo),
) -> RunResponse:
    run = await run_event_repo.get_run(run_id=run_id)
    if run is None:
        raise ApiError(code="runs.not_found", message="Run 不存在", status_code=404)

    await _authorize_or_audit(
        "runs.get",
        request=request,
        authorizer=authorizer,
        audit=audit,
        actor=actor,
        resource=Resource(org_id=run.org_id, owner_user_id=run.created_by_user_id),
        target_type="run",
        target_id=str(run.id),
    )

    terminal_event_type = await run_event_repo.get_latest_event_type(
        run_id=run.id, types=_RUN_TERMINAL_EVENT_TYPES
    )
    return RunResponse(
        run_id=run.id,
        org_id=run.org_id,
        thread_id=run.thread_id,
        created_by_user_id=run.created_by_user_id,
        status=_derive_run_status(terminal_event_type),
        created_at=run.created_at,
        trace_id=_request_trace_id(request),
    )


def _to_rfc3339_millis_z(value: datetime) -> str:
    aware = value if value.tzinfo is not None else value.replace(tzinfo=timezone.utc)
    utc = aware.astimezone(timezone.utc)
    return utc.isoformat(timespec="milliseconds").replace("+00:00", "Z")


@_v1_router.get("/runs/{run_id}/events")
async def stream_run_events(
    run_id: uuid.UUID,
    request: Request,
    after_seq: int = Query(0, ge=0),
    follow: bool = Query(True),
    actor: Actor = Depends(_get_current_actor),
    authorizer: Authorizer = Depends(_get_authorizer),
    audit: AuditLogWriter = Depends(get_audit_log_writer),
    run_event_repo: RunEventRepository = Depends(_get_run_event_repo),
    sse_config: SseConfig = Depends(get_sse_config),
) -> StreamingResponse:
    run = await run_event_repo.get_run(run_id=run_id)
    if run is None:
        raise ApiError(code="runs.not_found", message="Run 不存在", status_code=404)

    await _authorize_or_audit(
        "runs.events",
        request=request,
        authorizer=authorizer,
        audit=audit,
        actor=actor,
        resource=Resource(org_id=run.org_id, owner_user_id=run.created_by_user_id),
        target_type="run",
        target_id=str(run.id),
    )

    async def _stream():
        cursor = after_seq
        last_send = time.monotonic()

        try:
            if follow:
                yield sse_comment("ping")
                last_send = time.monotonic()
            while True:
                if await request.is_disconnected():
                    return

                events = await run_event_repo.list_events(
                    run_id=run_id,
                    after_seq=cursor,
                    limit=sse_config.batch_limit,
                )
                if events:
                    for item in events:
                        cursor = item.seq
                        payload = {
                            "event_id": str(item.event_id),
                            "run_id": str(item.run_id),
                            "seq": item.seq,
                            "ts": _to_rfc3339_millis_z(item.ts),
                            "type": item.type,
                            "data": item.data_json,
                        }
                        yield sse_event(
                            event=item.type,
                            event_id=str(item.seq),
                            data=json.dumps(payload, ensure_ascii=False, separators=(",", ":")),
                        )
                        last_send = time.monotonic()
                    continue

                if not follow:
                    return

                now = time.monotonic()
                if (
                    sse_config.heartbeat_seconds > 0
                    and (now - last_send) >= sse_config.heartbeat_seconds
                ):
                    yield sse_comment("ping")
                    last_send = now

                await asyncio.sleep(sse_config.poll_seconds)
        except asyncio.CancelledError:
            return

    return StreamingResponse(
        _stream(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "X-Accel-Buffering": "no",
        },
    )


__all__ = ["configure_auth", "install_auth", "v1_router"]

v1_router = _v1_router
