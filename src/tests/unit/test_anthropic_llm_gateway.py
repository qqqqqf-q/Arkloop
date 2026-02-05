from __future__ import annotations

import json

import anyio
import httpx
import pytest

from packages.llm_gateway import (
    ERROR_CLASS_PROVIDER_NON_RETRYABLE,
    LlmGatewayRequest,
    LlmMessage,
    LlmStreamLlmRequest,
    LlmStreamLlmResponseChunk,
    LlmStreamMessageDelta,
    LlmStreamRunCompleted,
    LlmStreamRunFailed,
    LlmStreamToolCall,
    LlmTextPart,
)
from packages.llm_gateway.anthropic import AnthropicGatewayConfig, AnthropicLlmGateway


def test_anthropic_gateway_messages_streams_deltas_and_completed_with_usage() -> None:
    sse = (
        'event: message_start\n'
        'data: {"type":"message_start","message":{"usage":{"input_tokens":1,"output_tokens":0}}}\n\n'
        'event: content_block_delta\n'
        'data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}\n\n'
        'event: content_block_delta\n'
        'data: {"type":"content_block_delta","delta":{"type":"text_delta","text":" world"}}\n\n'
        'event: message_delta\n'
        'data: {"type":"message_delta","usage":{"output_tokens":2}}\n\n'
        'event: message_stop\n'
        'data: {"type":"message_stop"}\n\n'
    )

    def _handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/messages"
        assert request.headers.get("x-api-key") == "sk-test"
        assert request.headers.get("anthropic-version") == "2023-06-01"
        assert request.headers.get("accept") == "text/event-stream"

        payload = json.loads(request.content)
        assert payload["model"] == "claude-test"
        assert payload["stream"] is True
        assert payload["max_tokens"] == 5
        assert payload["messages"] == [{"role": "user", "content": [{"type": "text", "text": "hi"}]}]

        return httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            content=sse.encode("utf-8"),
        )

    transport = httpx.MockTransport(_handler)
    client = httpx.AsyncClient(base_url="https://example.test/v1", transport=transport)
    gateway = AnthropicLlmGateway(
        config=AnthropicGatewayConfig(
            api_key="sk-test",
            base_url="https://example.test/v1",
            anthropic_version="2023-06-01",
            total_timeout_seconds=5.0,
        ),
        client=client,
    )

    request = LlmGatewayRequest(
        model="claude-test",
        messages=[LlmMessage(role="user", content=[LlmTextPart(text="hi")])],
        max_output_tokens=5,
    )

    async def _collect() -> list[object]:
        items: list[object] = []
        async for item in gateway.stream(request=request):
            items.append(item)
        await client.aclose()
        return items

    items = anyio.run(_collect)

    assert [type(item) for item in items] == [
        LlmStreamMessageDelta,
        LlmStreamMessageDelta,
        LlmStreamRunCompleted,
    ]
    assert items[0].content_delta == "hello"
    assert items[1].content_delta == " world"
    assert items[2].usage is not None
    assert items[2].usage.total_tokens == 3


def test_anthropic_gateway_applies_advanced_json_extra_headers_and_query() -> None:
    sse = (
        'data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}\n\n'
        'data: {"type":"message_stop"}\n\n'
    )

    def _handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/messages"
        assert request.url.params.get("foo") == "bar"
        assert request.headers.get("x-test") == "1"
        return httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            content=sse.encode("utf-8"),
        )

    transport = httpx.MockTransport(_handler)
    client = httpx.AsyncClient(base_url="https://example.test/v1", transport=transport)
    gateway = AnthropicLlmGateway(
        config=AnthropicGatewayConfig(
            api_key="sk-test",
            base_url="https://example.test/v1",
            advanced_json={"extra_headers": {"x-test": "1"}, "extra_query": {"foo": "bar"}, "timeout_ms": 5000},
        ),
        client=client,
    )

    request = LlmGatewayRequest(
        model="claude-test",
        messages=[LlmMessage(role="user", content=[LlmTextPart(text="hi")])],
        max_output_tokens=5,
    )

    async def _collect() -> list[object]:
        items: list[object] = []
        async for item in gateway.stream(request=request):
            items.append(item)
        await client.aclose()
        return items

    items = anyio.run(_collect)

    assert [type(item) for item in items] == [LlmStreamMessageDelta, LlmStreamRunCompleted]
    assert items[0].content_delta == "ok"


