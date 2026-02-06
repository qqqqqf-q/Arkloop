from __future__ import annotations

from dataclasses import dataclass
import json
from typing import Any, AsyncIterator, Callable, Mapping, Protocol

from packages.llm_gateway import (
    ERROR_CLASS_INTERNAL_STREAM_ENDED,
    LlmGatewayError,
    LlmGatewayRequest,
    LlmMessage,
    LlmStreamLlmRequest,
    LlmStreamLlmResponseChunk,
    LlmStreamMessageDelta,
    LlmStreamProviderFallback,
    LlmStreamRunCompleted,
    LlmStreamRunFailed,
    LlmStreamToolCall,
    LlmStreamToolResult,
    LlmTextPart,
)
from packages.llm_gateway.gateway import LlmGateway

from .events import RunEvent, RunEventEmitter
from .runner import AgentRunContext

ERROR_CLASS_AGENT_MAX_ITERATIONS_EXCEEDED = "agent.max_iterations_exceeded"


class ToolCallExecutor(Protocol):
    async def execute(
        self,
        *,
        tool_call: LlmStreamToolCall,
        context: AgentRunContext,
    ) -> LlmStreamToolResult: ...


@dataclass(slots=True)
class StubToolExecutor:
    result_factory: Callable[[LlmStreamToolCall], Mapping[str, Any]] | None = None

    async def execute(
        self,
        *,
        tool_call: LlmStreamToolCall,
        context: AgentRunContext,
    ) -> LlmStreamToolResult:
        _ = context
        payload_factory = self.result_factory or _default_stub_tool_result
        payload = dict(payload_factory(tool_call))
        return LlmStreamToolResult(
            tool_call_id=tool_call.tool_call_id,
            tool_name=tool_call.tool_name,
            result_json=payload,
        )


def _default_stub_tool_result(tool_call: LlmStreamToolCall) -> Mapping[str, Any]:
    return {
        "stub": True,
        "tool_name": tool_call.tool_name,
        "tool_call_id": tool_call.tool_call_id,
        "echo_arguments": dict(tool_call.arguments_json),
    }


class AgentLoop:
    def __init__(
        self,
        *,
        gateway: LlmGateway,
        tool_executor: ToolCallExecutor | None = None,
    ) -> None:
        self._gateway = gateway
        self._tool_executor = tool_executor or StubToolExecutor()

    async def run(
        self,
        *,
        context: AgentRunContext,
        emitter: RunEventEmitter,
        request: LlmGatewayRequest,
    ) -> AsyncIterator[RunEvent]:
        if context.max_iterations <= 0:
            yield _emit_max_iterations_failed(emitter=emitter, max_iterations=context.max_iterations)
            return

        messages = list(request.messages)
        for _ in range(1, context.max_iterations + 1):
            if _cancelled(context):
                yield emitter.emit(type="run.cancelled", data_json={"reason": "cancel_signal"})
                return

            turn_request = _copy_request(request=request, messages=messages)
            turn = await self._run_single_turn(
                context=context,
                emitter=emitter,
                request=turn_request,
            )
            for event in turn.events:
                yield event

            if turn.terminal:
                return
            if turn.cancelled:
                yield emitter.emit(type="run.cancelled", data_json={"reason": "cancel_signal"})
                return

            if turn.assistant_text:
                messages.append(_assistant_message(turn.assistant_text))

            if turn.tool_results:
                for tool_result in turn.tool_results:
                    messages.append(_tool_result_message(tool_result))

            completed_tool_result_ids = {item.tool_call_id for item in turn.tool_results}

            if turn.completed_data_json is None:
                error = _internal_stream_ended_error()
                yield emitter.emit(
                    type="run.failed",
                    data_json=error.to_json(),
                    error_class=error.error_class,
                )
                return

            if not turn.tool_calls:
                yield emitter.emit(type="run.completed", data_json=turn.completed_data_json)
                return

            for tool_call in turn.tool_calls:
                if tool_call.tool_call_id in completed_tool_result_ids:
                    continue
                if _cancelled(context):
                    yield emitter.emit(type="run.cancelled", data_json={"reason": "cancel_signal"})
                    return
                tool_result = await self._tool_executor.execute(tool_call=tool_call, context=context)
                messages.append(_tool_result_message(tool_result))
                yield emitter.emit(
                    type="tool.result",
                    data_json=tool_result.to_data_json(),
                    tool_name=tool_result.tool_name,
                    error_class=tool_result.error.error_class if tool_result.error is not None else None,
                )

            # 该轮工具结果已由 gateway 补齐，AgentLoop 不再发起新一轮请求。
            has_manual_execution = any(
                tool_call.tool_call_id not in completed_tool_result_ids for tool_call in turn.tool_calls
            )
            if not has_manual_execution:
                yield emitter.emit(type="run.completed", data_json=turn.completed_data_json)
                return

        yield _emit_max_iterations_failed(emitter=emitter, max_iterations=context.max_iterations)

    async def _run_single_turn(
        self,
        *,
        context: AgentRunContext,
        emitter: RunEventEmitter,
        request: LlmGatewayRequest,
    ) -> "_TurnResult":
        stream = self._gateway.stream(request=request)
        close = getattr(stream, "aclose", None)

        events: list[RunEvent] = []
        tool_calls: list[LlmStreamToolCall] = []
        tool_results: list[LlmStreamToolResult] = []
        assistant_chunks: list[str] = []
        completed: LlmStreamRunCompleted | None = None

        try:
            async for item in stream:
                if _cancelled(context):
                    return _TurnResult(events=tuple(events), terminal=False, cancelled=True)

                if isinstance(item, LlmStreamMessageDelta):
                    if item.content_delta:
                        assistant_chunks.append(item.content_delta)
                        events.append(emitter.emit(type="message.delta", data_json=item.to_data_json()))
                    continue

                if isinstance(item, LlmStreamLlmRequest):
                    events.append(emitter.emit(type="llm.request", data_json=item.to_data_json()))
                    continue

                if isinstance(item, LlmStreamLlmResponseChunk):
                    events.append(emitter.emit(type="llm.response.chunk", data_json=item.to_data_json()))
                    continue

                if isinstance(item, LlmStreamProviderFallback):
                    events.append(emitter.emit(type="run.provider_fallback", data_json=item.to_data_json()))
                    continue

                if isinstance(item, LlmStreamToolCall):
                    tool_calls.append(item)
                    events.append(
                        emitter.emit(type="tool.call", data_json=item.to_data_json(), tool_name=item.tool_name)
                    )
                    continue

                if isinstance(item, LlmStreamToolResult):
                    tool_results.append(item)
                    events.append(
                        emitter.emit(
                            type="tool.result",
                            data_json=item.to_data_json(),
                            tool_name=item.tool_name,
                            error_class=item.error.error_class if item.error is not None else None,
                        )
                    )
                    continue

                if isinstance(item, LlmStreamRunFailed):
                    events.append(
                        emitter.emit(
                            type="run.failed",
                            data_json=item.to_data_json(),
                            error_class=item.error.error_class,
                        )
                    )
                    return _TurnResult(events=tuple(events), terminal=True)

                if isinstance(item, LlmStreamRunCompleted):
                    completed = item
                    break

                raise TypeError(f"未知的 LLM Gateway 事件类型: {type(item)!r}")
        finally:
            if close is not None:
                try:
                    await close()
                except Exception:
                    pass

        completed_payload = completed.to_data_json() if completed is not None else None
        return _TurnResult(
            events=tuple(events),
            terminal=False,
            tool_calls=tuple(tool_calls),
            tool_results=tuple(tool_results),
            assistant_text="".join(assistant_chunks),
            completed_data_json=completed_payload,
        )


