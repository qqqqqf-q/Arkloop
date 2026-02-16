from __future__ import annotations

import asyncio
import logging
import os
from typing import TYPE_CHECKING, Callable, Protocol
import uuid

from fastapi import FastAPI, Request
from sqlalchemy.ext.asyncio import AsyncSession

from packages.data import Database
from packages.data.runs import RunNotFoundError
from packages.job_queue import (
    RUN_EXECUTE_JOB_TYPE,
    RUN_EXECUTE_QUEUE_JOB_TYPE_GO_BRIDGE,
    JobQueue,
)
from packages.observability.context import new_trace_id, trace_id_context

from .error_envelope import ApiError

_TOOL_ALLOWLIST_ENV = "ARKLOOP_TOOL_ALLOWLIST"
_RUN_EXECUTOR_ENV = "ARKLOOP_RUN_EXECUTOR"
_RUN_EXECUTOR_IN_PROCESS = "in_process"
_RUN_EXECUTOR_WORKER = "worker"
_WORKER_GO_TRAFFIC_PERCENT_ENV = "ARKLOOP_WORKER_GO_TRAFFIC_PERCENT"

if TYPE_CHECKING:
    from packages.llm_gateway import ToolSpec as LlmToolSpec


class StubAgentConfig(Protocol):
    enabled: bool


class RunExecutor(Protocol):
    async def enqueue(
        self,
        *,
        org_id: uuid.UUID,
        run_id: uuid.UUID,
        trace_id: str | None,
        session: AsyncSession | None = None,
    ) -> None: ...

    async def start(self) -> None: ...

    async def stop(self) -> None: ...


JobQueueFactory = Callable[[AsyncSession], JobQueue]


class QueuedRunExecutor(RunExecutor):
    def __init__(self, *, database: Database, job_queue_factory: JobQueueFactory) -> None:
        self._database = database
        self._job_queue_factory = job_queue_factory
        self._logger = logging.getLogger("arkloop.api")

    async def enqueue(
        self,
        *,
        org_id: uuid.UUID,
        run_id: uuid.UUID,
        trace_id: str | None,
        session: AsyncSession | None = None,
    ) -> None:
        queue_job_type = _select_queue_job_type(run_id=run_id)

        async def _enqueue(target: AsyncSession) -> uuid.UUID:
            queue = self._job_queue_factory(target)
            return await queue.enqueue_run(
                org_id=org_id,
                run_id=run_id,
                trace_id=trace_id,
                queue_job_type=queue_job_type,
                payload={"source": "api"},
            )

        if session is None:
            async with self._database.sessionmaker() as new_session:
                job_id = await _enqueue(new_session)
                await new_session.commit()
        else:
            job_id = await _enqueue(session)
        self._logger.info(
            "run job 已投递",
            extra={
                "job_id": str(job_id),
                "org_id": str(org_id),
                "run_id": str(run_id),
                "queue_job_type": queue_job_type,
            },
        )

    async def start(self) -> None:
        return

    async def stop(self) -> None:
        return


class _RunEngine(Protocol):
    async def execute(self, *, run_id: uuid.UUID, trace_id: str) -> None: ...


class InProcessStubRunExecutor(RunExecutor):
    def __init__(self, *, engine: _RunEngine, config: StubAgentConfig) -> None:
        self._engine = engine
        self._config = config
        self._queue: asyncio.Queue[uuid.UUID] = asyncio.Queue()
        self._task: asyncio.Task[None] | None = None
        self._logger = logging.getLogger("arkloop.stub_agent")

    async def enqueue(
        self,
        *,
        org_id: uuid.UUID,
        run_id: uuid.UUID,
        trace_id: str | None,
        session: AsyncSession | None = None,
    ) -> None:
        _ = org_id
        _ = trace_id
        _ = session
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
                await self._engine.execute(run_id=run_id, trace_id=trace_id)
            except RunNotFoundError:
                self._logger.warning("run 不存在，跳过", extra={"run_id": str(run_id)})
                return
            self._logger.info("stub agent 完成", extra={"run_id": str(run_id)})


def install_run_executor(app: FastAPI, executor: RunExecutor) -> None:
    app.state.run_executor = executor


def _get_installed_run_executor(app: FastAPI) -> RunExecutor:
    executor = getattr(app.state, "run_executor", None)
    if executor is None:
        raise ApiError(
            code="run_executor.not_configured", message="RunExecutor 未配置", status_code=503
        )
    return executor


def get_run_executor(request: Request) -> RunExecutor:
    return _get_installed_run_executor(request.app)


def configure_run_executor(app: FastAPI) -> None:
    database = getattr(app.state, "database", None)
    if not isinstance(database, Database):
        return

    mode = _parse_run_executor_mode()
    if mode == _RUN_EXECUTOR_WORKER:
        factory = getattr(app.state, "job_queue_factory", None)
        if not callable(factory):
            raise RuntimeError("job_queue_factory 未配置，无法启用 worker RunExecutor")
        install_run_executor(app, QueuedRunExecutor(database=database, job_queue_factory=factory))
        return

    if mode == _RUN_EXECUTOR_IN_PROCESS:
        install_run_executor(app, _create_in_process_executor(database=database))
        return

    raise RuntimeError(f"未知 RunExecutor 模式: {mode}")


def _parse_run_executor_mode() -> str:
    raw = (os.getenv(_RUN_EXECUTOR_ENV) or "").strip()
    if not raw:
        return _RUN_EXECUTOR_WORKER
    cleaned = raw.casefold().replace("-", "_")
    if cleaned in {"worker", "queued"}:
        return _RUN_EXECUTOR_WORKER
    if cleaned in {"in_process", "inprocess", "inproc"}:
        return _RUN_EXECUTOR_IN_PROCESS
    raise ValueError(f"{_RUN_EXECUTOR_ENV} 必须为 worker 或 in_process")


