from __future__ import annotations

from dataclasses import dataclass
import time
from typing import Any, Callable, Mapping

from .executor import ToolExecutionContext, ToolExecutionResult, _duration_ms

StubResultFactory = Callable[[str, dict[str, Any], ToolExecutionContext], Mapping[str, Any]]


@dataclass(slots=True)
class StubToolExecutor:
    result_factory: StubResultFactory | None = None

    async def execute(
        self,
        *,
        tool_name: str,
        args: dict[str, Any],
        context: ToolExecutionContext,
    ) -> ToolExecutionResult:
        started = time.monotonic()
        payload_factory = self.result_factory or _default_result_factory
        payload = dict(payload_factory(tool_name, args, context))
        return ToolExecutionResult(result_json=payload, duration_ms=_duration_ms(started))


def _default_result_factory(
    tool_name: str,
    args: dict[str, Any],
    context: ToolExecutionContext,
) -> Mapping[str, Any]:
    return {
        "stub": True,
        "tool_name": tool_name,
        "echo_arguments": dict(args),
        "trace_id": context.trace_id,
    }

__all__ = ["StubToolExecutor", "StubResultFactory"]
