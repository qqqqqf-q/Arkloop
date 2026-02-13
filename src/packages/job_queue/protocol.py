from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass
from datetime import datetime
from typing import Any, Mapping
import uuid

RUN_EXECUTE_JOB_TYPE = "run.execute"

JOB_STATUS_QUEUED = "queued"
JOB_STATUS_LEASED = "leased"
JOB_STATUS_DONE = "done"
JOB_STATUS_DEAD = "dead"

JOB_PAYLOAD_VERSION_V1 = 1

_RETRY_BASE_SECONDS = 1
_RETRY_MAX_SECONDS = 30
_RETRY_MAX_EXPONENT = 5


def default_retry_delay_seconds(*, attempts: int) -> int:
    if attempts <= 0:
        return _RETRY_BASE_SECONDS
    exponent = min(attempts - 1, _RETRY_MAX_EXPONENT)
    seconds = _RETRY_BASE_SECONDS * (2**exponent)
    return min(seconds, _RETRY_MAX_SECONDS)


@dataclass(frozen=True, slots=True)
class JobLease:
    job_id: uuid.UUID
    job_type: str
    payload_json: Mapping[str, Any]
    attempts: int
    leased_until: datetime
    lease_token: uuid.UUID


class JobLeaseLostError(RuntimeError):
    def __init__(self, *, job_id: uuid.UUID) -> None:
        super().__init__("job lease 已丢失")
        self.job_id = job_id


class JobQueue(ABC):
    @abstractmethod
    async def enqueue_run(
        self,
        *,
        org_id: uuid.UUID,
        run_id: uuid.UUID,
        trace_id: str | None,
        payload: Mapping[str, Any] | None = None,
        available_at: datetime | None = None,
    ) -> uuid.UUID: ...

    @abstractmethod
    async def lease(self, *, lease_seconds: int = 30) -> JobLease | None: ...

    @abstractmethod
    async def heartbeat(self, *, lease: JobLease, lease_seconds: int = 30) -> None: ...

    @abstractmethod
    async def ack(self, *, lease: JobLease) -> None: ...

    @abstractmethod
    async def nack(self, *, lease: JobLease, delay_seconds: int | None = None) -> None: ...


__all__ = [
    "JOB_PAYLOAD_VERSION_V1",
    "JOB_STATUS_DONE",
    "JOB_STATUS_DEAD",
    "JOB_STATUS_LEASED",
    "JOB_STATUS_QUEUED",
    "RUN_EXECUTE_JOB_TYPE",
    "JobLease",
    "JobLeaseLostError",
    "JobQueue",
    "default_retry_delay_seconds",
]
