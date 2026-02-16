from __future__ import annotations

from datetime import datetime
from typing import Any, Mapping, Sequence
import uuid

import sqlalchemy as sa
from sqlalchemy.dialects import postgresql
from sqlalchemy.ext.asyncio import AsyncSession

from packages.observability.context import new_trace_id, normalize_trace_id

from .protocol import (
    JOB_PAYLOAD_VERSION_V1,
    JOB_STATUS_DEAD,
    JOB_STATUS_DONE,
    JOB_STATUS_LEASED,
    JOB_STATUS_QUEUED,
    RUN_EXECUTE_JOB_TYPE,
    JobLease,
    JobLeaseLostError,
    JobQueue,
    default_retry_delay_seconds,
)

_metadata = sa.MetaData()

_jobs = sa.Table(
    "jobs",
    _metadata,
    sa.Column(
        "id",
        postgresql.UUID(as_uuid=True),
        primary_key=True,
        server_default=sa.text("gen_random_uuid()"),
    ),
    sa.Column("job_type", sa.Text(), nullable=False),
    sa.Column(
        "payload_json",
        postgresql.JSONB(astext_type=sa.Text()),
        nullable=False,
        server_default=sa.text("'{}'::jsonb"),
    ),
    sa.Column("status", sa.Text(), nullable=False, server_default=sa.text("'queued'")),
    sa.Column(
        "available_at",
        sa.TIMESTAMP(timezone=True),
        nullable=False,
        server_default=sa.text("now()"),
    ),
    sa.Column("leased_until", sa.TIMESTAMP(timezone=True), nullable=True),
    sa.Column("lease_token", postgresql.UUID(as_uuid=True), nullable=True),
    sa.Column("attempts", sa.Integer(), nullable=False, server_default=sa.text("0")),
    sa.Column(
        "created_at",
        sa.TIMESTAMP(timezone=True),
        nullable=False,
        server_default=sa.text("now()"),
    ),
    sa.Column(
        "updated_at",
        sa.TIMESTAMP(timezone=True),
        nullable=False,
        server_default=sa.text("now()"),
    ),
)


_LEASE_ATTEMPTS_REAP_LIMIT = 10


