from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass
from datetime import datetime
from typing import Any, Sequence
import uuid

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql
from sqlalchemy.exc import IntegrityError
from sqlalchemy.ext.asyncio import AsyncSession

_metadata = sa.MetaData()

_runs = sa.Table(
    "runs",
    _metadata,
    sa.Column(
        "id",
        postgresql.UUID(as_uuid=True),
        primary_key=True,
        server_default=sa.text("gen_random_uuid()"),
    ),
    sa.Column(
        "org_id",
        postgresql.UUID(as_uuid=True),
        sa.ForeignKey("orgs.id", ondelete="CASCADE"),
        nullable=False,
    ),
    sa.Column(
        "thread_id",
        postgresql.UUID(as_uuid=True),
        sa.ForeignKey("threads.id", ondelete="CASCADE"),
        nullable=False,
    ),
    sa.Column(
        "created_by_user_id",
        postgresql.UUID(as_uuid=True),
        sa.ForeignKey("users.id", ondelete="SET NULL"),
        nullable=True,
    ),
    sa.Column("status", sa.Text(), nullable=False, server_default=sa.text("'running'")),
    sa.Column("next_event_seq", sa.BigInteger(), nullable=False, server_default=sa.text("1")),
    sa.Column(
        "created_at",
        sa.TIMESTAMP(timezone=True),
        nullable=False,
        server_default=sa.text("now()"),
    ),
)

_run_events = sa.Table(
    "run_events",
    _metadata,
    sa.Column(
        "event_id",
        postgresql.UUID(as_uuid=True),
        primary_key=True,
        server_default=sa.text("gen_random_uuid()"),
    ),
    sa.Column(
        "run_id",
        postgresql.UUID(as_uuid=True),
        sa.ForeignKey("runs.id", ondelete="CASCADE"),
        nullable=False,
    ),
    sa.Column("seq", sa.BigInteger(), nullable=False),
    sa.Column(
        "ts",
        sa.TIMESTAMP(timezone=True),
        nullable=False,
        server_default=sa.text("now()"),
    ),
    sa.Column("type", sa.Text(), nullable=False),
    sa.Column(
        "data_json",
        postgresql.JSONB(astext_type=sa.Text()),
        nullable=False,
        server_default=sa.text("'{}'::jsonb"),
    ),
    sa.Column("tool_name", sa.Text(), nullable=True),
    sa.Column("error_class", sa.Text(), nullable=True),
    sa.UniqueConstraint("run_id", "seq", name="uq_run_events_run_id_seq"),
)


@dataclass(frozen=True, slots=True)
class Run:
    id: uuid.UUID
    org_id: uuid.UUID
    thread_id: uuid.UUID
    created_by_user_id: uuid.UUID | None
    status: str
    created_at: datetime


@dataclass(frozen=True, slots=True)
class RunEvent:
    event_id: uuid.UUID
    run_id: uuid.UUID
    seq: int
    ts: datetime
    type: str
    data_json: Any
    tool_name: str | None
    error_class: str | None


class RunNotFoundError(LookupError):
    def __init__(self, *, run_id: uuid.UUID) -> None:
        super().__init__("Run 不存在")
        self.run_id = run_id


class RunEventSeqConflictError(Exception):
    def __init__(self, *, run_id: uuid.UUID, seq: int) -> None:
        super().__init__("RunEvent seq 冲突")
        self.run_id = run_id
        self.seq = seq


def _is_unique_violation(exc: IntegrityError, *, constraint: str) -> bool:
    original = getattr(exc, "orig", None)
    sqlstate = getattr(original, "sqlstate", None)
    if sqlstate != "23505":
        return False
    name = getattr(original, "constraint_name", None) or getattr(original, "constraint", None)
    if name:
        return bool(name == constraint)
    return constraint in str(original)


class RunEventRepository(ABC):
    @abstractmethod
    async def create_run_with_started_event(
        self,
        *,
        org_id: uuid.UUID,
        thread_id: uuid.UUID,
        created_by_user_id: uuid.UUID | None = None,
        started_type: str = "run.started",
        started_data: Any | None = None,
    ) -> tuple[Run, RunEvent]: ...

    @abstractmethod
    async def get_run(self, *, run_id: uuid.UUID) -> Run | None: ...

    @abstractmethod
    async def get_latest_event_type(
        self,
        *,
        run_id: uuid.UUID,
        types: Sequence[str] | None = None,
    ) -> str | None: ...

    @abstractmethod
    async def append_event(
        self,
        *,
        run_id: uuid.UUID,
        ts: datetime | None = None,
        type: str,
        data_json: Any,
        tool_name: str | None = None,
        error_class: str | None = None,
    ) -> RunEvent: ...

    @abstractmethod
    async def list_events(
        self,
        *,
        run_id: uuid.UUID,
        after_seq: int = 0,
        limit: int = 500,
    ) -> list[RunEvent]: ...