@dataclass(frozen=True, slots=True)
class _TurnResult:
    events: tuple[RunEvent, ...]
    terminal: bool
    cancelled: bool = False
    tool_calls: tuple[LlmStreamToolCall, ...] = ()
    tool_results: tuple[LlmStreamToolResult, ...] = ()
    assistant_text: str = ""
    completed_data_json: Mapping[str, Any] | None = None


def _copy_request(*, request: LlmGatewayRequest, messages: list[LlmMessage]) -> LlmGatewayRequest:
    return LlmGatewayRequest(
        model=request.model,
        messages=list(messages),
        temperature=request.temperature,
        max_output_tokens=request.max_output_tokens,
        tools=list(request.tools),
        metadata=dict(request.metadata),
    )


def _assistant_message(content: str) -> LlmMessage:
    return LlmMessage(role="assistant", content=[LlmTextPart(text=content)])


def _tool_result_message(tool_result: LlmStreamToolResult) -> LlmMessage:
    payload: dict[str, Any] = {
        "tool_call_id": tool_result.tool_call_id,
        "tool_name": tool_result.tool_name,
    }
    if tool_result.result_json is not None:
        payload["result"] = dict(tool_result.result_json)
    if tool_result.error is not None:
        payload["error"] = tool_result.error.to_json()
    content = json.dumps(payload, ensure_ascii=False, separators=(",", ":"), sort_keys=True)
    return LlmMessage(role="tool", content=[LlmTextPart(text=content)])


def _cancelled(context: AgentRunContext) -> bool:
    if context.cancel_signal is None:
        return False
    return bool(context.cancel_signal())


def _internal_stream_ended_error() -> LlmGatewayError:
    return LlmGatewayError(
        error_class=ERROR_CLASS_INTERNAL_STREAM_ENDED,
        message="上游流在未结束状态时提前结束",
    )


def _emit_max_iterations_failed(*, emitter: RunEventEmitter, max_iterations: int) -> RunEvent:
    error = LlmGatewayError(
        error_class=ERROR_CLASS_AGENT_MAX_ITERATIONS_EXCEEDED,
        message="Agent 循环达到最大轮次",
        details={"max_iterations": max_iterations},
    )
    return emitter.emit(type="run.failed", data_json=error.to_json(), error_class=error.error_class)


__all__ = [
    "AgentLoop",
    "ERROR_CLASS_AGENT_MAX_ITERATIONS_EXCEEDED",
    "StubToolExecutor",
    "ToolCallExecutor",
]
