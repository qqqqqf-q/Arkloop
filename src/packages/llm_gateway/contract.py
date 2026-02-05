from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, AsyncIterator, Mapping

from packages.agent_core import RunEvent, RunEventEmitter

JsonObject = dict[str, Any]

ERROR_CLASS_PROVIDER_RETRYABLE = "provider.retryable"
ERROR_CLASS_PROVIDER_NON_RETRYABLE = "provider.non_retryable"
ERROR_CLASS_BUDGET_EXCEEDED = "budget.exceeded"
ERROR_CLASS_POLICY_DENIED = "policy.denied"
ERROR_CLASS_INTERNAL_ERROR = "internal.error"
ERROR_CLASS_INTERNAL_STREAM_ENDED = "internal.stream_ended"


@dataclass(frozen=True, slots=True)
class LlmUsage:
    input_tokens: int | None = None
    output_tokens: int | None = None
    total_tokens: int | None = None

    def to_json(self) -> JsonObject:
        payload: JsonObject = {}
        if self.input_tokens is not None:
            payload["input_tokens"] = int(self.input_tokens)
        if self.output_tokens is not None:
            payload["output_tokens"] = int(self.output_tokens)
        if self.total_tokens is not None:
            payload["total_tokens"] = int(self.total_tokens)
        return payload


@dataclass(frozen=True, slots=True)
class LlmCost:
    currency: str
    amount_micros: int

    def to_json(self) -> JsonObject:
        return {"currency": self.currency, "amount_micros": int(self.amount_micros)}


@dataclass(frozen=True, slots=True)
class LlmGatewayError:
    error_class: str
    message: str
    details: Mapping[str, Any] = field(default_factory=dict)

    def to_json(self) -> JsonObject:
        payload: JsonObject = {"error_class": self.error_class, "message": self.message}
        if self.details:
            payload["details"] = dict(self.details)  # details 需要上游做脱敏
        return payload


@dataclass(frozen=True, slots=True)
class LlmTextPart:
    text: str

    def to_json(self) -> JsonObject:
        return {"type": "text", "text": self.text}


@dataclass(frozen=True, slots=True)
class LlmMessage:
    role: str
    content: list[LlmTextPart]

    def to_json(self) -> JsonObject:
        return {"role": self.role, "content": [part.to_json() for part in self.content]}


@dataclass(frozen=True, slots=True)
class ToolSpec:
    name: str
    description: str | None = None
    json_schema: Mapping[str, Any] = field(default_factory=dict)

    def to_json(self) -> JsonObject:
        payload: JsonObject = {"name": self.name, "schema": dict(self.json_schema)}
        if self.description is not None:
            payload["description"] = self.description
        return payload


@dataclass(frozen=True, slots=True)
class LlmGatewayRequest:
    model: str
    messages: list[LlmMessage]
    temperature: float | None = None
    max_output_tokens: int | None = None
    tools: list[ToolSpec] = field(default_factory=list)
    metadata: Mapping[str, Any] = field(default_factory=dict)

    def to_json(self) -> JsonObject:
        payload: JsonObject = {
            "model": self.model,
            "messages": [message.to_json() for message in self.messages],
        }
        if self.temperature is not None:
            payload["temperature"] = float(self.temperature)
        if self.max_output_tokens is not None:
            payload["max_output_tokens"] = int(self.max_output_tokens)
        if self.tools:
            payload["tools"] = [tool.to_json() for tool in self.tools]
        if self.metadata:
            payload["metadata"] = dict(self.metadata)
        return payload


@dataclass(frozen=True, slots=True)
class LlmStreamMessageDelta:
    content_delta: str
    role: str = "assistant"
    channel: str | None = None

    def to_data_json(self) -> JsonObject:
        payload: JsonObject = {"content_delta": self.content_delta, "role": self.role}
        if self.channel is not None:
            payload["channel"] = self.channel
        return payload


@dataclass(frozen=True, slots=True)
class LlmStreamLlmRequest:
    llm_call_id: str
    provider_kind: str
    api_mode: str
    base_url: str | None = None
    path: str | None = None
    payload_json: Mapping[str, Any] = field(default_factory=dict)

    def to_data_json(self) -> JsonObject:
        payload: JsonObject = {
            "llm_call_id": self.llm_call_id,
            "provider_kind": self.provider_kind,
            "api_mode": self.api_mode,
            "payload": dict(self.payload_json),
        }
        if self.base_url is not None:
            payload["base_url"] = self.base_url
        if self.path is not None:
            payload["path"] = self.path
        return payload


@dataclass(frozen=True, slots=True)
class LlmStreamLlmResponseChunk:
    llm_call_id: str
    provider_kind: str
    api_mode: str
    raw: str
    chunk_json: Any | None = None
    status_code: int | None = None
    truncated: bool = False

    def to_data_json(self) -> JsonObject:
        payload: JsonObject = {
            "llm_call_id": self.llm_call_id,
            "provider_kind": self.provider_kind,
            "api_mode": self.api_mode,
            "raw": self.raw,
            "truncated": bool(self.truncated),
        }
        if self.chunk_json is not None:
            payload["json"] = self.chunk_json
        if self.status_code is not None:
            payload["status_code"] = int(self.status_code)
        return payload


@dataclass(frozen=True, slots=True)
class LlmStreamToolCall:
    tool_call_id: str
    tool_name: str
    arguments_json: Mapping[str, Any]

    def to_data_json(self) -> JsonObject:
        return {
            "tool_call_id": self.tool_call_id,
            "tool_name": self.tool_name,
            "arguments": dict(self.arguments_json),
        }