def test_anthropic_gateway_rejects_advanced_json_with_secret_header_keys() -> None:
    with pytest.raises(ValueError, match="敏感键"):
        AnthropicLlmGateway(
            config=AnthropicGatewayConfig(
                api_key="sk-test",
                advanced_json={"extra_headers": {"Authorization": "Bearer x"}},
            )
        )


def test_anthropic_gateway_http_error_emits_run_failed() -> None:
    def _handler(_request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            401,
            json={"error": {"type": "authentication_error", "message": "bad key"}},
        )

    transport = httpx.MockTransport(_handler)
    client = httpx.AsyncClient(base_url="https://example.test/v1", transport=transport)
    gateway = AnthropicLlmGateway(
        config=AnthropicGatewayConfig(
            api_key="sk-test",
            base_url="https://example.test/v1",
            total_timeout_seconds=5.0,
        ),
        client=client,
    )

    request = LlmGatewayRequest(
        model="claude-test",
        messages=[LlmMessage(role="user", content=[LlmTextPart(text="hi")])],
        max_output_tokens=5,
    )

    async def _collect() -> list[object]:
        items: list[object] = []
        async for item in gateway.stream(request=request):
            items.append(item)
        await client.aclose()
        return items

    items = anyio.run(_collect)

    assert [type(item) for item in items] == [LlmStreamRunFailed]
    assert items[0].error.error_class == ERROR_CLASS_PROVIDER_NON_RETRYABLE
    assert items[0].error.details.get("status_code") == 401


def test_anthropic_gateway_emits_tool_call_placeholder_events() -> None:
    sse = (
        'data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"search","input":{"q":"hi"}}}\n\n'
        'data: {"type":"message_stop"}\n\n'
    )

    def _handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/messages"
        return httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            content=sse.encode("utf-8"),
        )

    transport = httpx.MockTransport(_handler)
    client = httpx.AsyncClient(base_url="https://example.test/v1", transport=transport)
    gateway = AnthropicLlmGateway(
        config=AnthropicGatewayConfig(
            api_key="sk-test",
            base_url="https://example.test/v1",
            total_timeout_seconds=5.0,
        ),
        client=client,
    )

    request = LlmGatewayRequest(
        model="claude-test",
        messages=[LlmMessage(role="user", content=[LlmTextPart(text="hi")])],
        max_output_tokens=5,
    )

    async def _collect() -> list[object]:
        items: list[object] = []
        async for item in gateway.stream(request=request):
            items.append(item)
        await client.aclose()
        return items

    items = anyio.run(_collect)

    assert [type(item) for item in items] == [LlmStreamToolCall, LlmStreamRunCompleted]
    assert items[0].tool_call_id == "toolu_1"
    assert items[0].tool_name == "search"
    assert items[0].arguments_json == {"q": "hi"}


def test_anthropic_gateway_emits_llm_debug_events_when_enabled() -> None:
    sse = (
        'data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"ok"}}\n\n'
        'data: {"type":"message_stop"}\n\n'
    )

    def _handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/messages"
        return httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            content=sse.encode("utf-8"),
        )

    transport = httpx.MockTransport(_handler)
    client = httpx.AsyncClient(base_url="https://example.test/v1", transport=transport)
    gateway = AnthropicLlmGateway(
        config=AnthropicGatewayConfig(
            api_key="sk-test",
            base_url="https://example.test/v1",
            total_timeout_seconds=5.0,
            emit_llm_debug_events=True,
        ),
        client=client,
    )

    request = LlmGatewayRequest(
        model="claude-test",
        messages=[LlmMessage(role="user", content=[LlmTextPart(text="hi")])],
        max_output_tokens=5,
    )

    async def _collect() -> list[object]:
        items: list[object] = []
        async for item in gateway.stream(request=request):
            items.append(item)
        await client.aclose()
        return items

    items = anyio.run(_collect)

    assert [type(item) for item in items] == [
        LlmStreamLlmRequest,
        LlmStreamLlmResponseChunk,
        LlmStreamMessageDelta,
        LlmStreamLlmResponseChunk,
        LlmStreamRunCompleted,
    ]
    assert items[0].payload_json["model"] == "claude-test"
    assert items[2].content_delta == "ok"
    assert {item.llm_call_id for item in items if hasattr(item, "llm_call_id")} == {items[0].llm_call_id}
