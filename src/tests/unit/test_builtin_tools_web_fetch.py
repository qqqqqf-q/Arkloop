from __future__ import annotations

import uuid

import anyio
import httpx
import pytest

from packages.agent_core.builtin_tools.web_fetch.basic import BasicWebFetchProvider
from packages.agent_core.builtin_tools.web_fetch.executor import WebFetchToolExecutor
from packages.agent_core.builtin_tools.web_fetch.provider import WebFetchResult
from packages.agent_core.builtin_tools.web_fetch.url_policy import UrlPolicyDeniedError, ensure_url_allowed
from packages.agent_core.executor import ToolExecutionContext


def _context() -> ToolExecutionContext:
    return ToolExecutionContext(run_id=uuid.uuid4(), trace_id="t" * 32)


@pytest.mark.parametrize(
    "url",
    [
        "http://127.0.0.1/",
        "http://10.0.0.1/",
        "http://192.168.0.1/",
        "http://169.254.1.1/",
        "http://localhost/",
        "http://[::1]/",
        "http://[fe80::1]/",
    ],
)
def test_url_policy_rejects_private_urls(url: str) -> None:
    with pytest.raises(UrlPolicyDeniedError):
        ensure_url_allowed(url)


def test_basic_provider_extracts_title_and_text() -> None:
    html = """
    <html>
      <head>
        <title>Example Title</title>
        <style>body{display:none}</style>
      </head>
      <body>
        <h1>Hello</h1>
        <p>World</p>
        <script>evil()</script>
      </body>
    </html>
    """.strip()

    def _handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "GET"
        assert str(request.url) == "https://example.test/page"
        return httpx.Response(
            200,
            headers={"content-type": "text/html; charset=utf-8"},
            text=html,
        )

    transport = httpx.MockTransport(_handler)

    async def _run() -> WebFetchResult:
        async with httpx.AsyncClient(transport=transport, follow_redirects=True) as client:
            provider = BasicWebFetchProvider(client=client)
            return await provider.fetch(url="https://example.test/page", max_length=10_000)

    result = anyio.run(_run)
    assert result.title == "Example Title"
    assert "Hello" in result.content
    assert "World" in result.content
    assert "evil()" not in result.content
    assert result.truncated is False


def test_basic_provider_truncates_content() -> None:
    html = "<html><head><title>x</title></head><body>abcdef</body></html>"

    def _handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            headers={"content-type": "text/html; charset=utf-8"},
            text=html,
        )

    transport = httpx.MockTransport(_handler)

    async def _run() -> WebFetchResult:
        async with httpx.AsyncClient(transport=transport, follow_redirects=True) as client:
            provider = BasicWebFetchProvider(client=client)
            return await provider.fetch(url="https://example.test/page", max_length=3)

    result = anyio.run(_run)
    assert result.truncated is True
    assert len(result.content) == 3


def test_web_fetch_executor_returns_schema() -> None:
    class _StubProvider:
        async def fetch(self, *, url: str, max_length: int) -> WebFetchResult:
            _ = (url, max_length)
            return WebFetchResult(
                url="https://example.test/page",
                title="t",
                content="c",
                truncated=False,
            )

    executor = WebFetchToolExecutor(provider=_StubProvider())

    async def _run():
        return await executor.execute(
            tool_name="web_fetch",
            args={"url": "https://example.test/page", "max_length": 5},
            context=_context(),
        )

    result = anyio.run(_run)
    assert result.error is None
    assert result.result_json == {
        "content": "c",
        "title": "t",
        "url": "https://example.test/page",
        "truncated": False,
    }


def test_web_fetch_executor_times_out() -> None:
    class _SlowProvider:
        async def fetch(self, *, url: str, max_length: int) -> WebFetchResult:
            _ = (url, max_length)
            await anyio.sleep(60)
            return WebFetchResult(url=url, title="", content="", truncated=False)

    executor = WebFetchToolExecutor(provider=_SlowProvider(), timeout_seconds=0.01)

    async def _run():
        return await executor.execute(
            tool_name="web_fetch",
            args={"url": "https://example.test/page", "max_length": 5},
            context=_context(),
        )

    result = anyio.run(_run)
    assert result.error is not None
    assert result.error.error_class == "tool.timeout"


def test_web_fetch_executor_rejects_private_url() -> None:
    class _ExplodingProvider:
        async def fetch(self, *, url: str, max_length: int) -> WebFetchResult:
            raise AssertionError("不应触发 fetch")

    executor = WebFetchToolExecutor(provider=_ExplodingProvider())

    async def _run():
        return await executor.execute(
            tool_name="web_fetch",
            args={"url": "http://127.0.0.1/", "max_length": 5},
            context=_context(),
        )

    result = anyio.run(_run)
    assert result.error is not None
    assert result.error.error_class == "tool.url_denied"


def test_web_fetch_executor_falls_back_when_backend_not_configured() -> None:
    class _FallbackProvider:
        async def fetch(self, *, url: str, max_length: int) -> WebFetchResult:
            _ = (url, max_length)
            return WebFetchResult(url=url, title="ok", content="hi", truncated=False)

    executor = WebFetchToolExecutor(
        provider_factory=lambda: None,
        fallback_provider_factory=_FallbackProvider,
    )

    async def _run():
        return await executor.execute(
            tool_name="web_fetch",
            args={"url": "https://example.test/page", "max_length": 5},
            context=_context(),
        )

    result = anyio.run(_run)
    assert result.error is None
    assert result.result_json is not None
    assert result.result_json["title"] == "ok"

