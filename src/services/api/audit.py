from __future__ import annotations

from dataclasses import dataclass
import hashlib
import logging
from typing import Any
import uuid

from fastapi import Request

from packages.data import Database
from packages.data.audit_logs import SqlAlchemyAuditLogRepository
from packages.data.identity import SqlAlchemyOrgMembershipRepository
from packages.observability.context import trace_id_context

from .authorization import Actor

_logger = logging.getLogger("arkloop.audit")


def _sha256_hex(value: str) -> str:
    return hashlib.sha256(value.encode("utf-8")).hexdigest()


@dataclass(frozen=True)
class AuditLogWriter:
    database: Database | None

    async def write_login_failed(self, *, trace_id: str, login: str) -> None:
        if self.database is None:
            return
        login_hash = _sha256_hex(login)
        await self._write(
            org_id=None,
            actor_user_id=None,
            action="auth.login",
            target_type="user_login",
            target_id=login_hash,
            trace_id=trace_id,
            metadata_json={
                "result": "failed",
                "method": "password",
                "login_hash": login_hash,
            },
        )

    async def write_login_succeeded(self, *, trace_id: str, user_id: uuid.UUID, login: str) -> None:
        if self.database is None:
            return
        login_hash = _sha256_hex(login)
        async with self.database.sessionmaker() as session:
            try:
                membership_repo = SqlAlchemyOrgMembershipRepository(session)
                membership = await membership_repo.get_default_for_user(user_id=user_id)
                org_id = None if membership is None else membership.org_id

                repo = SqlAlchemyAuditLogRepository(session)
                await repo.create(
                    org_id=org_id,
                    actor_user_id=user_id,
                    action="auth.login",
                    target_type="user",
                    target_id=str(user_id),
                    trace_id=trace_id,
                    metadata_json={
                        "result": "succeeded",
                        "method": "password",
                        "login_hash": login_hash,
                    },
                )
                await session.commit()
            except Exception:
                try:
                    await session.rollback()
                except Exception:
                    _logger.exception("回滚登录审计失败", extra={"user_id": str(user_id)})
                _logger.exception("写入登录成功审计失败", extra={"user_id": str(user_id)})

    async def write_user_registered(self, *, trace_id: str, user_id: uuid.UUID, login: str) -> None:
        if self.database is None:
            return
        login_hash = _sha256_hex(login)
        await self._write(
            org_id=None,
            actor_user_id=user_id,
            action="auth.register",
            target_type="user",
            target_id=str(user_id),
            trace_id=trace_id,
            metadata_json={
                "login_hash": login_hash,
            },
        )

    async def write_access_denied(
        self,
        *,
        trace_id: str,
        actor: Actor,
        action: str,
        target_type: str,
        target_id: str,
        resource_org_id: uuid.UUID,
        resource_owner_user_id: uuid.UUID | None,
        deny_reason: str,
    ) -> None:
        with trace_id_context(trace_id):
            _logger.warning(
                "访问拒绝",
                extra={
                    "action": action,
                    "target_type": target_type,
                    "target_id": target_id,
                    "deny_reason": deny_reason,
                    "actor_org_id": str(actor.org_id),
                    "actor_user_id": str(actor.user_id),
                    "resource_org_id": str(resource_org_id),
                    "resource_owner_user_id": None
                    if resource_owner_user_id is None
                    else str(resource_owner_user_id),
                },
            )
        if self.database is None:
            return
        await self._write(
            org_id=actor.org_id,
            actor_user_id=actor.user_id,
            action=action,
            target_type=target_type,
            target_id=target_id,
            trace_id=trace_id,
            metadata_json={
                "result": "denied",
                "deny_reason": deny_reason,
                "actor_org_id": str(actor.org_id),
                "actor_user_id": str(actor.user_id),
                "resource_org_id": str(resource_org_id),
                "resource_owner_user_id": None
                if resource_owner_user_id is None
                else str(resource_owner_user_id),
            },
        )

    async def _write(
        self,
        *,
        org_id: uuid.UUID | None,
        actor_user_id: uuid.UUID | None,
        action: str,
        target_type: str | None,
        target_id: str | None,
        trace_id: str,
        metadata_json: Any,
    ) -> None:
        if self.database is None:
            return
        try:
            async with self.database.sessionmaker() as session:
                try:
                    repo = SqlAlchemyAuditLogRepository(session)
                    await repo.create(
                        org_id=org_id,
                        actor_user_id=actor_user_id,
                        action=action,
                        target_type=target_type,
                        target_id=target_id,
                        trace_id=trace_id,
                        metadata_json=metadata_json,
                    )
                    await session.commit()
                except Exception:
                    try:
                        await session.rollback()
                    except Exception:
                        _logger.exception(
                            "回滚审计失败",
                            extra={
                                "action": action,
                                "target_type": target_type,
                                "target_id": target_id,
                            },
                        )
                    _logger.exception(
                        "写入审计失败",
                        extra={
                            "action": action,
                            "target_type": target_type,
                            "target_id": target_id,
                        },
                    )
        except Exception:
            _logger.exception(
                "写入审计失败",
                extra={
                    "action": action,
                    "target_type": target_type,
                    "target_id": target_id,
                },
            )


def get_audit_log_writer(request: Request) -> AuditLogWriter:
    database = getattr(request.app.state, "database", None)
    if isinstance(database, Database):
        return AuditLogWriter(database=database)
    return AuditLogWriter(database=None)


__all__ = ["AuditLogWriter", "get_audit_log_writer"]
