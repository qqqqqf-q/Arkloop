from __future__ import annotations

import json
import uuid

import anyio
import httpx
import pytest

from packages.agent_core.builtin_tools.web_search.config import WebSearchConfig
from packages.agent_core.builtin_tools.web_search.executor import WebSearchToolExecutor
from packages.agent_core.builtin_tools.web_search.provider import WebSearchResult
from packages.agent_core.builtin_tools.web_search.searxng import SearxngWebSearchProvider
from packages.agent_core.builtin_tools.web_search.tavily import TavilyWebSearchProvider
from packages.agent_core.executor import ToolExecutionContext


def _context() -> ToolExecutionContext:
    return ToolExecutionContext(run_id=uuid.uuid4(), trace_id="t" * 32)


def test_searxng_provider_maps_results_and_truncates() -> None:
    def _handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "GET"
        assert request.url.path == "/search"
        assert request.url.params.get("q") == "hello"
        assert request.url.params.get("format") == "json"
        return httpx.Response(
            200,
            json={
                "results": [
                    {"title": "t1", "url": "https://a.test", "content": "s1"},
                    {"title": "t2", "url": "https://b.test", "content": "s2"},
                ]
            },
        )

    transport = httpx.MockTransport(_handler)

    async def _run():
        async with httpx.AsyncClient(
            base_url="https://searxng.example.test",
            transport=transport,
        ) as client:
            provider = SearxngWebSearchProvider(base_url="https://searxng.example.test", client=client)
            return await provider.search(query="hello", max_results=1)

    results = anyio.run(_run)
    assert results == [WebSearchResult(title="t1", url="https://a.test", snippet="s1")]


def test_tavily_provider_maps_results_and_truncates() -> None:
    def _handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.path == "/search"
        payload = json.loads(request.content.decode("utf-8"))
        assert payload["api_key"] == "tvly-test"
        assert payload["query"] == "hello"
        assert payload["max_results"] == 1
        assert payload["include_answer"] is False
        assert payload["include_raw_content"] is False
        assert payload["include_images"] is False
        return httpx.Response(
            200,
            json={
                "results": [
                    {"title": "t1", "url": "https://a.test", "content": "s1"},
                    {"title": "t2", "url": "https://b.test", "content": "s2"},
                ]
            },
        )

    transport = httpx.MockTransport(_handler)

    async def _run():
        async with httpx.AsyncClient(
            base_url="https://api.tavily.example.test",
            transport=transport,
        ) as client:
            provider = TavilyWebSearchProvider(
                api_key="tvly-test",
                base_url="https://api.tavily.example.test",
                client=client,
            )
            return await provider.search(query="hello", max_results=1)

    results = anyio.run(_run)
    assert results == [WebSearchResult(title="t1", url="https://a.test", snippet="s1")]


def test_web_search_config_reads_tavily_api_key_env(monkeypatch) -> None:
    monkeypatch.delenv("ARKLOOP_LOAD_DOTENV", raising=False)
    monkeypatch.delenv("ARKLOOP_DOTENV_FILE", raising=False)
    monkeypatch.setenv("ARKLOOP_WEB_SEARCH_PROVIDER", "tavily")
    monkeypatch.setenv("ARKLOOP_WEB_SEARCH_TAVILY_API_KEY", "tvly-test")

    config = WebSearchConfig.from_env(required=True)
    assert config is not None
    assert config.provider_kind == "tavily"
    assert config.tavily_api_key == "tvly-test"
    assert "tvly-test" not in repr(config)


def test_web_search_config_rejects_missing_tavily_api_key(monkeypatch) -> None:
    monkeypatch.delenv("ARKLOOP_LOAD_DOTENV", raising=False)
    monkeypatch.delenv("ARKLOOP_DOTENV_FILE", raising=False)
    monkeypatch.setenv("ARKLOOP_WEB_SEARCH_PROVIDER", "tavily")
    monkeypatch.delenv("ARKLOOP_WEB_SEARCH_TAVILY_API_KEY", raising=False)

    with pytest.raises(ValueError) as exc:
        WebSearchConfig.from_env(required=True)
    assert "ARKLOOP_WEB_SEARCH_TAVILY_API_KEY" in str(exc.value)


def test_web_search_executor_returns_results_schema() -> None:
    class _StubProvider:
        async def search(self, *, query: str, max_results: int) -> list[WebSearchResult]:
            _ = (query, max_results)
            return [
                WebSearchResult(title="t", url="https://example.test", snippet="s"),
            ]

    executor = WebSearchToolExecutor(provider=_StubProvider())

    async def _run():
        return await executor.execute(
            tool_name="web_search",
            args={"query": "hello", "max_results": 5},
            context=_context(),
        )

    result = anyio.run(_run)
    assert result.error is None
    assert result.result_json == {
        "results": [{"title": "t", "url": "https://example.test", "snippet": "s"}]
    }


def test_web_search_executor_times_out() -> None:
    class _SlowProvider:
        async def search(self, *, query: str, max_results: int) -> list[WebSearchResult]:
            _ = (query, max_results)
            await anyio.sleep(60)
            return []

    executor = WebSearchToolExecutor(provider=_SlowProvider(), timeout_seconds=0.01)

    async def _run():
        return await executor.execute(
            tool_name="web_search",
            args={"query": "hello", "max_results": 5},
            context=_context(),
        )

    result = anyio.run(_run)
    assert result.error is not None
    assert result.error.error_class == "tool.timeout"


def test_web_search_executor_errors_when_backend_not_configured() -> None:
    executor = WebSearchToolExecutor(provider_factory=lambda: None)

    async def _run():
        return await executor.execute(
            tool_name="web_search",
            args={"query": "hello", "max_results": 5},
            context=_context(),
        )

    result = anyio.run(_run)
    assert result.error is not None
    assert result.error.error_class == "tool.not_configured"
