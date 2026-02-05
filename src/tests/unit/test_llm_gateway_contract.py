from __future__ import annotations

from datetime import datetime, timezone
import uuid

import anyio

from packages.agent_core import RunEventEmitter
from packages.llm_gateway import (
    ERROR_CLASS_INTERNAL_STREAM_ENDED,
    ERROR_CLASS_PROVIDER_RETRYABLE,
    LlmCost,
    LlmGatewayError,
    LlmStreamLlmRequest,
    LlmStreamLlmResponseChunk,
    LlmStreamMessageDelta,
    LlmStreamProviderFallback,
    LlmStreamRunCompleted,
    LlmStreamRunFailed,
    LlmUsage,
    run_events_from_llm_stream,
)


class _FakeEventIdFactory:
    def __init__(self) -> None:
        self._next_int = 1

    def __call__(self) -> uuid.UUID:
        value = uuid.UUID(int=self._next_int)
        self._next_int += 1
        return value


def _fixed_clock() -> datetime:
    return datetime(2025, 1, 1, 0, 0, 0, tzinfo=timezone.utc)


def test_llm_gateway_stream_emits_message_delta_and_completed() -> None:
    run_id = uuid.UUID(int=42)
    emitter = RunEventEmitter(
        run_id=run_id,
        trace_id="a" * 32,
        event_id_factory=_FakeEventIdFactory(),
        clock=_fixed_clock,
    )

    async def _stub_stream():
        yield LlmStreamMessageDelta(content_delta="hello", role="assistant")
        yield LlmStreamMessageDelta(content_delta=" world", role="assistant")
        yield LlmStreamRunCompleted(
            usage=LlmUsage(input_tokens=1, output_tokens=2, total_tokens=3),
            cost=LlmCost(currency="USD", amount_micros=123),
        )

    async def _collect():
        events = []
        async for event in run_events_from_llm_stream(emitter=emitter, stream=_stub_stream()):
            events.append(event)
        return events

    events = anyio.run(_collect)

    assert [event.seq for event in events] == [1, 2, 3]
    assert [event.type for event in events] == ["message.delta", "message.delta", "run.completed"]
    assert all(event.run_id == run_id for event in events)
    assert all(event.ts == _fixed_clock() for event in events)
    assert all(event.data_json.get("trace_id") == "a" * 32 for event in events)

    assert events[0].data_json["content_delta"] == "hello"
    assert events[1].data_json["content_delta"] == " world"
    assert events[2].data_json["usage"]["total_tokens"] == 3
    assert events[2].data_json["cost"]["amount_micros"] == 123


def test_llm_gateway_stream_emits_run_failed_with_stable_error_class() -> None:
    run_id = uuid.UUID(int=1)
    emitter = RunEventEmitter(
        run_id=run_id,
        trace_id="b" * 32,
        event_id_factory=_FakeEventIdFactory(),
        clock=_fixed_clock,
    )

    async def _stub_stream():
        yield LlmStreamMessageDelta(content_delta="x", role="assistant")
        yield LlmStreamRunFailed(
            error=LlmGatewayError(
                error_class=ERROR_CLASS_PROVIDER_RETRYABLE,
                message="rate limited",
            )
        )

    async def _collect():
        events = []
        async for event in run_events_from_llm_stream(emitter=emitter, stream=_stub_stream()):
            events.append(event)
        return events

    events = anyio.run(_collect)

    assert [event.type for event in events] == ["message.delta", "run.failed"]
    assert events[1].error_class == ERROR_CLASS_PROVIDER_RETRYABLE
    assert events[1].data_json["error_class"] == ERROR_CLASS_PROVIDER_RETRYABLE
    assert events[1].data_json["message"] == "rate limited"
    assert events[1].data_json["trace_id"] == "b" * 32


