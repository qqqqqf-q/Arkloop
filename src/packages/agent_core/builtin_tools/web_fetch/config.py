from __future__ import annotations

from dataclasses import dataclass
import os
from typing import Literal, Optional

from packages.config import load_dotenv_if_enabled

WebFetchProviderKind = Literal["basic", "firecrawl", "jina"]

_WEB_FETCH_PROVIDER_ENV = "ARKLOOP_WEB_FETCH_PROVIDER"


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

    @classmethod
    def from_env(cls, *, required: bool = False) -> Optional["WebFetchConfig"]:
        load_dotenv_if_enabled(override=False)

        raw = os.getenv(_WEB_FETCH_PROVIDER_ENV)
        if not raw:
            if required:
                raise ValueError(f"缺少环境变量 {_WEB_FETCH_PROVIDER_ENV}")
            return None

        provider_kind = _parse_provider_kind(raw)
        return cls(provider_kind=provider_kind)


__all__ = ["WebFetchConfig", "WebFetchProviderKind"]

