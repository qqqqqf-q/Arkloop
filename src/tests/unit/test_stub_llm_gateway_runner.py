from __future__ import annotations

import uuid

import anyio

from packages.agent_core import AgentRunContext
from packages.llm_gateway.agent_runner import LlmGatewayAgentRunner
from packages.llm_gateway.stub import StubLlmGateway, StubLlmGatewayConfig


def test_stub_llm_gateway_runner_emits_deltas_and_completed() -> None:
    run_id = uuid.UUID(int=42)
    context = AgentRunContext(run_id=run_id, trace_id="a" * 32)

    gateway = StubLlmGateway(
        config=StubLlmGatewayConfig(enabled=True, delta_count=3, delta_interval_seconds=0.0),
    )
    runner = LlmGatewayAgentRunner(gateway=gateway)

    async def _collect():
        events = []
        async for event in runner.run(context=context):
            events.append(event)
        return events

    events = anyio.run(_collect)

    assert [event.type for event in events] == [
        "message.delta",
        "message.delta",
        "message.delta",
        "run.completed",
    ]
    assert [event.data_json.get("trace_id") for event in events] == ["a" * 32] * 4
    assert all(event.run_id == run_id for event in events)

    deltas = [event.data_json["content_delta"] for event in events if event.type == "message.delta"]
    assert deltas == ["stub delta 1", "stub delta 2", "stub delta 3"]
