from __future__ import annotations

from dataclasses import dataclass, field
from typing import TYPE_CHECKING, Any, AsyncIterator, Callable, Mapping, Protocol
import uuid

from .events import RunEvent

if TYPE_CHECKING:
    from packages.llm_gateway import ToolSpec as LlmToolSpec


CancelSignal = Callable[[], bool]


@dataclass(frozen=True, slots=True)
class AgentRunContext:
    run_id: uuid.UUID
    trace_id: str | None = None
    input_json: Mapping[str, Any] = field(default_factory=dict)
    max_iterations: int = 10
    tool_specs: tuple["LlmToolSpec", ...] = field(default_factory=tuple)
    cancel_signal: CancelSignal | None = None


class AgentRunner(Protocol):
    async def run(self, *, context: AgentRunContext) -> AsyncIterator[RunEvent]: ...


__all__ = ["AgentRunContext", "AgentRunner", "CancelSignal"]

