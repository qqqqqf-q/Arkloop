from __future__ import annotations

import asyncio
from collections.abc import Callable, Mapping
from dataclasses import dataclass
import logging
import os
from typing import Optional
import uuid

import sqlalchemy as sa
from sqlalchemy.ext.asyncio import AsyncSession

from packages.config import load_dotenv_if_enabled
from packages.data import Database
from packages.job_queue import RUN_EXECUTE_JOB_TYPE, JobLease, JobLeaseLostError, JobQueue

from .worker import Worker

JobQueueFactory = Callable[[AsyncSession], JobQueue]

_WORKER_CONCURRENCY_ENV = "ARKLOOP_WORKER_CONCURRENCY"
_WORKER_POLL_SECONDS_ENV = "ARKLOOP_WORKER_POLL_SECONDS"
_WORKER_LEASE_SECONDS_ENV = "ARKLOOP_WORKER_LEASE_SECONDS"
_WORKER_HEARTBEAT_SECONDS_ENV = "ARKLOOP_WORKER_HEARTBEAT_SECONDS"

_HEARTBEAT_MAX_CONSECUTIVE_ERRORS = 3


def _parse_positive_int(raw: str, *, label: str) -> int:
    cleaned = raw.strip()
    value = int(cleaned)
    if value <= 0:
        raise ValueError(f"{label} 必须为正整数")
    return value


def _parse_non_negative_float(raw: str, *, label: str) -> float:
    cleaned = raw.strip()
    value = float(cleaned)
    if value < 0:
        raise ValueError(f"{label} 必须为非负数")
    return value


def _advisory_lock_key(run_id: uuid.UUID) -> int:
    value = run_id.int & 0xFFFFFFFFFFFFFFFF
    if value >= 2**63:
        value -= 2**64
    return int(value)


def _extract_uuid(payload_json: Mapping[str, object], key: str) -> uuid.UUID | None:
    raw = payload_json.get(key)
    if not isinstance(raw, str) or not raw.strip():
        return None
    try:
        return uuid.UUID(raw.strip())
    except ValueError:
        return None


@dataclass(frozen=True, slots=True)
class WorkerLoopConfig:
    concurrency: int = 4
    poll_seconds: float = 0.25
    lease_seconds: int = 30
    heartbeat_seconds: float = 10.0

    @classmethod
    def from_env(cls) -> "WorkerLoopConfig":
        load_dotenv_if_enabled(override=False)

        defaults = cls()

        concurrency = defaults.concurrency
        raw_concurrency = os.getenv(_WORKER_CONCURRENCY_ENV)
        if raw_concurrency:
            concurrency = _parse_positive_int(raw_concurrency, label=_WORKER_CONCURRENCY_ENV)

        poll_seconds = defaults.poll_seconds
        raw_poll = os.getenv(_WORKER_POLL_SECONDS_ENV)
        if raw_poll:
            poll_seconds = _parse_non_negative_float(raw_poll, label=_WORKER_POLL_SECONDS_ENV)

        lease_seconds = defaults.lease_seconds
        raw_lease = os.getenv(_WORKER_LEASE_SECONDS_ENV)
        if raw_lease:
            lease_seconds = _parse_positive_int(raw_lease, label=_WORKER_LEASE_SECONDS_ENV)

        heartbeat_seconds = defaults.heartbeat_seconds
        raw_heartbeat = os.getenv(_WORKER_HEARTBEAT_SECONDS_ENV)
        if raw_heartbeat:
            heartbeat_seconds = _parse_non_negative_float(raw_heartbeat, label=_WORKER_HEARTBEAT_SECONDS_ENV)

        return cls(
            concurrency=concurrency,
            poll_seconds=poll_seconds,
            lease_seconds=lease_seconds,
            heartbeat_seconds=heartbeat_seconds,
        )