class SqlAlchemyRunEventRepository(RunEventRepository):
    def __init__(self, session: AsyncSession) -> None:
        self._session = session

    async def create_run_with_started_event(
        self,
        *,
        org_id: uuid.UUID,
        thread_id: uuid.UUID,
        created_by_user_id: uuid.UUID | None = None,
        started_type: str = "run.started",
        started_data: Any | None = None,
    ) -> tuple[Run, RunEvent]:
        async with self._session.begin_nested():
            run_stmt = (
                sa.insert(_runs)
                .values(
                    org_id=org_id,
                    thread_id=thread_id,
                    created_by_user_id=created_by_user_id,
                    status="running",
                )
                .returning(
                    _runs.c.id,
                    _runs.c.org_id,
                    _runs.c.thread_id,
                    _runs.c.created_by_user_id,
                    _runs.c.status,
                    _runs.c.created_at,
                )
            )
            run_row = (await self._session.execute(run_stmt)).mappings().one()
            run = Run(**run_row)

            event = await self._insert_event(
                run_id=run.id,
                type=started_type,
                data_json={} if started_data is None else started_data,
            )
            return run, event

    async def get_run(self, *, run_id: uuid.UUID) -> Run | None:
        stmt = (
            sa.select(
                _runs.c.id,
                _runs.c.org_id,
                _runs.c.thread_id,
                _runs.c.created_by_user_id,
                _runs.c.status,
                _runs.c.created_at,
            )
            .where(_runs.c.id == run_id)
            .limit(1)
        )
        row = (await self._session.execute(stmt)).mappings().one_or_none()
        return None if row is None else Run(**row)

    async def get_latest_event_type(
        self,
        *,
        run_id: uuid.UUID,
        types: Sequence[str] | None = None,
    ) -> str | None:
        if types is not None and not types:
            return None

        stmt = sa.select(_run_events.c.type).where(_run_events.c.run_id == run_id)
        if types is not None:
            stmt = stmt.where(_run_events.c.type.in_(tuple(types)))

        stmt = stmt.order_by(_run_events.c.seq.desc()).limit(1)
        row = (await self._session.execute(stmt)).one_or_none()
        if row is None:
            return None
        value = row[0]
        return None if value is None else str(value)

    async def append_event(
        self,
        *,
        run_id: uuid.UUID,
        ts: datetime | None = None,
        type: str,
        data_json: Any,
        tool_name: str | None = None,
        error_class: str | None = None,
    ) -> RunEvent:
        async with self._session.begin_nested():
            return await self._insert_event(
                run_id=run_id,
                ts=ts,
                type=type,
                data_json=data_json,
                tool_name=tool_name,
                error_class=error_class,
            )

    async def list_events(
        self,
        *,
        run_id: uuid.UUID,
        after_seq: int = 0,
        limit: int = 500,
    ) -> list[RunEvent]:
        if after_seq < 0:
            raise ValueError("after_seq 不能为负数")

        stmt = (
            sa.select(
                _run_events.c.event_id,
                _run_events.c.run_id,
                _run_events.c.seq,
                _run_events.c.ts,
                _run_events.c.type,
                _run_events.c.data_json,
                _run_events.c.tool_name,
                _run_events.c.error_class,
            )
            .where(_run_events.c.run_id == run_id)
            .where(_run_events.c.seq > after_seq)
            .order_by(_run_events.c.seq.asc())
            .limit(limit)
        )
        rows = (await self._session.execute(stmt)).mappings().all()
        return [RunEvent(**row) for row in rows]

    async def _allocate_seq(self, *, run_id: uuid.UUID) -> int:
        stmt = (
            sa.update(_runs)
            .where(_runs.c.id == run_id)
            .values(next_event_seq=_runs.c.next_event_seq + 1)
            .returning(_runs.c.next_event_seq)
        )
        row = (await self._session.execute(stmt)).one_or_none()
        if row is None:
            raise RunNotFoundError(run_id=run_id)
        next_seq = int(row[0])
        return next_seq - 1

    async def _insert_event(
        self,
        *,
        run_id: uuid.UUID,
        ts: datetime | None = None,
        type: str,
        data_json: Any,
        tool_name: str | None = None,
        error_class: str | None = None,
    ) -> RunEvent:
        seq = await self._allocate_seq(run_id=run_id)
        values: dict[str, Any] = {
            "run_id": run_id,
            "seq": seq,
            "type": type,
            "data_json": data_json,
            "tool_name": tool_name,
            "error_class": error_class,
        }
        if ts is not None:
            values["ts"] = ts
        stmt = (
            sa.insert(_run_events)
            .values(values)
            .returning(
                _run_events.c.event_id,
                _run_events.c.run_id,
                _run_events.c.seq,
                _run_events.c.ts,
                _run_events.c.type,
                _run_events.c.data_json,
                _run_events.c.tool_name,
                _run_events.c.error_class,
            )
        )
        try:
            row = (await self._session.execute(stmt)).mappings().one()
        except IntegrityError as exc:
            if _is_unique_violation(exc, constraint="uq_run_events_run_id_seq"):
                raise RunEventSeqConflictError(run_id=run_id, seq=seq) from exc
            raise
        return RunEvent(**row)


__all__ = [
    "Run",
    "RunEvent",
    "RunEventRepository",
    "RunEventSeqConflictError",
    "RunNotFoundError",
    "SqlAlchemyRunEventRepository",
]
