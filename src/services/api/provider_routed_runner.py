from __future__ import annotations

import os
from typing import AsyncIterator, Protocol
import uuid

from packages.agent_core import AgentRunContext, AgentRunner, RunEvent, RunEventEmitter
from packages.agent_core.loop import AgentLoop
from packages.data import Database
from packages.data.threads import SqlAlchemyMessageRepository
from packages.llm_gateway import (
    ERROR_CLASS_INTERNAL_ERROR,
    LlmGatewayError,
    LlmGatewayRequest,
    LlmMessage,
    LlmTextPart,
    ToolSpec,
)
from packages.llm_gateway.anthropic import AnthropicGatewayConfig, AnthropicLlmGateway
from packages.llm_gateway.gateway import LlmGateway
from packages.llm_gateway.openai import OpenAiGatewayConfig, OpenAiLlmGateway
from packages.llm_routing import ProviderCredential, ProviderRouteDenied, ProviderRouter

_LLM_DEBUG_EVENTS_ENV = "ARKLOOP_LLM_DEBUG_EVENTS"
_TRUTHY = {"1", "true", "yes", "y", "on"}
_FALSY = {"0", "false", "no", "n", "off"}


def _parse_bool(value: str) -> bool:
    cleaned = value.strip().casefold()
    if cleaned in _TRUTHY:
        return True
    if cleaned in _FALSY:
        return False
    raise ValueError("必须为布尔值（0/1、true/false）")


def _llm_debug_events_enabled() -> bool:
    raw = os.getenv(_LLM_DEBUG_EVENTS_ENV)
    if not raw:
        return False
    return _parse_bool(raw)


class OrgByokPolicy(Protocol):
    async def is_byok_enabled(self, *, org_id: uuid.UUID) -> bool: ...


class AlwaysDisabledOrgByokPolicy:
    async def is_byok_enabled(self, *, org_id: uuid.UUID) -> bool:
        _ = org_id
        return False


class ProviderGatewayFactory(Protocol):
    def create(self, *, credential: ProviderCredential) -> LlmGateway: ...


class EnvProviderGatewayFactory:
    def __init__(self, *, stub_gateway: LlmGateway) -> None:
        self._stub_gateway = stub_gateway
        self._emit_llm_debug_events = _llm_debug_events_enabled()

    def create(self, *, credential: ProviderCredential) -> LlmGateway:
        if credential.provider_kind == "stub":
            return self._stub_gateway

        api_key_env = credential.api_key_env
        if not api_key_env:
            raise ValueError("缺少 api_key_env")
        api_key = (os.getenv(api_key_env) or "").strip()
        if not api_key:
            raise ValueError(f"缺少环境变量 {api_key_env}")

        if credential.provider_kind == "openai":
            base_url = credential.base_url or OpenAiGatewayConfig.base_url
            cfg = OpenAiGatewayConfig(
                api_key=api_key,
                base_url=base_url,
                api_mode=credential.openai_api_mode or "auto",
                emit_llm_debug_events=self._emit_llm_debug_events,
            )
            return OpenAiLlmGateway(config=cfg)

        if credential.provider_kind == "anthropic":
            base_url = credential.base_url or AnthropicGatewayConfig.base_url
            cfg = AnthropicGatewayConfig(
                api_key=api_key,
                base_url=base_url,
                advanced_json=dict(credential.advanced_json),
                emit_llm_debug_events=self._emit_llm_debug_events,
            )
            return AnthropicLlmGateway(config=cfg)

        raise ValueError(f"未知 provider_kind: {credential.provider_kind}")


class ProviderRoutedAgentRunner(AgentRunner):
    def __init__(
        self,
        *,
        database: Database,
        router: ProviderRouter,
        byok_policy: OrgByokPolicy,
        gateway_factory: ProviderGatewayFactory,
    ) -> None:
        self._database = database
        self._router = router
        self._byok_policy = byok_policy
        self._gateway_factory = gateway_factory

    async def run(self, *, context: AgentRunContext) -> AsyncIterator[RunEvent]:
        emitter = RunEventEmitter(run_id=context.run_id, trace_id=context.trace_id)

        try:
            org_id = _required_uuid(context.input_json.get("org_id"), label="org_id")
            thread_id = _required_uuid(context.input_json.get("thread_id"), label="thread_id")
        except Exception:
            error = LlmGatewayError(error_class=ERROR_CLASS_INTERNAL_ERROR, message="Run 输入缺失")
            yield emitter.emit(type="run.failed", error_class=error.error_class, data_json=error.to_json())
            return

        byok_enabled = await self._byok_policy.is_byok_enabled(org_id=org_id)
        decision = self._router.decide(input_json=context.input_json, byok_enabled=byok_enabled)

        if isinstance(decision, ProviderRouteDenied):
            yield emitter.emit(
                type="run.failed",
                error_class=decision.error_class,
                data_json=decision.to_run_failed_data_json(),
            )
            return

        yield emitter.emit(type="run.route.selected", data_json=decision.to_run_event_data_json())

        try:
            request = await self._build_request(
                org_id=org_id,
                thread_id=thread_id,
                model=decision.route.model,
                tool_specs=context.tool_specs,
            )
            gateway = self._gateway_factory.create(credential=decision.credential)
        except Exception:
            error = LlmGatewayError(error_class=ERROR_CLASS_INTERNAL_ERROR, message="路由初始化失败")
            yield emitter.emit(type="run.failed", error_class=error.error_class, data_json=error.to_json())
            return

        loop = AgentLoop(gateway=gateway)
        async for event in loop.run(
            context=context,
            emitter=emitter,
            request=request,
        ):
            yield event

    async def _build_request(
        self,
        *,
        org_id: uuid.UUID,
        thread_id: uuid.UUID,
        model: str,
        tool_specs: tuple[ToolSpec, ...],
    ) -> LlmGatewayRequest:
        async with self._database.sessionmaker() as session:
            repo = SqlAlchemyMessageRepository(session)
            messages = await repo.list_by_thread(org_id=org_id, thread_id=thread_id, limit=200)

        llm_messages = [
            LlmMessage(role=item.role, content=[LlmTextPart(text=item.content)]) for item in messages
        ]
        return LlmGatewayRequest(model=model, messages=llm_messages, tools=list(tool_specs))


def _required_uuid(value: object, *, label: str) -> uuid.UUID:
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{label} 缺失")
    try:
        return uuid.UUID(value.strip())
    except ValueError as exc:
        raise ValueError(f"{label} 非法") from exc


__all__ = [
    "AlwaysDisabledOrgByokPolicy",
    "EnvProviderGatewayFactory",
    "OrgByokPolicy",
    "ProviderGatewayFactory",
    "ProviderRoutedAgentRunner",
]
