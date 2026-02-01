from __future__ import annotations

from typing import AsyncIterator, Protocol

from .contract import LlmGatewayRequest, LlmGatewayStreamEvent


class LlmGateway(Protocol):
    async def stream(
        self, *, request: LlmGatewayRequest
    ) -> AsyncIterator[LlmGatewayStreamEvent]: ...


__all__ = ["LlmGateway"]
