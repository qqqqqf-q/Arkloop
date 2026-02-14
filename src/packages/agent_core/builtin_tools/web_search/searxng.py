from __future__ import annotations

from typing import Any, Mapping

import httpx

from .provider import WebSearchResult


class SearxngSearchHttpError(RuntimeError):
    def __init__(self, *, status_code: int, message: str) -> None:
        super().__init__(message)
        self.status_code = status_code


class SearxngWebSearchProvider:
    def __init__(self, *, base_url: str, client: httpx.AsyncClient | None = None) -> None:
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
        resp = await client.get(
            "/search",
            params={
                "q": query,
                "format": "json",
            },
        )
        if resp.status_code != 200:
            raise SearxngSearchHttpError(status_code=int(resp.status_code), message=f"SearXNG 返回 {resp.status_code}")

        payload = resp.json()
        if not isinstance(payload, Mapping):
            raise ValueError("SearXNG JSON 响应必须为对象")
        raw_results = payload.get("results")
        if not isinstance(raw_results, list):
            raise ValueError("SearXNG JSON 响应 results 必须为数组")

        results: list[WebSearchResult] = []
        for item in raw_results:
            if len(results) >= max_results:
                break
            if not isinstance(item, Mapping):
                continue

            title = _as_non_empty_str(item.get("title"))
            url = _as_non_empty_str(item.get("url"))
            snippet = _as_str(item.get("content")) or _as_str(item.get("snippet")) or ""
            if title is None or url is None:
                continue
            results.append(WebSearchResult(title=title, url=url, snippet=snippet))

        return results


def _as_str(value: Any) -> str | None:
    if isinstance(value, str):
        cleaned = value.strip()
        return cleaned if cleaned else None
    return None


def _as_non_empty_str(value: Any) -> str | None:
    return _as_str(value)


__all__ = ["SearxngSearchHttpError", "SearxngWebSearchProvider"]
