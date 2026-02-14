from __future__ import annotations

from dataclasses import dataclass, field
import os
from typing import Literal, Optional

from packages.config import load_dotenv_if_enabled

WebSearchProviderKind = Literal["searxng", "tavily", "serper"]

_WEB_SEARCH_PROVIDER_ENV = "ARKLOOP_WEB_SEARCH_PROVIDER"
_SEARXNG_BASE_URL_ENV = "ARKLOOP_WEB_SEARCH_SEARXNG_BASE_URL"
_TAVILY_API_KEY_ENV = "ARKLOOP_WEB_SEARCH_TAVILY_API_KEY"


def _normalize_base_url(value: str) -> str:
    cleaned = value.strip()
    if not cleaned:
        raise ValueError("base_url 不能为空")
    return cleaned.rstrip("/")


def _parse_provider_kind(value: str) -> WebSearchProviderKind:
    cleaned = value.strip()
    if not cleaned:
        raise ValueError(f"环境变量 {_WEB_SEARCH_PROVIDER_ENV} 不能为空")
    normalized = cleaned.casefold().replace("-", "_")
    if normalized in {"searxng", "tavily", "serper"}:
        return normalized  # type: ignore[return-value]
    raise ValueError(f"{_WEB_SEARCH_PROVIDER_ENV} 必须为 searxng/tavily/serper")


@dataclass(frozen=True, slots=True)
class WebSearchConfig:
    provider_kind: WebSearchProviderKind
    searxng_base_url: str | None = None
    tavily_api_key: str | None = field(default=None, repr=False)

    @classmethod
    def from_env(cls, *, required: bool = False) -> Optional["WebSearchConfig"]:
        load_dotenv_if_enabled(override=False)

        raw = os.getenv(_WEB_SEARCH_PROVIDER_ENV)
        if not raw:
            if required:
                raise ValueError(f"缺少环境变量 {_WEB_SEARCH_PROVIDER_ENV}")
            return None

        provider_kind = _parse_provider_kind(raw)
        if provider_kind == "searxng":
            raw_base_url = os.getenv(_SEARXNG_BASE_URL_ENV)
            if not raw_base_url:
                raise ValueError(f"缺少环境变量 {_SEARXNG_BASE_URL_ENV}")
            return cls(provider_kind=provider_kind, searxng_base_url=_normalize_base_url(raw_base_url))

        if provider_kind == "tavily":
            api_key = (os.getenv(_TAVILY_API_KEY_ENV) or "").strip()
            if not api_key:
                raise ValueError(f"缺少环境变量 {_TAVILY_API_KEY_ENV}")
            return cls(provider_kind=provider_kind, tavily_api_key=api_key)

        return cls(provider_kind=provider_kind)


__all__ = [
    "WebSearchConfig",
    "WebSearchProviderKind",
]