def _parse_worker_go_traffic_percent() -> int:
    raw = (os.getenv(_WORKER_GO_TRAFFIC_PERCENT_ENV) or "").strip()
    if not raw:
        return 0
    value = int(raw)
    if value < 0 or value > 100:
        raise ValueError(f"{_WORKER_GO_TRAFFIC_PERCENT_ENV} 必须在 0~100 之间")
    return value


def _select_queue_job_type(*, run_id: uuid.UUID) -> str:
    percent = _parse_worker_go_traffic_percent()
    if percent <= 0:
        return RUN_EXECUTE_JOB_TYPE
    if percent >= 100:
        return RUN_EXECUTE_QUEUE_JOB_TYPE_GO_BRIDGE

    bucket = int(run_id.int % 100)
    if bucket < percent:
        return RUN_EXECUTE_QUEUE_JOB_TYPE_GO_BRIDGE
    return RUN_EXECUTE_JOB_TYPE


def _create_in_process_executor(*, database: Database) -> InProcessStubRunExecutor:
    from packages.agent_core import (
        DispatchingToolExecutor,
        ToolAllowlist,
        ToolPolicyEnforcer,
        ToolRegistry,
    )
    from packages.agent_core.builtin_tools import (
        builtin_agent_tool_specs,
        builtin_llm_tool_specs,
        create_builtin_tool_executors,
    )
    from packages.llm_gateway.stub import StubLlmGateway, StubLlmGatewayConfig
    from packages.llm_routing import ProviderRouter, ProviderRoutingConfig

    from .provider_routed_runner import (
        AlwaysDisabledOrgByokPolicy,
        EnvProviderGatewayFactory,
        ProviderRoutedAgentRunner,
    )
    from .run_engine import RunEngine

    stub_config = StubLlmGatewayConfig.from_env()
    stub_gateway = StubLlmGateway(config=stub_config)
    routing_config = ProviderRoutingConfig.from_env()
    router = ProviderRouter(config=routing_config)

    tool_registry = ToolRegistry(specs=builtin_agent_tool_specs())
    from packages.mcp import load_mcp_tool_registration_from_env

    mcp_registration = load_mcp_tool_registration_from_env()
    for spec in mcp_registration.agent_specs:
        tool_registry.register(spec)
    tool_allowlist_names = _parse_tool_allowlist_names()
    _warn_unknown_tool_allowlist_names(
        allowlist_names=tool_allowlist_names,
        known_names=tool_registry.list_names(),
    )
    tool_allowlist = ToolAllowlist.from_names(tool_allowlist_names)
    tool_policy_enforcer = ToolPolicyEnforcer(
        registry=tool_registry,
        allowlist=tool_allowlist,
    )
    executors = dict(create_builtin_tool_executors())
    executors.update(mcp_registration.executors)
    tool_executor = DispatchingToolExecutor(
        registry=tool_registry,
        policy_enforcer=tool_policy_enforcer,
        executors=executors,
    )

    allowed_llm_tool_specs = _select_llm_tool_specs(
        allowed_names=set(tool_allowlist_names),
        specs=builtin_llm_tool_specs() + mcp_registration.llm_specs,
    )
    runner = ProviderRoutedAgentRunner(
        database=database,
        router=router,
        byok_policy=AlwaysDisabledOrgByokPolicy(),
        gateway_factory=EnvProviderGatewayFactory(stub_gateway=stub_gateway),
        tool_executor=tool_executor,
        tool_specs=allowed_llm_tool_specs,
    )
    from packages.skill_runtime import SkillRunner, builtin_skills_root, load_skill_registry

    skill_registry = load_skill_registry(builtin_skills_root())
    runner = SkillRunner(
        base_runner=runner,
        registry=skill_registry,
        tool_registry=tool_registry,
        tool_executors=executors,
        base_tool_allowlist_names=frozenset(tool_allowlist_names),
    )
    engine = RunEngine(database=database, runner=runner)
    return InProcessStubRunExecutor(engine=engine, config=stub_config)


def _parse_tool_allowlist_names() -> list[str]:
    raw = (os.getenv(_TOOL_ALLOWLIST_ENV) or "").strip()
    if not raw:
        return []
    items = [item.strip() for item in raw.split(",")]
    deduped: list[str] = []
    seen: set[str] = set()
    for item in items:
        if not item or item in seen:
            continue
        seen.add(item)
        deduped.append(item)
    return deduped


def _select_llm_tool_specs(
    *,
    allowed_names: set[str],
    specs: tuple[LlmToolSpec, ...],
) -> tuple[LlmToolSpec, ...]:
    if not allowed_names:
        return ()
    selected = [spec for spec in specs if spec.name in allowed_names]
    return tuple(selected)


def _warn_unknown_tool_allowlist_names(*, allowlist_names: list[str], known_names: list[str]) -> None:
    unknown = sorted(set(allowlist_names).difference(known_names))
    if not unknown:
        return
    logging.getLogger("arkloop.api").warning(
        "tool allowlist 包含未知工具，可能为拼写错误",
        extra={"unknown_tools": unknown, "known_tools": known_names},
    )


__all__ = [
    "InProcessStubRunExecutor",
    "QueuedRunExecutor",
    "RunExecutor",
    "configure_run_executor",
    "get_run_executor",
    "install_run_executor",
]