class WorkerConsumerLoop:
    def __init__(
        self,
        *,
        database: Database,
        job_queue_factory: JobQueueFactory,
        worker: Worker,
        config: WorkerLoopConfig,
    ) -> None:
        if config.concurrency <= 0:
            raise ValueError("concurrency 必须为正整数")
        if config.poll_seconds < 0:
            raise ValueError("poll_seconds 必须为非负数")
        if config.lease_seconds <= 0:
            raise ValueError("lease_seconds 必须为正整数")
        if config.heartbeat_seconds < 0:
            raise ValueError("heartbeat_seconds 必须为非负数")

        self._database = database
        self._job_queue_factory = job_queue_factory
        self._worker = worker
        self._config = config
        self._semaphore = asyncio.Semaphore(config.concurrency)
        self._tasks: set[asyncio.Task[None]] = set()
        self._logger = logging.getLogger("arkloop.worker")
        self._stop = asyncio.Event()

    def request_stop(self) -> None:
        self._stop.set()

    async def run_forever(self) -> None:
        try:
            while not self._stop.is_set():
                await self._semaphore.acquire()
                lease = await self._lease_one()
                if lease is None:
                    self._semaphore.release()
                    if self._config.poll_seconds > 0:
                        try:
                            await asyncio.wait_for(self._stop.wait(), timeout=self._config.poll_seconds)
                        except asyncio.TimeoutError:
                            pass
                    continue

                task = asyncio.create_task(self._process_lease(lease), name=f"worker.job:{lease.job_id}")
                self._tasks.add(task)
                task.add_done_callback(self._tasks.discard)
        except asyncio.CancelledError:
            self._stop.set()
            raise
        finally:
            await self._cancel_outstanding_tasks()

    async def run_once(self) -> bool:
        await self._semaphore.acquire()
        lease = await self._lease_one()
        if lease is None:
            self._semaphore.release()
            return False
        await self._process_lease(lease)
        return True

    async def _lease_one(self) -> Optional[JobLease]:
        try:
            async with self._database.sessionmaker() as session:
                queue = self._job_queue_factory(session)
                lease = await queue.lease(lease_seconds=self._config.lease_seconds)
                await session.commit()
                return lease
        except Exception:
            self._logger.exception("lease job 失败")
            return None

    async def _process_lease(self, lease: JobLease) -> None:
        lock_conn: sa.ext.asyncio.AsyncConnection | None = None
        run_id = _extract_uuid(lease.payload_json, "run_id")
        lock_key: int | None = None
        if lease.job_type == RUN_EXECUTE_JOB_TYPE and run_id is not None:
            lock_key = _advisory_lock_key(run_id)

        heartbeat_task: asyncio.Task[str] | None = None
        job_task: asyncio.Task[None] | None = None
        try:
            if lock_key is not None:
                lock_conn = await self._database.engine.connect()
                acquired = await self._try_acquire_run_lock(lock_conn, lock_key=lock_key)
                if not acquired:
                    self._logger.info(
                        "run 正在执行，延后重试",
                        extra={"job_id": str(lease.job_id), "run_id": str(run_id)},
                    )
                    await lock_conn.close()
                    lock_conn = None
                    await self._nack(lease)
                    return

            heartbeat_task = self._start_heartbeat(lease)
            job_task = asyncio.create_task(
                self._worker.handle_job(lease.payload_json), name=f"worker.handle_job:{lease.job_id}"
            )

            if heartbeat_task is None:
                await self._settle_job(lease=lease, job_task=job_task)
                return

            done, _pending = await asyncio.wait(
                {job_task, heartbeat_task}, return_when=asyncio.FIRST_COMPLETED
            )
            if job_task in done:
                await self._settle_job(lease=lease, job_task=job_task)
                return

            reason = heartbeat_task.result()
            job_task.cancel()
            try:
                await job_task
            except asyncio.CancelledError:
                pass
            except Exception:
                pass

            if reason == "lease_lost":
                return

            self._logger.error(
                "heartbeat 连续失败，已停止当前 job",
                extra={"job_id": str(lease.job_id), "job_type": lease.job_type},
            )
            await self._nack(lease)
        finally:
            if lock_conn is not None and lock_key is not None:
                await self._release_run_lock(lock_conn, lock_key=lock_key)
                try:
                    await lock_conn.close()
                except Exception:
                    self._logger.exception("run advisory lock 连接关闭失败")

            if heartbeat_task is not None:
                heartbeat_task.cancel()
                try:
                    await heartbeat_task
                except asyncio.CancelledError:
                    pass
                except Exception:
                    self._logger.exception(
                        "heartbeat task 退出异常",
                        extra={"job_id": str(lease.job_id), "job_type": lease.job_type},
                    )
            self._semaphore.release()

    async def _settle_job(self, *, lease: JobLease, job_task: asyncio.Task[None]) -> None:
        try:
            await job_task
            await self._ack(lease)
        except (LookupError, PermissionError) as exc:
            self._logger.warning(
                "job 不可处理，直接 ack",
                extra={"job_id": str(lease.job_id), "job_type": lease.job_type, "error": str(exc)},
            )
            await self._ack(lease)
        except asyncio.CancelledError:
            raise
        except Exception:
            self._logger.exception(
                "job 执行失败，将 nack 重试",
                extra={"job_id": str(lease.job_id), "job_type": lease.job_type},
            )
            await self._nack(lease)

    async def _try_acquire_run_lock(self, conn: sa.ext.asyncio.AsyncConnection, *, lock_key: int) -> bool:
        result = await conn.execute(
            sa.text("SELECT pg_try_advisory_lock(:lock_key) AS acquired"),
            {"lock_key": lock_key},
        )
        return bool(result.scalar_one())

    async def _release_run_lock(self, conn: sa.ext.asyncio.AsyncConnection, *, lock_key: int) -> None:
        try:
            await conn.execute(
                sa.text("SELECT pg_advisory_unlock(:lock_key)"),
                {"lock_key": lock_key},
            )
        except Exception:
            self._logger.exception("run advisory lock 释放失败")
            try:
                await conn.invalidate()
            except Exception:
                self._logger.exception("run advisory lock 连接 invalidate 失败")

    def _start_heartbeat(self, lease: JobLease) -> asyncio.Task[str] | None:
        if self._config.heartbeat_seconds <= 0:
            return None
        if self._config.heartbeat_seconds >= self._config.lease_seconds:
            self._logger.warning(
                "heartbeat_seconds 不应大于等于 lease_seconds，已自动禁用",
                extra={
                    "heartbeat_seconds": self._config.heartbeat_seconds,
                    "lease_seconds": self._config.lease_seconds,
                },
            )
            return None
        return asyncio.create_task(self._heartbeat_loop(lease), name=f"worker.heartbeat:{lease.job_id}")

    async def _heartbeat_loop(self, lease: JobLease) -> str:
        consecutive_errors = 0
        while True:
            await asyncio.sleep(self._config.heartbeat_seconds)
            try:
                async with self._database.sessionmaker() as session:
                    queue = self._job_queue_factory(session)
                    await queue.heartbeat(lease=lease, lease_seconds=self._config.lease_seconds)
                    await session.commit()
                consecutive_errors = 0
            except JobLeaseLostError:
                self._logger.warning(
                    "job lease 已丢失，停止 heartbeat",
                    extra={"job_id": str(lease.job_id), "job_type": lease.job_type},
                )
                return "lease_lost"
            except Exception:
                consecutive_errors += 1
                self._logger.exception(
                    "job heartbeat 失败",
                    extra={"job_id": str(lease.job_id), "job_type": lease.job_type},
                )
                if consecutive_errors >= _HEARTBEAT_MAX_CONSECUTIVE_ERRORS:
                    return "too_many_errors"

    async def _ack(self, lease: JobLease) -> None:
        try:
            async with self._database.sessionmaker() as session:
                queue = self._job_queue_factory(session)
                await queue.ack(lease=lease)
                await session.commit()
        except JobLeaseLostError:
            self._logger.warning(
                "ack 失败：lease 已丢失",
                extra={"job_id": str(lease.job_id), "job_type": lease.job_type},
            )
        except Exception:
            self._logger.exception(
                "ack 失败",
                extra={"job_id": str(lease.job_id), "job_type": lease.job_type},
            )

    async def _nack(self, lease: JobLease, *, delay_seconds: int | None = None) -> None:
        try:
            async with self._database.sessionmaker() as session:
                queue = self._job_queue_factory(session)
                await queue.nack(lease=lease, delay_seconds=delay_seconds)
                await session.commit()
        except JobLeaseLostError:
            self._logger.warning(
                "nack 失败：lease 已丢失",
                extra={"job_id": str(lease.job_id), "job_type": lease.job_type},
            )
        except Exception:
            self._logger.exception(
                "nack 失败",
                extra={"job_id": str(lease.job_id), "job_type": lease.job_type},
            )

    async def _cancel_outstanding_tasks(self) -> None:
        if not self._tasks:
            return
        for task in list(self._tasks):
            task.cancel()
        results = await asyncio.gather(*self._tasks, return_exceptions=True)
        for result in results:
            if isinstance(result, Exception) and not isinstance(result, asyncio.CancelledError):
                self._logger.exception("worker task 退出异常", exc_info=result)
        self._tasks.clear()


__all__ = ["JobQueueFactory", "WorkerConsumerLoop", "WorkerLoopConfig"]
