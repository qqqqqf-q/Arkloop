from __future__ import annotations

from typing import AsyncIterator, Callable

from packages.agent_core import AgentRunContext, AgentRunner, RunEvent, RunEventEmitter

from .contract import LlmGatewayRequest, run_events_from_llm_stream
from .gateway import LlmGateway

LlmGatewayRequestBuilder = Callable[[AgentRunContext], LlmGatewayRequest]


def _default_request_builder(_context: AgentRunContext) -> LlmGatewayRequest:
    return LlmGatewayRequest(model="stub", messages=[])


class LlmGatewayAgentRunner(AgentRunner):
    def __init__(
        self,
        *,
        gateway: LlmGateway,
        request_builder: LlmGatewayRequestBuilder = _default_request_builder,
    ) -> None:
        self._gateway = gateway
        self._request_builder = request_builder

    async def run(self, *, context: AgentRunContext) -> AsyncIterator[RunEvent]:
        request = self._request_builder(context)
        emitter = RunEventEmitter(run_id=context.run_id, trace_id=context.trace_id)
        async for event in run_events_from_llm_stream(
            emitter=emitter,
            stream=self._gateway.stream(request=request),
        ):
            yield event


__all__ = ["LlmGatewayAgentRunner", "LlmGatewayRequestBuilder"]
