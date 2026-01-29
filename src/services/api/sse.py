from __future__ import annotations

from dataclasses import dataclass
import os

from fastapi import FastAPI, Request

from packages.config import load_dotenv_if_enabled

from .error_envelope import ApiError

_SSE_POLL_SECONDS_ENV = "ARKLOOP_SSE_POLL_SECONDS"
_SSE_HEARTBEAT_SECONDS_ENV = "ARKLOOP_SSE_HEARTBEAT_SECONDS"
_SSE_BATCH_LIMIT_ENV = "ARKLOOP_SSE_BATCH_LIMIT"

_DEFAULT_SSE_POLL_SECONDS = 0.25
_DEFAULT_SSE_HEARTBEAT_SECONDS = 15.0
_DEFAULT_SSE_BATCH_LIMIT = 500


def _parse_non_negative_float(value: str) -> float:
    cleaned = value.strip()
    parsed = float(cleaned)
    if parsed < 0:
        raise ValueError("必须为非负数")
    return parsed


def _parse_positive_int(value: str) -> int:
    cleaned = value.strip()
    parsed = int(cleaned)
    if parsed <= 0:
        raise ValueError("必须为正整数")
    return parsed


@dataclass(frozen=True, slots=True)
class SseConfig:
    poll_seconds: float = _DEFAULT_SSE_POLL_SECONDS
    heartbeat_seconds: float = _DEFAULT_SSE_HEARTBEAT_SECONDS
    batch_limit: int = _DEFAULT_SSE_BATCH_LIMIT

    @classmethod
    def from_env(cls) -> "SseConfig":
        load_dotenv_if_enabled(override=False)

        poll_seconds = _DEFAULT_SSE_POLL_SECONDS
        raw_poll = os.getenv(_SSE_POLL_SECONDS_ENV)
        if raw_poll:
            poll_seconds = _parse_non_negative_float(raw_poll)

        heartbeat_seconds = _DEFAULT_SSE_HEARTBEAT_SECONDS
        raw_heartbeat = os.getenv(_SSE_HEARTBEAT_SECONDS_ENV)
        if raw_heartbeat:
            heartbeat_seconds = _parse_non_negative_float(raw_heartbeat)

        batch_limit = _DEFAULT_SSE_BATCH_LIMIT
        raw_batch = os.getenv(_SSE_BATCH_LIMIT_ENV)
        if raw_batch:
            batch_limit = _parse_positive_int(raw_batch)

        return cls(
            poll_seconds=poll_seconds, heartbeat_seconds=heartbeat_seconds, batch_limit=batch_limit
        )


def install_sse_config(app: FastAPI, config: SseConfig) -> None:
    app.state.sse_config = config


def configure_sse(app: FastAPI) -> None:
    install_sse_config(app, SseConfig.from_env())


def _get_installed_sse_config(app: FastAPI) -> SseConfig:
    config = getattr(app.state, "sse_config", None)
    if isinstance(config, SseConfig):
        return config
    raise ApiError(code="sse.not_configured", message="SSE 未配置", status_code=503)


def get_sse_config(request: Request) -> SseConfig:
    return _get_installed_sse_config(request.app)


def sse_comment(message: str) -> bytes:
    safe = message.replace("\r", " ").replace("\n", " ")
    return f": {safe}\n\n".encode("utf-8")


def sse_event(*, event: str | None, data: str, event_id: str | None = None) -> bytes:
    lines: list[str] = []
    if event_id is not None:
        lines.append(f"id: {event_id}")
    if event is not None:
        lines.append(f"event: {event}")
    lines.append(f"data: {data}")
    return ("\n".join(lines) + "\n\n").encode("utf-8")


__all__ = [
    "SseConfig",
    "configure_sse",
    "get_sse_config",
    "install_sse_config",
    "sse_comment",
    "sse_event",
]
