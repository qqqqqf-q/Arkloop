from __future__ import annotations

import json
from typing import Any, Mapping

import httpx

from .provider import WebFetchResult

_DEFAULT_FIRECRAWL_BASE_URL = "https://api.firecrawl.dev"


class FirecrawlWebFetchHttpError(RuntimeError):
    def __init__(self, *, status_code: int, message: str) -> None:
        super().__init__(message)
        self.status_code = status_code


class FirecrawlWebFetchProvider:
    def __init__(
        self,
        *,
        api_key: str | None = None,
        base_url: str = _DEFAULT_FIRECRAWL_BASE_URL,
        client: httpx.AsyncClient | None = None,
    ) -> None:
        cleaned = api_key.strip() if isinstance(api_key, str) else ""
        self._api_key = cleaned or None
        self._base_url = base_url.rstrip("/")
        self._client = client

    async def fetch(self, *, url: str, max_length: int) -> WebFetchResult:
        client = self._client
        if client is None:
            timeout = httpx.Timeout(None)
            async with httpx.AsyncClient(base_url=self._base_url, timeout=timeout) as client:
                return await self._fetch_with_client(client=client, url=url, max_length=max_length)
        return await self._fetch_with_client(client=client, url=url, max_length=max_length)

    async def _fetch_with_client(
        self,
        *,
        client: httpx.AsyncClient,
        url: str,
        max_length: int,
    ) -> WebFetchResult:
        headers: dict[str, str] = {}
        if self._api_key:
            headers["Authorization"] = f"Bearer {self._api_key}"
            headers["x-api-key"] = self._api_key

        resp = await client.post(
            "/v1/scrape",
            headers=headers,
            json={
                "url": url,
                "formats": ["markdown"],
                "onlyMainContent": True,
            },
        )
        if resp.status_code != 200:
            raise FirecrawlWebFetchHttpError(
                status_code=int(resp.status_code),
                message=f"Firecrawl 返回 {resp.status_code}",
            )

        payload = _parse_json_bytes(resp.content)
        if not isinstance(payload, Mapping):
            raise ValueError("Firecrawl JSON 响应必须为对象")
        if payload.get("success") is False:
            raise ValueError("Firecrawl 响应 success=false")
        raw_data = payload.get("data")
        if not isinstance(raw_data, Mapping):
            raise ValueError("Firecrawl JSON 响应 data 必须为对象")

        content = _as_str(raw_data.get("markdown")) or _as_str(raw_data.get("content")) or ""
        title = ""
        meta = raw_data.get("metadata")
        if isinstance(meta, Mapping):
            title = _as_str(meta.get("title")) or ""
        if not title:
            title = _as_str(raw_data.get("title")) or ""

        final_url = _as_str(raw_data.get("url")) or url
        truncated = False
        if len(content) > max_length:
            content = content[:max_length]
            truncated = True
        if len(title) > 512:
            title = title[:512]

        return WebFetchResult(
            url=final_url,
            content=content,
            title=title,
            truncated=truncated,
        )


def _parse_json_bytes(value: bytes) -> Any:
    text = value.decode("utf-8", errors="replace")
    return json.loads(text)


def _as_str(value: Any) -> str | None:
    if isinstance(value, str):
        cleaned = value.strip()
        return cleaned if cleaned else None
    return None


__all__ = [
    "FirecrawlWebFetchHttpError",
    "FirecrawlWebFetchProvider",
]
