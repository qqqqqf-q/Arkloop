from __future__ import annotations

import time
from typing import Any

from packages.agent_core.executor import (
    ToolExecutionContext,
    ToolExecutionError,
    ToolExecutionResult,
)
from packages.agent_core.tools import ToolSpec as AgentToolSpec
from packages.llm_gateway import ToolSpec as LlmToolSpec

_ERROR_CLASS_ARGS_INVALID = "tool.args_invalid"

NOOP_AGENT_TOOL_SPEC = AgentToolSpec(
    name="noop",
    version="1",
    description="无副作用的空操作",
    risk_level="low",
)

NOOP_LLM_TOOL_SPEC = LlmToolSpec(
    name="noop",
    description="无副作用的空操作",
    json_schema={
        "type": "object",
        "properties": {},
        "additionalProperties": False,
    },
)


class NoopToolExecutor:
    async def execute(
        self,
        *,
        tool_name: str,
        args: dict[str, Any],
        context: ToolExecutionContext,
        tool_call_id: str | None = None,
    ) -> ToolExecutionResult:
        _ = (tool_name, context, tool_call_id)
        started = time.monotonic()
        if args:
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=_ERROR_CLASS_ARGS_INVALID,
                    message="noop 不接受参数",
                    details={"unexpected_fields": sorted(args.keys())},
                ),
                duration_ms=_duration_ms(started),
            )
        return ToolExecutionResult(result_json={"ok": True}, duration_ms=_duration_ms(started))


def _duration_ms(started: float) -> int:
    elapsed = time.monotonic() - started
    millis = int(elapsed * 1000)
    return millis if millis >= 0 else 0


__all__ = ["NOOP_AGENT_TOOL_SPEC", "NOOP_LLM_TOOL_SPEC", "NoopToolExecutor"]

