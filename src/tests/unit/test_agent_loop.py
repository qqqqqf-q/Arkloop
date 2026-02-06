from __future__ import annotations

import json
import uuid

import anyio

from packages.agent_core import AgentRunContext, RunEventEmitter
from packages.agent_core.loop import (
    ERROR_CLASS_AGENT_MAX_ITERATIONS_EXCEEDED,
    AgentLoop,
    StubToolExecutor,
)
from packages.llm_gateway import (
    LlmGatewayRequest,
    LlmMessage,
    LlmStreamMessageDelta,
    LlmStreamRunCompleted,
    LlmStreamToolCall,
    LlmStreamToolResult,
    LlmTextPart,
    ToolSpec,
)


class _ScriptedGateway:
    def __init__(self, *, turns: list[list[object]]) -> None:
        self._turns = turns
        self.requests: list[LlmGatewayRequest] = []

    async def stream(self, *, request: LlmGatewayRequest):
        self.requests.append(request)
        turn_index = len(self.requests) - 1
        items = self._turns[turn_index] if turn_index < len(self._turns) else []
        for item in items:
            yield item


class _CancelOnFirstToolExecutor(StubToolExecutor):
    def __init__(self, *, cancel_state: dict[str, bool]) -> None:
        super().__init__()
        self._cancel_state = cancel_state

    async def execute(
        self,
        *,
        tool_call: LlmStreamToolCall,
        context: AgentRunContext,
    ) -> LlmStreamToolResult:
        self._cancel_state["cancelled"] = True
        return await super().execute(tool_call=tool_call, context=context)


def _base_request() -> LlmGatewayRequest:
    return LlmGatewayRequest(
        model="stub-model",
        messages=[LlmMessage(role="user", content=[LlmTextPart(text="hi")])],
        tools=[ToolSpec(name="echo", description="echo tool", json_schema={"type": "object"})],
    )


def test_agent_loop_supports_multi_turn_with_stub_tool_executor() -> None:
    gateway = _ScriptedGateway(
        turns=[
            [
                LlmStreamToolCall(
                    tool_call_id="tool_1",
                    tool_name="echo",
                    arguments_json={"query": "ping"},
                ),
                LlmStreamRunCompleted(),
            ],
            [
                LlmStreamMessageDelta(content_delta="done", role="assistant"),
                LlmStreamRunCompleted(),
            ],
        ]
    )
    loop = AgentLoop(gateway=gateway)
    context = AgentRunContext(run_id=uuid.uuid4(), trace_id="a" * 32)
    emitter = RunEventEmitter(run_id=context.run_id, trace_id=context.trace_id)

    async def _collect():
        events = []
        async for event in loop.run(context=context, emitter=emitter, request=_base_request()):
            events.append(event)
        return events

    events = anyio.run(_collect)

    assert [event.type for event in events] == ["tool.call", "tool.result", "message.delta", "run.completed"]
    assert events[1].data_json["result"]["stub"] is True
    assert events[2].data_json["content_delta"] == "done"
    assert events[3].data_json["trace_id"] == "a" * 32

    assert len(gateway.requests) == 2
    assert gateway.requests[0].tools[0].name == "echo"
    tool_message = gateway.requests[1].messages[-1]
    assert tool_message.role == "tool"
    payload = json.loads(tool_message.content[0].text)
    assert payload["tool_call_id"] == "tool_1"


def test_agent_loop_emits_failed_when_max_iterations_exceeded() -> None:
    gateway = _ScriptedGateway(
        turns=[
            [
                LlmStreamToolCall(
                    tool_call_id="tool_1",
                    tool_name="echo",
                    arguments_json={"step": 1},
                ),
                LlmStreamRunCompleted(),
            ],
            [
                LlmStreamToolCall(
                    tool_call_id="tool_2",
                    tool_name="echo",
                    arguments_json={"step": 2},
                ),
                LlmStreamRunCompleted(),
            ],
        ]
    )
    loop = AgentLoop(gateway=gateway)
    context = AgentRunContext(run_id=uuid.uuid4(), trace_id="b" * 32, max_iterations=2)
    emitter = RunEventEmitter(run_id=context.run_id, trace_id=context.trace_id)

    async def _collect():
        events = []
        async for event in loop.run(context=context, emitter=emitter, request=_base_request()):
            events.append(event)
        return events

    events = anyio.run(_collect)

    assert [event.type for event in events] == [
        "tool.call",
        "tool.result",
        "tool.call",
        "tool.result",
        "run.failed",
    ]
    assert events[-1].error_class == ERROR_CLASS_AGENT_MAX_ITERATIONS_EXCEEDED
    assert events[-1].data_json["details"]["max_iterations"] == 2


def test_agent_loop_stops_when_cancel_signal_triggered() -> None:
    cancel_state = {"cancelled": False}
    gateway = _ScriptedGateway(
        turns=[
            [
                LlmStreamToolCall(
                    tool_call_id="tool_1",
                    tool_name="echo",
                    arguments_json={"query": "stop"},
                ),
                LlmStreamRunCompleted(),
            ],
            [
                LlmStreamMessageDelta(content_delta="should-not-run", role="assistant"),
                LlmStreamRunCompleted(),
            ],
        ]
    )
    loop = AgentLoop(gateway=gateway, tool_executor=_CancelOnFirstToolExecutor(cancel_state=cancel_state))
    context = AgentRunContext(
        run_id=uuid.uuid4(),
        trace_id="c" * 32,
        cancel_signal=lambda: cancel_state["cancelled"],
    )
    emitter = RunEventEmitter(run_id=context.run_id, trace_id=context.trace_id)

    async def _collect():
        events = []
        async for event in loop.run(context=context, emitter=emitter, request=_base_request()):
            events.append(event)
        return events

    events = anyio.run(_collect)

    assert [event.type for event in events] == ["tool.call", "tool.result", "run.cancelled"]
    assert events[-1].data_json["reason"] == "cancel_signal"
    assert len(gateway.requests) == 1


def test_agent_loop_completes_when_gateway_already_completed_tool_calls() -> None:
    gateway = _ScriptedGateway(
        turns=[
            [
                LlmStreamToolCall(
                    tool_call_id="tool_1",
                    tool_name="echo",
                    arguments_json={"query": "from-gateway"},
                ),
                LlmStreamToolResult(
                    tool_call_id="tool_1",
                    tool_name="echo",
                    result_json={"from_gateway": True},
                ),
                LlmStreamRunCompleted(),
            ],
        ]
    )
    loop = AgentLoop(gateway=gateway)
    context = AgentRunContext(run_id=uuid.uuid4(), trace_id="d" * 32, max_iterations=3)
    emitter = RunEventEmitter(run_id=context.run_id, trace_id=context.trace_id)

    async def _collect():
        events = []
        async for event in loop.run(context=context, emitter=emitter, request=_base_request()):
            events.append(event)
        return events

    events = anyio.run(_collect)

    assert [event.type for event in events] == [
        "tool.call",
        "tool.result",
        "run.completed",
    ]
    assert events[1].data_json["result"] == {"from_gateway": True}
    assert len(gateway.requests) == 1
