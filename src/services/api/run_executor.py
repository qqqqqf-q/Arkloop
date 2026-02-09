from __future__ import annotations

import asyncio
import logging
import os
from typing import Protocol
import uuid

from fastapi import FastAPI, Request

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
from packages.data import Database
from packages.data.runs import RunNotFoundError
from packages.llm_gateway import ToolSpec as LlmToolSpec
from packages.llm_gateway.stub import StubLlmGateway, StubLlmGatewayConfig
from packages.llm_routing import ProviderRouter, ProviderRoutingConfig
from packages.observability.context import new_trace_id, trace_id_context

from .error_envelope import ApiError
from .provider_routed_runner import (
    AlwaysDisabledOrgByokPolicy,
    EnvProviderGatewayFactory,
    ProviderRoutedAgentRunner,
)
from .run_engine import RunEngine

_TOOL_ALLOWLIST_ENV = "ARKLOOP_TOOL_ALLOWLIST"


class RunExecutor(Protocol):
    def enqueue(self, *, run_id: uuid.UUID) -> None: ...

    async def start(self) -> None: ...

    async def stop(self) -> None: ...


class InProcessStubRunExecutor(RunExecutor):
    def __init__(self, *, engine: RunEngine, config: StubLlmGatewayConfig) -> None:
        self._engine = engine
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
    stub_config = StubLlmGatewayConfig.from_env()
    stub_gateway = StubLlmGateway(config=stub_config)
    routing_config = ProviderRoutingConfig.from_env()
    router = ProviderRouter(config=routing_config)
    tool_registry = ToolRegistry(specs=builtin_agent_tool_specs())
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
    tool_executor = DispatchingToolExecutor(
        registry=tool_registry,
        policy_enforcer=tool_policy_enforcer,
        executors=create_builtin_tool_executors(),
    )
    allowed_llm_tool_specs = _select_llm_tool_specs(
        allowed_names=set(tool_allowlist_names),
        specs=builtin_llm_tool_specs(),
    )
    runner = ProviderRoutedAgentRunner(
        database=database,
        router=router,
        byok_policy=AlwaysDisabledOrgByokPolicy(),
        gateway_factory=EnvProviderGatewayFactory(stub_gateway=stub_gateway),
        tool_executor=tool_executor,
        tool_specs=allowed_llm_tool_specs,
    )
    engine = RunEngine(database=database, runner=runner)
    install_run_executor(
        app,
        InProcessStubRunExecutor(engine=engine, config=stub_config),
    )


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
    "RunExecutor",
    "StubLlmGatewayConfig",
    "configure_run_executor",
    "get_run_executor",
    "install_run_executor",
]
