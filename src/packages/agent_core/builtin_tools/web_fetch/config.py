from __future__ import annotations

from dataclasses import dataclass, field
import os
from typing import Literal, Optional

from packages.config import load_dotenv_if_enabled

WebFetchProviderKind = Literal["basic", "firecrawl", "jina"]

_WEB_FETCH_PROVIDER_ENV = "ARKLOOP_WEB_FETCH_PROVIDER"
_FIRECRAWL_API_KEY_ENV = "ARKLOOP_WEB_FETCH_FIRECRAWL_API_KEY"
_FIRECRAWL_BASE_URL_ENV = "ARKLOOP_WEB_FETCH_FIRECRAWL_BASE_URL"
_JINA_API_KEY_ENV = "ARKLOOP_WEB_FETCH_JINA_API_KEY"


def _normalize_api_key(value: str, *, env: str) -> str:
    cleaned = value.strip()
    if not cleaned:
        raise ValueError(f"缺少环境变量 {env}")
    return cleaned


def _normalize_base_url(value: str, *, env: str) -> str:
    cleaned = value.strip()
    if not cleaned:
        raise ValueError(f"环境变量 {env} 不能为空")
    return cleaned.rstrip("/")


def _parse_provider_kind(value: str) -> WebFetchProviderKind:
    cleaned = value.strip()
    if not cleaned:
        raise ValueError(f"环境变量 {_WEB_FETCH_PROVIDER_ENV} 不能为空")
    normalized = cleaned.casefold().replace("-", "_")
    if normalized in {"basic", "firecrawl", "jina"}:
        return normalized  # type: ignore[return-value]
    raise ValueError(f"{_WEB_FETCH_PROVIDER_ENV} 必须为 basic/firecrawl/jina")


@dataclass(frozen=True, slots=True)
class WebFetchConfig:
    provider_kind: WebFetchProviderKind
    firecrawl_api_key: str | None = field(default=None, repr=False)
    firecrawl_base_url: str | None = None
    jina_api_key: str | None = field(default=None, repr=False)

    @classmethod
    def from_env(cls, *, required: bool = False) -> Optional["WebFetchConfig"]:
        load_dotenv_if_enabled(override=False)

        raw = os.getenv(_WEB_FETCH_PROVIDER_ENV)
        if not raw:
            if required:
                raise ValueError(f"缺少环境变量 {_WEB_FETCH_PROVIDER_ENV}")
            return None

        provider_kind = _parse_provider_kind(raw)
        if provider_kind == "firecrawl":
            api_key_raw = os.getenv(_FIRECRAWL_API_KEY_ENV)
            api_key = api_key_raw.strip() if isinstance(api_key_raw, str) else ""
            base_url_raw = os.getenv(_FIRECRAWL_BASE_URL_ENV)
            base_url = None
            if isinstance(base_url_raw, str) and base_url_raw.strip():
                base_url = _normalize_base_url(base_url_raw, env=_FIRECRAWL_BASE_URL_ENV)
            return cls(
                provider_kind=provider_kind,
                firecrawl_api_key=api_key or None,
                firecrawl_base_url=base_url,
            )

        if provider_kind == "jina":
            api_key = _normalize_api_key(os.getenv(_JINA_API_KEY_ENV) or "", env=_JINA_API_KEY_ENV)
            return cls(provider_kind=provider_kind, jina_api_key=api_key)

        return cls(provider_kind=provider_kind)


__all__ = ["WebFetchConfig", "WebFetchProviderKind"]