class SqlAlchemyPgJobQueue(JobQueue):
    def __init__(self, session: AsyncSession, *, max_attempts: int = 25) -> None:
        if max_attempts <= 0:
            raise ValueError("max_attempts 必须为正数")
        self._session = session
        self._max_attempts = max_attempts

    async def enqueue_run(
        self,
        *,
        org_id: uuid.UUID,
        run_id: uuid.UUID,
        trace_id: str | None,
        queue_job_type: str | None = None,
        payload: Mapping[str, Any] | None = None,
        available_at: datetime | None = None,
    ) -> uuid.UUID:
        job_id = uuid.uuid4()
        chosen_trace_id = normalize_trace_id(trace_id) or new_trace_id()
        chosen_job_type = (queue_job_type or "").strip() or RUN_EXECUTE_JOB_TYPE
        if not chosen_job_type:
            raise ValueError("queue_job_type 不能为空")
        # 约定：jobs.id 与 payload_json.job_id 必须一致，便于 worker 只看 payload 即可回放/审计。
        payload_json: dict[str, Any] = {
            "v": JOB_PAYLOAD_VERSION_V1,
            "job_id": str(job_id),
            "type": RUN_EXECUTE_JOB_TYPE,
            "trace_id": chosen_trace_id,
            "org_id": str(org_id),
            "run_id": str(run_id),
            "payload": dict(payload or {}),
        }
        values: dict[str, Any] = {
            "id": job_id,
            "job_type": chosen_job_type,
            "payload_json": payload_json,
            "status": JOB_STATUS_QUEUED,
            "leased_until": None,
            "attempts": 0,
            "updated_at": sa.func.now(),
        }
        if available_at is not None:
            values["available_at"] = available_at
        stmt = sa.insert(_jobs).values(values)
        await self._session.execute(stmt)
        return job_id

    async def lease(
        self,
        *,
        lease_seconds: int = 30,
        job_types: Sequence[str] | None = None,
    ) -> JobLease | None:
        if lease_seconds <= 0:
            raise ValueError("lease_seconds 必须为正数")

        chosen_job_types: tuple[str, ...] | None = None
        if job_types is not None:
            cleaned = [item.strip() for item in job_types if item and item.strip()]
            if not cleaned:
                return None
            seen: set[str] = set()
            deduped: list[str] = []
            for item in cleaned:
                if item in seen:
                    continue
                seen.add(item)
                deduped.append(item)
            chosen_job_types = tuple(deduped)

        for _ in range(_LEASE_ATTEMPTS_REAP_LIMIT):
            lease = await self._try_lease_one(lease_seconds=lease_seconds, job_types=chosen_job_types)
            if lease is not None:
                return lease
            if not await self._try_mark_dead_one(job_types=chosen_job_types):
                return None
        return None

    async def heartbeat(self, *, lease: JobLease, lease_seconds: int = 30) -> None:
        if lease_seconds <= 0:
            raise ValueError("lease_seconds 必须为正数")

        now = sa.func.now()
        lease_until = now + sa.func.make_interval(0, 0, 0, 0, 0, 0, lease_seconds)
        stmt = (
            sa.update(_jobs)
            .where(_jobs.c.id == lease.job_id)
            .where(_jobs.c.status == JOB_STATUS_LEASED)
            .where(_jobs.c.lease_token == lease.lease_token)
            .values(leased_until=lease_until, updated_at=now)
        )
        result = await self._session.execute(stmt)
        if result.rowcount != 1:
            raise JobLeaseLostError(job_id=lease.job_id)

    async def ack(self, *, lease: JobLease) -> None:
        stmt = (
            sa.update(_jobs)
            .where(_jobs.c.id == lease.job_id)
            .where(_jobs.c.status == JOB_STATUS_LEASED)
            .where(_jobs.c.lease_token == lease.lease_token)
            .values(
                status=JOB_STATUS_DONE,
                leased_until=None,
                lease_token=None,
                updated_at=sa.func.now(),
            )
        )
        result = await self._session.execute(stmt)
        if result.rowcount != 1:
            raise JobLeaseLostError(job_id=lease.job_id)

    async def nack(self, *, lease: JobLease, delay_seconds: int | None = None) -> None:
        if lease.attempts >= self._max_attempts:
            await self._dead_letter(lease=lease)
            return

        chosen_delay = delay_seconds
        if chosen_delay is None:
            chosen_delay = default_retry_delay_seconds(attempts=lease.attempts)
        if chosen_delay < 0:
            raise ValueError("delay_seconds 不能为负数")

        now = sa.func.now()
        retry_at = now + sa.func.make_interval(0, 0, 0, 0, 0, 0, chosen_delay)
        stmt = (
            sa.update(_jobs)
            .where(_jobs.c.id == lease.job_id)
            .where(_jobs.c.status == JOB_STATUS_LEASED)
            .where(_jobs.c.lease_token == lease.lease_token)
            .values(
                status=JOB_STATUS_QUEUED,
                leased_until=None,
                lease_token=None,
                available_at=retry_at,
                updated_at=now,
            )
        )
        result = await self._session.execute(stmt)
        if result.rowcount != 1:
            raise JobLeaseLostError(job_id=lease.job_id)

    async def _try_lease_one(
        self,
        *,
        lease_seconds: int,
        job_types: tuple[str, ...] | None,
    ) -> JobLease | None:
        now = sa.func.now()
        lease_until = now + sa.func.make_interval(0, 0, 0, 0, 0, 0, lease_seconds)
        candidate = self._candidate_job_cte(now=now, attempts_lt=self._max_attempts, job_types=job_types)
        stmt = (
            sa.update(_jobs)
            .where(_jobs.c.id == candidate.c.id)
            .values(
                status=JOB_STATUS_LEASED,
                leased_until=lease_until,
                lease_token=sa.func.gen_random_uuid(),
                attempts=_jobs.c.attempts + 1,
                updated_at=now,
            )
            .returning(
                _jobs.c.id.label("job_id"),
                _jobs.c.job_type,
                _jobs.c.payload_json,
                _jobs.c.attempts,
                _jobs.c.leased_until,
                _jobs.c.lease_token,
            )
        )
        row = (await self._session.execute(stmt)).mappings().one_or_none()
        if row is None:
            return None
        return JobLease(
            job_id=row["job_id"],
            job_type=row["job_type"],
            payload_json=row["payload_json"],
            attempts=int(row["attempts"]),
            leased_until=row["leased_until"],
            lease_token=row["lease_token"],
        )

    async def _try_mark_dead_one(self, *, job_types: tuple[str, ...] | None) -> bool:
        now = sa.func.now()
        candidate = self._candidate_job_cte(now=now, attempts_gte=self._max_attempts, job_types=job_types)
        stmt = (
            sa.update(_jobs)
            .where(_jobs.c.id == candidate.c.id)
            .values(
                status=JOB_STATUS_DEAD,
                leased_until=None,
                lease_token=None,
                updated_at=now,
            )
        )
        result = await self._session.execute(stmt)
        return bool(result.rowcount == 1)

    async def _dead_letter(self, *, lease: JobLease) -> None:
        stmt = (
            sa.update(_jobs)
            .where(_jobs.c.id == lease.job_id)
            .where(_jobs.c.status == JOB_STATUS_LEASED)
            .where(_jobs.c.lease_token == lease.lease_token)
            .values(
                status=JOB_STATUS_DEAD,
                leased_until=None,
                lease_token=None,
                updated_at=sa.func.now(),
            )
        )
        result = await self._session.execute(stmt)
        if result.rowcount != 1:
            raise JobLeaseLostError(job_id=lease.job_id)

    def _candidate_job_cte(
        self,
        *,
        now: sa.SQLColumnExpression[Any],
        attempts_lt: int | None = None,
        attempts_gte: int | None = None,
        job_types: tuple[str, ...] | None = None,
    ) -> sa.CTE:
        if attempts_lt is not None and attempts_gte is not None:
            raise ValueError("attempts_lt 与 attempts_gte 不能同时设置")

        conditions: list[sa.ColumnElement[bool]] = [
            sa.or_(
                sa.and_(
                    _jobs.c.status == JOB_STATUS_QUEUED,
                    _jobs.c.available_at <= now,
                ),
                sa.and_(
                    _jobs.c.status == JOB_STATUS_LEASED,
                    _jobs.c.leased_until.is_not(None),
                    _jobs.c.leased_until <= now,
                ),
            )
        ]
        if attempts_lt is not None:
            conditions.append(_jobs.c.attempts < attempts_lt)
        if attempts_gte is not None:
            conditions.append(_jobs.c.attempts >= attempts_gte)
        if job_types is not None:
            conditions.append(_jobs.c.job_type.in_(job_types))

        return (
            sa.select(_jobs.c.id)
            .where(sa.and_(*conditions))
            .order_by(_jobs.c.available_at.asc(), _jobs.c.created_at.asc(), _jobs.c.id.asc())
            .with_for_update(skip_locked=True)
            .limit(1)
            .cte("candidate")
        )


__all__ = ["SqlAlchemyPgJobQueue"]
