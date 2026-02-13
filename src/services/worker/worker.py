from __future__ import annotations

import logging
from typing import Mapping, Protocol
import uuid

from packages.data import Database
from packages.data.runs import SqlAlchemyRunEventRepository
from packages.job_queue import RUN_EXECUTE_JOB_TYPE
from packages.observability.worker import worker_job_context

from .job_payload import WorkerJobPayload


class RunEngine(Protocol):
    async def execute(self, *, run_id: uuid.UUID, trace_id: str) -> None: ...


class Worker:
    def __init__(self, *, database: Database, engine: RunEngine | None = None) -> None:
        self._database = database
        self._engine = engine
        self._logger = logging.getLogger("arkloop.worker")

    async def handle_job(self, payload_json: Mapping[str, object]) -> None:
        job = WorkerJobPayload.from_json(payload_json)
        with worker_job_context(
            trace_id=job.trace_id,
            org_id=str(job.org_id),
            run_id=str(job.run_id),
        ) as trace_id:
            self._logger.info(
                "收到 job",
                extra={
                    "job_id": str(job.job_id),
                    "job_type": job.job_type,
                    "org_id": str(job.org_id),
                    "run_id": str(job.run_id),
                },
            )
            async with self._database.sessionmaker() as session:
                repo = SqlAlchemyRunEventRepository(session)
                run = await repo.get_run(run_id=job.run_id)
                if run is None:
                    raise LookupError("Run 不存在")
                if run.org_id != job.org_id:
                    raise PermissionError("job.org_id 与 run.org_id 不一致")

                terminal = await repo.get_latest_event_type(
                    run_id=job.run_id,
                    types=("run.completed", "run.failed", "run.cancelled"),
                )
                if terminal is not None:
                    self._logger.info(
                        "run 已终态，跳过执行",
                        extra={
                            "job_id": str(job.job_id),
                            "job_type": job.job_type,
                            "org_id": str(job.org_id),
                            "run_id": str(job.run_id),
                            "terminal_type": terminal,
                        },
                    )
                    return

                await repo.append_event(
                    run_id=job.run_id,
                    type="worker.job.received",
                    data_json={
                        "trace_id": trace_id,
                        "job_id": str(job.job_id),
                        "job_type": job.job_type,
                        "org_id": str(job.org_id),
                    },
                )
                await session.commit()

            if job.job_type == RUN_EXECUTE_JOB_TYPE:
                if self._engine is None:
                    raise RuntimeError("RunEngine 未配置")
                await self._engine.execute(run_id=job.run_id, trace_id=trace_id)


__all__ = ["Worker"]

