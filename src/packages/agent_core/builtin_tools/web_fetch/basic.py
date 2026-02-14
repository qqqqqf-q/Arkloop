from __future__ import annotations

from dataclasses import dataclass
from html.parser import HTMLParser
import re
from typing import Any

import httpx

from .provider import WebFetchResult

_DEFAULT_MAX_BYTES = 5_000_000
_BYTES_PER_CHAR_ESTIMATE = 16


class BasicWebFetchHttpError(RuntimeError):
    def __init__(self, *, status_code: int, message: str) -> None:
        super().__init__(message)
        self.status_code = status_code


class BasicWebFetchProvider:
    def __init__(self, *, client: httpx.AsyncClient | None = None) -> None:
        self._client = client

    async def fetch(self, *, url: str, max_length: int) -> WebFetchResult:
        client = self._client
        if client is None:
            timeout = httpx.Timeout(None)
            async with httpx.AsyncClient(timeout=timeout, follow_redirects=True) as client:
                return await self._fetch_with_client(client=client, url=url, max_length=max_length)
        return await self._fetch_with_client(client=client, url=url, max_length=max_length)

    async def _fetch_with_client(
        self,
        *,
        client: httpx.AsyncClient,
        url: str,
        max_length: int,
    ) -> WebFetchResult:
        max_bytes = _max_bytes_for_length(max_length)
        async with client.stream("GET", url) as resp:
            if resp.status_code < 200 or resp.status_code >= 300:
                raise BasicWebFetchHttpError(
                    status_code=int(resp.status_code),
                    message=f"web_fetch 返回 {resp.status_code}",
                )

            content_type = (resp.headers.get("content-type") or "").casefold()
            body_bytes, download_truncated = await _read_limited_bytes(resp, max_bytes=max_bytes)
            encoding = resp.encoding or "utf-8"
            body_text = body_bytes.decode(encoding, errors="replace")

        final_url = str(resp.url)
        title = ""
        content = body_text.strip()
        if _looks_like_html(content_type, content):
            title, content = _extract_title_and_text_from_html(body_text)

        truncated = download_truncated
        if len(content) > max_length:
            content = content[:max_length]
            truncated = True

        if len(title) > 512:
            title = title[:512]

        return WebFetchResult(url=final_url, content=content, title=title, truncated=truncated)


def _max_bytes_for_length(max_length: int) -> int:
    estimate = max_length * _BYTES_PER_CHAR_ESTIMATE
    if estimate <= 0:
        return 1024
    return min(int(estimate), _DEFAULT_MAX_BYTES)


async def _read_limited_bytes(resp: httpx.Response, *, max_bytes: int) -> tuple[bytes, bool]:
    if max_bytes <= 0:
        return b"", True

    chunks: list[bytes] = []
    total = 0
    truncated = False
    async for chunk in resp.aiter_bytes():
        if not chunk:
            continue
        remaining = max_bytes - total
        if remaining <= 0:
            truncated = True
            break
        if len(chunk) > remaining:
            chunks.append(chunk[:remaining])
            total += remaining
            truncated = True
            break
        chunks.append(chunk)
        total += len(chunk)
    return b"".join(chunks), truncated


def _looks_like_html(content_type: str, text: str) -> bool:
    if "text/html" in content_type or "application/xhtml+xml" in content_type:
        return True
    prefix = text.lstrip()[:200].casefold()
    return prefix.startswith("<!doctype html") or prefix.startswith("<html")


@dataclass(slots=True)
class _HtmlExtraction:
    title: str = ""
    text: str = ""


class _HtmlToTextParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self._ignored_depth = 0
        self._in_title = False
        self._title_parts: list[str] = []
        self._text_parts: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        _ = attrs
        name = tag.casefold()
        if name in {"script", "style", "noscript"}:
            self._ignored_depth += 1
            return
        if name == "title":
            self._in_title = True
            return
        if self._ignored_depth > 0:
            return
        if name in _BLOCK_TAGS:
            self._text_parts.append("\n")

    def handle_endtag(self, tag: str) -> None:
        name = tag.casefold()
        if name in {"script", "style", "noscript"}:
            if self._ignored_depth > 0:
                self._ignored_depth -= 1
            return
        if name == "title":
            self._in_title = False
            return
        if self._ignored_depth > 0:
            return
        if name in _BLOCK_TAGS:
            self._text_parts.append("\n")

    def handle_data(self, data: str) -> None:
        if self._ignored_depth > 0:
            return
        cleaned = data.strip()
        if not cleaned:
            return
        if self._in_title:
            self._title_parts.append(cleaned)
        else:
            self._text_parts.append(cleaned + " ")

    def extraction(self) -> _HtmlExtraction:
        title = _normalize_text(" ".join(self._title_parts))
        text = _normalize_text("".join(self._text_parts))
        return _HtmlExtraction(title=title, text=text)


_BLOCK_TAGS = {
    "br",
    "p",
    "div",
    "li",
    "ul",
    "ol",
    "article",
    "section",
    "header",
    "footer",
    "nav",
    "main",
    "aside",
    "table",
    "tr",
    "td",
    "th",
    "h1",
    "h2",
    "h3",
    "h4",
    "h5",
    "h6",
}


def _extract_title_and_text_from_html(html: str) -> tuple[str, str]:
    parser = _HtmlToTextParser()
    try:
        parser.feed(html)
        parser.close()
    except Exception:
        return "", _normalize_text(html)
    extracted = parser.extraction()
    return extracted.title, extracted.text


def _normalize_text(value: Any) -> str:
    if not isinstance(value, str):
        return ""
    text = value.replace("\r\n", "\n").replace("\r", "\n")
    text = re.sub(r"[ \t\f\v]+", " ", text)
    text = re.sub(r"[ \t]+\n", "\n", text)
    text = re.sub(r"\n[ \t]+", "\n", text)
    text = re.sub(r"\n{3,}", "\n\n", text)
    return text.strip()


__all__ = [
    "BasicWebFetchHttpError",
    "BasicWebFetchProvider",
]