def test_llm_gateway_stream_ends_without_final_emits_internal_failed() -> None:
    emitter = RunEventEmitter(
        run_id=uuid.UUID(int=9),
        trace_id="c" * 32,
        event_id_factory=_FakeEventIdFactory(),
        clock=_fixed_clock,
    )

    async def _stub_stream():
        yield LlmStreamMessageDelta(content_delta="partial", role="assistant")

    async def _collect():
        events = []
        async for event in run_events_from_llm_stream(emitter=emitter, stream=_stub_stream()):
            events.append(event)
        return events

    events = anyio.run(_collect)

    assert [event.type for event in events] == ["message.delta", "run.failed"]
    assert events[1].error_class == ERROR_CLASS_INTERNAL_STREAM_ENDED


def test_llm_gateway_stream_emits_provider_fallback_as_run_event() -> None:
    emitter = RunEventEmitter(
        run_id=uuid.UUID(int=9),
        trace_id="d" * 32,
        event_id_factory=_FakeEventIdFactory(),
        clock=_fixed_clock,
    )

    async def _stub_stream():
        yield LlmStreamProviderFallback(
            provider_kind="openai",
            from_api_mode="responses",
            to_api_mode="chat_completions",
            reason="responses_endpoint_not_supported",
            status_code=404,
        )
        yield LlmStreamRunCompleted()

    async def _collect():
        events = []
        async for event in run_events_from_llm_stream(emitter=emitter, stream=_stub_stream()):
            events.append(event)
        return events

    events = anyio.run(_collect)

    assert [event.seq for event in events] == [1, 2]
    assert [event.type for event in events] == ["run.provider_fallback", "run.completed"]
    assert events[0].data_json["provider_kind"] == "openai"
    assert events[0].data_json["from_api_mode"] == "responses"
    assert events[0].data_json["to_api_mode"] == "chat_completions"
    assert events[0].data_json["status_code"] == 404
    assert events[0].data_json["trace_id"] == "d" * 32


def test_llm_gateway_stream_maps_llm_debug_events_to_run_events() -> None:
    run_id = uuid.UUID(int=7)
    emitter = RunEventEmitter(
        run_id=run_id,
        trace_id="f" * 32,
        event_id_factory=_FakeEventIdFactory(),
        clock=_fixed_clock,
    )

    async def _stub_stream():
        yield LlmStreamLlmRequest(
            llm_call_id="call_1",
            provider_kind="openai",
            api_mode="chat_completions",
            base_url="https://example.test/v1",
            path="/chat/completions",
            payload_json={"model": "gpt-test", "stream": True},
        )
        yield LlmStreamLlmResponseChunk(
            llm_call_id="call_1",
            provider_kind="openai",
            api_mode="chat_completions",
            raw='{"foo":"bar"}',
            chunk_json={"foo": "bar"},
        )
        yield LlmStreamMessageDelta(content_delta="ok", role="assistant")
        yield LlmStreamRunCompleted()

    async def _collect():
        events = []
        async for event in run_events_from_llm_stream(emitter=emitter, stream=_stub_stream()):
            events.append(event)
        return events

    events = anyio.run(_collect)

    assert [event.seq for event in events] == [1, 2, 3, 4]
    assert [event.type for event in events] == [
        "llm.request",
        "llm.response.chunk",
        "message.delta",
        "run.completed",
    ]
    assert events[0].data_json["llm_call_id"] == "call_1"
    assert events[0].data_json["provider_kind"] == "openai"
    assert events[1].data_json["raw"] == '{"foo":"bar"}'
    assert events[1].data_json["json"] == {"foo": "bar"}
    assert all(event.data_json.get("trace_id") == "f" * 32 for event in events)


def test_llm_gateway_stream_closes_underlying_stream_on_completed() -> None:
    emitter = RunEventEmitter(
        run_id=uuid.UUID(int=9),
        trace_id="e" * 32,
        event_id_factory=_FakeEventIdFactory(),
        clock=_fixed_clock,
    )

    closed = False

    async def _stub_stream():
        nonlocal closed
        try:
            yield LlmStreamRunCompleted()
            await anyio.sleep(999)
        finally:
            closed = True

    async def _collect():
        events = []
        stream = _stub_stream()
        async for event in run_events_from_llm_stream(emitter=emitter, stream=stream):
            events.append(event)
        return events

    events = anyio.run(_collect)

    assert [event.type for event in events] == ["run.completed"]
    assert closed is True
