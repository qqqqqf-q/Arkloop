from __future__ import annotations

import json

import anyio
import httpx

from packages.llm_gateway import (
    LlmGatewayRequest,
    LlmMessage,
    LlmStreamMessageDelta,
    LlmStreamProviderFallback,
    LlmStreamRunCompleted,
    LlmTextPart,
)
from packages.llm_gateway.openai import OpenAiGatewayConfig, OpenAiLlmGateway


def test_openai_gateway_chat_completions_streams_deltas_and_completed() -> None:
    sse = (
        'data: {"choices":[{"delta":{"role":"assistant","content":"hello"}}]}\n\n'
        'data: {"choices":[{"delta":{"content":" world"}}]}\n\n'
        "data: [DONE]\n\n"
    )

    def _handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/chat/completions"
        assert request.headers.get("authorization") == "Bearer sk-test"

        payload = json.loads(request.content)
        assert payload["model"] == "gpt-test"
        assert payload["stream"] is True
        assert payload["messages"] == [{"role": "user", "content": "hi"}]

        return httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            content=sse.encode("utf-8"),
        )

    transport = httpx.MockTransport(_handler)
    client = httpx.AsyncClient(base_url="https://example.test/v1", transport=transport)
    gateway = OpenAiLlmGateway(
        config=OpenAiGatewayConfig(
            api_key="sk-test",
            base_url="https://example.test/v1",
            api_mode="chat_completions",
            total_timeout_seconds=5.0,
        ),
        client=client,
    )

    request = LlmGatewayRequest(
        model="gpt-test",
        messages=[LlmMessage(role="user", content=[LlmTextPart(text="hi")])],
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


def test_openai_gateway_responses_streams_deltas_and_completed_with_usage() -> None:
    sse = (
        'data: {"type":"response.output_text.delta","delta":"hello"}\n\n'
        'data: {"type":"response.output_text.delta","delta":" world"}\n\n'
        'data: {"type":"response.completed","response":{"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}\n\n'
    )

    def _handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/responses"
        assert request.headers.get("authorization") == "Bearer sk-test"

        payload = json.loads(request.content)
        assert payload["model"] == "gpt-test"
        assert payload["stream"] is True
        assert payload["input"] == [{"role": "user", "content": [{"type": "input_text", "text": "hi"}]}]

        return httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            content=sse.encode("utf-8"),
        )

    transport = httpx.MockTransport(_handler)
    client = httpx.AsyncClient(base_url="https://example.test/v1", transport=transport)
    gateway = OpenAiLlmGateway(
        config=OpenAiGatewayConfig(
            api_key="sk-test",
            base_url="https://example.test/v1",
            api_mode="responses",
            total_timeout_seconds=5.0,
        ),
        client=client,
    )

    request = LlmGatewayRequest(
        model="gpt-test",
        messages=[LlmMessage(role="user", content=[LlmTextPart(text="hi")])],
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


def test_openai_gateway_auto_falls_back_from_responses_to_chat_completions() -> None:
    sse = 'data: {"choices":[{"delta":{"content":"ok"}}]}\n\n' "data: [DONE]\n\n"
    call_paths: list[str] = []

    def _handler(request: httpx.Request) -> httpx.Response:
        call_paths.append(request.url.path)
        if request.url.path == "/v1/responses":
            return httpx.Response(
                404,
                json={"error": {"message": "Not Found", "type": "not_found"}},
            )
        if request.url.path == "/v1/chat/completions":
            return httpx.Response(
                200,
                headers={"content-type": "text/event-stream"},
                content=sse.encode("utf-8"),
            )
        raise AssertionError(f"unexpected path: {request.url.path}")

    transport = httpx.MockTransport(_handler)
    client = httpx.AsyncClient(base_url="https://example.test/v1", transport=transport)
    gateway = OpenAiLlmGateway(
        config=OpenAiGatewayConfig(
            api_key="sk-test",
            base_url="https://example.test/v1",
            api_mode="auto",
            total_timeout_seconds=5.0,
        ),
        client=client,
    )

    request = LlmGatewayRequest(
        model="gpt-test",
        messages=[LlmMessage(role="user", content=[LlmTextPart(text="hi")])],
    )

    async def _collect() -> list[object]:
        items: list[object] = []
        async for item in gateway.stream(request=request):
            items.append(item)
        await client.aclose()
        return items

    items = anyio.run(_collect)

    assert call_paths == ["/v1/responses", "/v1/chat/completions"]
    assert [type(item) for item in items] == [
        LlmStreamProviderFallback,
        LlmStreamMessageDelta,
        LlmStreamRunCompleted,
    ]
    assert items[0].from_api_mode == "responses"
    assert items[0].to_api_mode == "chat_completions"
    assert items[0].status_code == 404
