from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, AsyncIterator, Mapping, Protocol
import uuid

from .events import RunEvent


@dataclass(frozen=True, slots=True)
class AgentRunContext:
    run_id: uuid.UUID
    trace_id: str | None = None
    input_json: Mapping[str, Any] = field(default_factory=dict)


class AgentRunner(Protocol):
    async def run(self, *, context: AgentRunContext) -> AsyncIterator[RunEvent]: ...


__all__ = ["AgentRunContext", "AgentRunner"]

