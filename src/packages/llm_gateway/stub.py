from __future__ import annotations

import asyncio
from dataclasses import dataclass
import os
from typing import AsyncIterator

from packages.config import load_dotenv_if_enabled

from .contract import (
    LlmGatewayRequest,
    LlmGatewayStreamEvent,
    LlmStreamMessageDelta,
    LlmStreamRunCompleted,
)
from .gateway import LlmGateway

_STUB_ENABLED_ENV = "ARKLOOP_STUB_AGENT_ENABLED"
_STUB_DELTA_COUNT_ENV = "ARKLOOP_STUB_AGENT_DELTA_COUNT"
_STUB_DELTA_INTERVAL_SECONDS_ENV = "ARKLOOP_STUB_AGENT_DELTA_INTERVAL_SECONDS"

_DEFAULT_STUB_ENABLED = True
_DEFAULT_STUB_DELTA_COUNT = 3
_DEFAULT_STUB_DELTA_INTERVAL_SECONDS = 0.02


def _parse_bool(value: str) -> bool:
    cleaned = value.strip().casefold()
    if cleaned in {"1", "true", "yes", "y", "on"}:
        return True
    if cleaned in {"0", "false", "no", "n", "off"}:
        return False
    raise ValueError("必须为布尔值（0/1、true/false）")


def _parse_positive_int(value: str) -> int:
    cleaned = value.strip()
    parsed = int(cleaned)
    if parsed <= 0:
        raise ValueError("必须为正整数")
    return parsed


def _parse_non_negative_float(value: str) -> float:
    cleaned = value.strip()
    parsed = float(cleaned)
    if parsed < 0:
        raise ValueError("必须为非负数")
    return parsed


@dataclass(frozen=True, slots=True)
class StubLlmGatewayConfig:
    enabled: bool = _DEFAULT_STUB_ENABLED
    delta_count: int = _DEFAULT_STUB_DELTA_COUNT
    delta_interval_seconds: float = _DEFAULT_STUB_DELTA_INTERVAL_SECONDS

    @classmethod
    def from_env(cls) -> "StubLlmGatewayConfig":
        load_dotenv_if_enabled(override=False)

        enabled = _DEFAULT_STUB_ENABLED
        raw_enabled = os.getenv(_STUB_ENABLED_ENV)
        if raw_enabled:
            enabled = _parse_bool(raw_enabled)

        delta_count = _DEFAULT_STUB_DELTA_COUNT
        raw_delta_count = os.getenv(_STUB_DELTA_COUNT_ENV)
        if raw_delta_count:
            delta_count = _parse_positive_int(raw_delta_count)

        delta_interval_seconds = _DEFAULT_STUB_DELTA_INTERVAL_SECONDS
        raw_delta_interval_seconds = os.getenv(_STUB_DELTA_INTERVAL_SECONDS_ENV)
        if raw_delta_interval_seconds:
            delta_interval_seconds = _parse_non_negative_float(raw_delta_interval_seconds)

        return cls(
            enabled=enabled,
            delta_count=delta_count,
            delta_interval_seconds=delta_interval_seconds,
        )


class StubLlmGateway(LlmGateway):
    def __init__(self, *, config: StubLlmGatewayConfig) -> None:
        self._config = config

    async def stream(self, *, request: LlmGatewayRequest) -> AsyncIterator[LlmGatewayStreamEvent]:
        _ = request
        for index in range(1, self._config.delta_count + 1):
            await asyncio.sleep(self._config.delta_interval_seconds)
            yield LlmStreamMessageDelta(content_delta=f"stub delta {index}", role="assistant")
        yield LlmStreamRunCompleted()


__all__ = ["StubLlmGateway", "StubLlmGatewayConfig"]
