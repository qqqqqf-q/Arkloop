from __future__ import annotations

import json
from typing import Any, Mapping

import httpx

from .provider import WebSearchResult

_DEFAULT_TAVILY_BASE_URL = "https://api.tavily.com"


class TavilySearchHttpError(RuntimeError):
    def __init__(self, *, status_code: int, message: str) -> None:
        super().__init__(message)
        self.status_code = status_code


class TavilyWebSearchProvider:
    def __init__(
        self,
        *,
        api_key: str,
        base_url: str = _DEFAULT_TAVILY_BASE_URL,
        client: httpx.AsyncClient | None = None,
    ) -> None:
        self._api_key = api_key
        self._base_url = base_url.rstrip("/")
        self._client = client

    async def search(self, *, query: str, max_results: int) -> list[WebSearchResult]:
        client = self._client
        if client is None:
            timeout = httpx.Timeout(None)
            async with httpx.AsyncClient(base_url=self._base_url, timeout=timeout) as client:
                return await self._search_with_client(client=client, query=query, max_results=max_results)
        return await self._search_with_client(client=client, query=query, max_results=max_results)

    async def _search_with_client(
        self,
        *,
        client: httpx.AsyncClient,
        query: str,
        max_results: int,
    ) -> list[WebSearchResult]:
        resp = await client.post(
            "/search",
            json={
                "api_key": self._api_key,
                "query": query,
                "max_results": max_results,
                "include_answer": False,
                "include_raw_content": False,
                "include_images": False,
            },
        )
        if resp.status_code != 200:
            raise TavilySearchHttpError(status_code=int(resp.status_code), message=f"Tavily 返回 {resp.status_code}")

        payload = _parse_json_bytes(resp.content)
        if not isinstance(payload, Mapping):
            raise ValueError("Tavily JSON 响应必须为对象")
        raw_results = payload.get("results")
        if not isinstance(raw_results, list):
            raise ValueError("Tavily JSON 响应 results 必须为数组")

        results: list[WebSearchResult] = []
        for item in raw_results:
            if len(results) >= max_results:
                break
            if not isinstance(item, Mapping):
                continue

            title = _as_non_empty_str(item.get("title"))
            url = _as_non_empty_str(item.get("url"))
            snippet = _as_str(item.get("content")) or ""
            if title is None or url is None:
                continue
            results.append(WebSearchResult(title=title, url=url, snippet=snippet))

        return results


def _parse_json_bytes(value: bytes) -> Any:
    text = value.decode("utf-8", errors="replace")
    return json.loads(text)


def _as_str(value: Any) -> str | None:
    if isinstance(value, str):
        cleaned = value.strip()
        return cleaned if cleaned else None
    return None


def _as_non_empty_str(value: Any) -> str | None:
    return _as_str(value)


__all__ = [
    "TavilySearchHttpError",
    "TavilyWebSearchProvider",
]
