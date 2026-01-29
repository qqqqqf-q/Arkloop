from __future__ import annotations

import asyncio
from dataclasses import dataclass
import logging
import os
from typing import Protocol
import uuid

from fastapi import FastAPI, Request

from packages.config import load_dotenv_if_enabled
from packages.data import Database
from packages.data.runs import RunNotFoundError, SqlAlchemyRunEventRepository
from packages.observability.context import new_trace_id, trace_id_context

from .error_envelope import ApiError

_STUB_ENABLED_ENV = "ARKLOOP_STUB_AGENT_ENABLED"
_STUB_DELTA_COUNT_ENV = "ARKLOOP_STUB_AGENT_DELTA_COUNT"
_STUB_DELTA_INTERVAL_SECONDS_ENV = "ARKLOOP_STUB_AGENT_DELTA_INTERVAL_SECONDS"

_DEFAULT_STUB_ENABLED = True
_DEFAULT_STUB_DELTA_COUNT = 3
_DEFAULT_STUB_DELTA_INTERVAL_SECONDS = 0.02


def _parse_bool(value: str) -> bool:
    cleaned = value.strip().casefold()
    if cleaned in {"1", "true", "yes", "y", "on"}:
        return True
    if cleaned in {"0", "false", "no", "n", "off"}:
        return False
    raise ValueError("必须为布尔值（0/1、true/false）")


def _parse_positive_int(value: str) -> int:
    cleaned = value.strip()
    parsed = int(cleaned)
    if parsed <= 0:
        raise ValueError("必须为正整数")
    return parsed


def _parse_non_negative_float(value: str) -> float:
    cleaned = value.strip()
    parsed = float(cleaned)
    if parsed < 0:
        raise ValueError("必须为非负数")
    return parsed


@dataclass(frozen=True, slots=True)
class StubAgentConfig:
    enabled: bool = _DEFAULT_STUB_ENABLED
    delta_count: int = _DEFAULT_STUB_DELTA_COUNT
    delta_interval_seconds: float = _DEFAULT_STUB_DELTA_INTERVAL_SECONDS

    @classmethod
    def from_env(cls) -> "StubAgentConfig":
        load_dotenv_if_enabled(override=False)

        enabled = _DEFAULT_STUB_ENABLED
        raw_enabled = os.getenv(_STUB_ENABLED_ENV)
        if raw_enabled:
            enabled = _parse_bool(raw_enabled)

        delta_count = _DEFAULT_STUB_DELTA_COUNT
        raw_delta_count = os.getenv(_STUB_DELTA_COUNT_ENV)
        if raw_delta_count:
            delta_count = _parse_positive_int(raw_delta_count)

        delta_interval_seconds = _DEFAULT_STUB_DELTA_INTERVAL_SECONDS
        raw_delta_interval_seconds = os.getenv(_STUB_DELTA_INTERVAL_SECONDS_ENV)
        if raw_delta_interval_seconds:
            delta_interval_seconds = _parse_non_negative_float(raw_delta_interval_seconds)

        return cls(
            enabled=enabled,
            delta_count=delta_count,
            delta_interval_seconds=delta_interval_seconds,
        )


class RunExecutor(Protocol):
    def enqueue(self, *, run_id: uuid.UUID) -> None: ...

    async def start(self) -> None: ...

    async def stop(self) -> None: ...


class InProcessStubRunExecutor(RunExecutor):
    def __init__(self, *, database: Database, config: StubAgentConfig) -> None:
        self._database = database
        self._config = config
        self._queue: asyncio.Queue[uuid.UUID] = asyncio.Queue()
        self._task: asyncio.Task[None] | None = None
        self._logger = logging.getLogger("arkloop.stub_agent")

    def enqueue(self, *, run_id: uuid.UUID) -> None:
        if not self._config.enabled:
            return
        self._queue.put_nowait(run_id)

    async def start(self) -> None:
        if self._task is not None or not self._config.enabled:
            return
        self._task = asyncio.create_task(self._run_loop(), name="arkloop.stub_agent")

    async def stop(self) -> None:
        if self._task is None:
            return
        self._task.cancel()
        try:
            await self._task
        except asyncio.CancelledError:
            pass
        self._task = None

    async def _run_loop(self) -> None:
        while True:
            run_id = await self._queue.get()
            try:
                await self._execute(run_id=run_id)
            except Exception:
                self._logger.exception("stub agent 执行失败", extra={"run_id": str(run_id)})
            finally:
                self._queue.task_done()

    async def _execute(self, *, run_id: uuid.UUID) -> None:
        trace_id = new_trace_id()
        with trace_id_context(trace_id):
            self._logger.info("stub agent 开始", extra={"run_id": str(run_id)})
            try:
                async with self._database.sessionmaker() as session:
                    repo = SqlAlchemyRunEventRepository(session)
                    for index in range(1, self._config.delta_count + 1):
                        await asyncio.sleep(self._config.delta_interval_seconds)
                        await repo.append_event(
                            run_id=run_id,
                            type="message.delta",
                            data_json={
                                "content_delta": f"stub delta {index}",
                                "role": "assistant",
                            },
                        )
                    await repo.append_event(run_id=run_id, type="run.completed", data_json={})
                    await session.commit()
            except RunNotFoundError:
                self._logger.warning("run 不存在，跳过", extra={"run_id": str(run_id)})
                return
            self._logger.info("stub agent 完成", extra={"run_id": str(run_id)})


def install_run_executor(app: FastAPI, executor: RunExecutor) -> None:
    app.state.run_executor = executor


def _get_installed_run_executor(app: FastAPI) -> RunExecutor:
    executor = getattr(app.state, "run_executor", None)
    if executor is None:
        raise ApiError(code="run_executor.not_configured", message="RunExecutor 未配置", status_code=503)
    return executor


def get_run_executor(request: Request) -> RunExecutor:
    return _get_installed_run_executor(request.app)


def configure_run_executor(app: FastAPI) -> None:
    database = getattr(app.state, "database", None)
    if not isinstance(database, Database):
        return
    install_run_executor(
        app,
        InProcessStubRunExecutor(database=database, config=StubAgentConfig.from_env()),
    )


__all__ = [
    "InProcessStubRunExecutor",
    "RunExecutor",
    "StubAgentConfig",
    "configure_run_executor",
    "get_run_executor",
    "install_run_executor",
]

