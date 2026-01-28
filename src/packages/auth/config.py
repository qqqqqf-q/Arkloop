from __future__ import annotations

from dataclasses import dataclass
import os
from typing import Optional

from packages.config import load_dotenv_if_enabled

_JWT_SECRET_ENV = "ARKLOOP_AUTH_JWT_SECRET"
_ACCESS_TOKEN_TTL_SECONDS_ENV = "ARKLOOP_AUTH_ACCESS_TOKEN_TTL_SECONDS"

_DEFAULT_ACCESS_TOKEN_TTL_SECONDS = 3600
_MIN_SECRET_LENGTH = 32


def _parse_positive_int(value: str) -> int:
    cleaned = value.strip()
    parsed = int(cleaned)
    if parsed <= 0:
        raise ValueError("必须为正整数")
    return parsed


@dataclass(frozen=True, slots=True)
class AuthConfig:
    jwt_secret: str
    access_token_ttl_seconds: int = _DEFAULT_ACCESS_TOKEN_TTL_SECONDS

    @classmethod
    def from_env(cls, *, required: bool = False) -> Optional["AuthConfig"]:
        load_dotenv_if_enabled(override=False)

        secret = (os.getenv(_JWT_SECRET_ENV) or "").strip()
        if not secret:
            if required:
                raise ValueError(f"缺少环境变量 {_JWT_SECRET_ENV}")
            return None
        if len(secret) < _MIN_SECRET_LENGTH:
            raise ValueError(f"{_JWT_SECRET_ENV} 太短，至少 {_MIN_SECRET_LENGTH} 字符")

        ttl_seconds = _DEFAULT_ACCESS_TOKEN_TTL_SECONDS
        raw_ttl = os.getenv(_ACCESS_TOKEN_TTL_SECONDS_ENV)
        if raw_ttl:
            ttl_seconds = _parse_positive_int(raw_ttl)

        return cls(jwt_secret=secret, access_token_ttl_seconds=ttl_seconds)


__all__ = ["AuthConfig"]

