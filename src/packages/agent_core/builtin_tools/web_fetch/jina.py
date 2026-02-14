from __future__ import annotations

import httpx

from .provider import WebFetchResult

_DEFAULT_JINA_BASE_URL = "https://r.jina.ai"


class JinaWebFetchHttpError(RuntimeError):
    def __init__(self, *, status_code: int, message: str) -> None:
        super().__init__(message)
        self.status_code = status_code


class JinaWebFetchProvider:
    def __init__(
        self,
        *,
        api_key: str,
        base_url: str = _DEFAULT_JINA_BASE_URL,
        client: httpx.AsyncClient | None = None,
    ) -> None:
        cleaned = api_key.strip()
        if not cleaned:
            raise ValueError("Jina api_key 不能为空")
        self._api_key = cleaned
        self._base_url = base_url.rstrip("/")
        self._client = client

    async def fetch(self, *, url: str, max_length: int) -> WebFetchResult:
        client = self._client
        if client is None:
            timeout = httpx.Timeout(None)
            async with httpx.AsyncClient(timeout=timeout) as client:
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

        request_url = f"{self._base_url}/{url}"
        resp = await client.get(request_url, headers=headers)
        if resp.status_code < 200 or resp.status_code >= 300:
            raise JinaWebFetchHttpError(
                status_code=int(resp.status_code),
                message=f"Jina Reader 返回 {resp.status_code}",
            )

        content = (resp.text or "").strip()
        title = _extract_title_from_markdown(content)

        truncated = False
        if len(content) > max_length:
            content = content[:max_length]
            truncated = True
        if len(title) > 512:
            title = title[:512]

        return WebFetchResult(url=url, content=content, title=title, truncated=truncated)


def _extract_title_from_markdown(text: str) -> str:
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped:
            continue
        if stripped.startswith("# "):
            return stripped[2:].strip()
        lowered = stripped.casefold()
        if lowered.startswith("title:"):
            return stripped.split(":", 1)[1].strip()
        return ""
    return ""


__all__ = [
    "JinaWebFetchHttpError",
    "JinaWebFetchProvider",
]
