from __future__ import annotations

import logging
import os

from sqlalchemy.ext.asyncio import AsyncSession

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
from packages.data import Database, DatabaseConfig
from packages.job_queue import JobQueue, SqlAlchemyPgJobQueue
from packages.llm_gateway import ToolSpec as LlmToolSpec
from packages.llm_gateway.stub import StubLlmGateway, StubLlmGatewayConfig
from packages.llm_routing import ProviderRouter, ProviderRoutingConfig
from packages.mcp import load_mcp_tool_registration_from_env_async
from services.api.provider_routed_runner import (
    AlwaysDisabledOrgByokPolicy,
    EnvProviderGatewayFactory,
    ProviderRoutedAgentRunner,
)
from services.api.run_engine import RunEngine

from .consumer_loop import JobQueueFactory, WorkerConsumerLoop, WorkerLoopConfig
from .worker import Worker

_TOOL_ALLOWLIST_ENV = "ARKLOOP_TOOL_ALLOWLIST"


def create_database(*, config: DatabaseConfig | None = None) -> Database:
    config = config or DatabaseConfig.from_env(required=True, allow_fallback=False)
    if config is None:
        raise ValueError("缺少数据库配置")
    return Database.from_config(config)


def create_job_queue_factory() -> JobQueueFactory:
    def _factory(session: AsyncSession) -> JobQueue:
        return SqlAlchemyPgJobQueue(session)

    return _factory


async def create_worker(*, database: Database) -> Worker:
    stub_config = StubLlmGatewayConfig.from_env()
    stub_gateway = StubLlmGateway(config=stub_config)

    routing_config = ProviderRoutingConfig.from_env()
    router = ProviderRouter(config=routing_config)

    tool_registry = ToolRegistry(specs=builtin_agent_tool_specs())
    mcp_registration = await load_mcp_tool_registration_from_env_async()
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
    engine = RunEngine(database=database, runner=runner)
    return Worker(database=database, engine=engine)


def create_loop(
    *,
    database: Database,
    job_queue_factory: JobQueueFactory,
    worker: Worker,
    loop_config: WorkerLoopConfig | None = None,
) -> WorkerConsumerLoop:
    config = loop_config or WorkerLoopConfig.from_env()
    return WorkerConsumerLoop(
        database=database,
        job_queue_factory=job_queue_factory,
        worker=worker,
        config=config,
    )


async def create_container(
    *,
    database_config: DatabaseConfig | None = None,
    loop_config: WorkerLoopConfig | None = None,
) -> tuple[Database, WorkerConsumerLoop]:
    database = create_database(config=database_config)
    job_queue_factory = create_job_queue_factory()
    worker = await create_worker(database=database)
    loop = create_loop(
        database=database,
        job_queue_factory=job_queue_factory,
        worker=worker,
        loop_config=loop_config,
    )
    return database, loop


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
    logging.getLogger("arkloop.worker").warning(
        "tool allowlist 包含未知工具，可能为拼写错误",
        extra={"unknown_tools": unknown, "known_tools": known_names},
    )


__all__ = ["create_container"]
