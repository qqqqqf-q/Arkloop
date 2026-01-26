from __future__ import annotations

from dataclasses import dataclass
import os
from typing import Optional
from urllib.parse import SplitResult, urlsplit, urlunsplit

_PRIMARY_DATABASE_URL_ENV = "ARKLOOP_DATABASE_URL"
_FALLBACK_DATABASE_URL_ENV = "DATABASE_URL"


def _replace_scheme(parsed: SplitResult, scheme: str) -> str:
    return urlunsplit((scheme, parsed.netloc, parsed.path, parsed.query, parsed.fragment))


def normalize_postgres_async_url(database_url: str) -> str:
    cleaned = database_url.strip()
    parsed = urlsplit(cleaned)
    scheme = parsed.scheme.casefold()

    if scheme in {"postgres", "postgresql"}:
        return _replace_scheme(parsed, "postgresql+asyncpg")

    if scheme == "postgresql+asyncpg":
        return cleaned

    if scheme.startswith("postgresql"):
        raise ValueError("当前仅支持 postgresql+asyncpg 作为异步驱动")

    raise ValueError("仅支持 PostgreSQL 连接串（postgresql:// 或 postgresql+asyncpg://）")


@dataclass(frozen=True)
class DatabaseConfig:
    url: str

    @classmethod
    def from_env(
        cls,
        *,
        required: bool = False,
        allow_fallback: bool = True,
    ) -> Optional["DatabaseConfig"]:
        raw = os.getenv(_PRIMARY_DATABASE_URL_ENV)
        if allow_fallback and not raw:
            raw = os.getenv(_FALLBACK_DATABASE_URL_ENV)
        if not raw:
            if required:
                raise ValueError(f"缺少环境变量 {_PRIMARY_DATABASE_URL_ENV}")
            return None
        return cls(url=normalize_postgres_async_url(raw))