@dataclass(frozen=True, slots=True)
class LlmStreamToolResult:
    tool_call_id: str
    tool_name: str
    result_json: Mapping[str, Any] | None = None
    error: LlmGatewayError | None = None
    usage: LlmUsage | None = None
    cost: LlmCost | None = None

    def to_data_json(self) -> JsonObject:
        payload: JsonObject = {"tool_call_id": self.tool_call_id, "tool_name": self.tool_name}
        if self.result_json is not None:
            payload["result"] = dict(self.result_json)
        if self.error is not None:
            payload["error"] = self.error.to_json()
        if self.usage is not None:
            payload["usage"] = self.usage.to_json()
        if self.cost is not None:
            payload["cost"] = self.cost.to_json()
        return payload


@dataclass(frozen=True, slots=True)
class LlmStreamProviderFallback:
    provider_kind: str
    from_api_mode: str
    to_api_mode: str
    reason: str
    status_code: int | None = None

    def to_data_json(self) -> JsonObject:
        payload: JsonObject = {
            "provider_kind": self.provider_kind,
            "from_api_mode": self.from_api_mode,
            "to_api_mode": self.to_api_mode,
            "reason": self.reason,
        }
        if self.status_code is not None:
            payload["status_code"] = int(self.status_code)
        return payload


@dataclass(frozen=True, slots=True)
class LlmStreamRunCompleted:
    usage: LlmUsage | None = None
    cost: LlmCost | None = None

    def to_data_json(self) -> JsonObject:
        payload: JsonObject = {}
        if self.usage is not None:
            payload["usage"] = self.usage.to_json()
        if self.cost is not None:
            payload["cost"] = self.cost.to_json()
        return payload


@dataclass(frozen=True, slots=True)
class LlmStreamRunFailed:
    error: LlmGatewayError
    usage: LlmUsage | None = None
    cost: LlmCost | None = None

    def to_data_json(self) -> JsonObject:
        payload: JsonObject = self.error.to_json()
        if self.usage is not None:
            payload["usage"] = self.usage.to_json()
        if self.cost is not None:
            payload["cost"] = self.cost.to_json()
        return payload


LlmGatewayStreamEvent = (
    LlmStreamMessageDelta
    | LlmStreamLlmRequest
    | LlmStreamLlmResponseChunk
    | LlmStreamToolCall
    | LlmStreamToolResult
    | LlmStreamProviderFallback
    | LlmStreamRunCompleted
    | LlmStreamRunFailed
)


def _internal_stream_ended_error() -> LlmGatewayError:
    return LlmGatewayError(
        error_class=ERROR_CLASS_INTERNAL_STREAM_ENDED,
        message="上游流在未结束状态时提前结束",
    )


async def run_events_from_llm_stream(
    *,
    emitter: RunEventEmitter,
    stream: AsyncIterator[LlmGatewayStreamEvent],
) -> AsyncIterator[RunEvent]:
    close = getattr(stream, "aclose", None)
    try:
        async for item in stream:
            if isinstance(item, LlmStreamMessageDelta):
                if not item.content_delta:
                    continue
                yield emitter.emit(type="message.delta", data_json=item.to_data_json())
                continue

            if isinstance(item, LlmStreamLlmRequest):
                yield emitter.emit(type="llm.request", data_json=item.to_data_json())
                continue

            if isinstance(item, LlmStreamLlmResponseChunk):
                yield emitter.emit(type="llm.response.chunk", data_json=item.to_data_json())
                continue

            if isinstance(item, LlmStreamToolCall):
                yield emitter.emit(
                    type="tool.call",
                    data_json=item.to_data_json(),
                    tool_name=item.tool_name,
                )
                continue

            if isinstance(item, LlmStreamToolResult):
                yield emitter.emit(
                    type="tool.result",
                    data_json=item.to_data_json(),
                    tool_name=item.tool_name,
                    error_class=item.error.error_class if item.error is not None else None,
                )
                continue

            if isinstance(item, LlmStreamProviderFallback):
                yield emitter.emit(type="run.provider_fallback", data_json=item.to_data_json())
                continue

            if isinstance(item, LlmStreamRunCompleted):
                yield emitter.emit(type="run.completed", data_json=item.to_data_json())
                return

            if isinstance(item, LlmStreamRunFailed):
                yield emitter.emit(
                    type="run.failed",
                    data_json=item.to_data_json(),
                    error_class=item.error.error_class,
                )
                return

            raise TypeError(f"未知的 LLM Gateway 事件类型: {type(item)!r}")
    finally:
        if close is not None:
            try:
                await close()
            except Exception:
                pass

    error = _internal_stream_ended_error()
    yield emitter.emit(
        type="run.failed",
        data_json=error.to_json(),
        error_class=error.error_class,
    )


__all__ = [
    "ERROR_CLASS_BUDGET_EXCEEDED",
    "ERROR_CLASS_INTERNAL_ERROR",
    "ERROR_CLASS_INTERNAL_STREAM_ENDED",
    "ERROR_CLASS_POLICY_DENIED",
    "ERROR_CLASS_PROVIDER_NON_RETRYABLE",
    "ERROR_CLASS_PROVIDER_RETRYABLE",
    "JsonObject",
    "LlmCost",
    "LlmGatewayError",
    "LlmGatewayRequest",
    "LlmGatewayStreamEvent",
    "LlmMessage",
    "LlmStreamLlmRequest",
    "LlmStreamLlmResponseChunk",
    "LlmStreamMessageDelta",
    "LlmStreamRunCompleted",
    "LlmStreamRunFailed",
    "LlmStreamProviderFallback",
    "LlmStreamToolCall",
    "LlmStreamToolResult",
    "LlmTextPart",
    "LlmUsage",
    "ToolSpec",
    "run_events_from_llm_stream",
]
