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

ECHO_AGENT_TOOL_SPEC = AgentToolSpec(
    name="echo",
    version="1",
    description="回显输入文本",
    risk_level="low",
)

ECHO_LLM_TOOL_SPEC = LlmToolSpec(
    name="echo",
    description="回显输入文本",
    json_schema={
        "type": "object",
        "properties": {"text": {"type": "string", "minLength": 1}},
        "required": ["text"],
        "additionalProperties": False,
    },
)


class EchoToolExecutor:
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
        text, error = _parse_echo_args(args)
        if error is not None:
            return ToolExecutionResult(error=error, duration_ms=_duration_ms(started))
        return ToolExecutionResult(result_json={"text": text}, duration_ms=_duration_ms(started))


def _parse_echo_args(args: dict[str, Any]) -> tuple[str | None, ToolExecutionError | None]:
    unknown = [key for key in args.keys() if key != "text"]
    if unknown:
        return None, ToolExecutionError(
            error_class=_ERROR_CLASS_ARGS_INVALID,
            message="工具参数不支持额外字段",
            details={"unknown_fields": sorted(unknown)},
        )

    text = args.get("text")
    if not isinstance(text, str) or not text.strip():
        return None, ToolExecutionError(
            error_class=_ERROR_CLASS_ARGS_INVALID,
            message="参数 text 必须为非空字符串",
            details={"field": "text"},
        )
    return text, None


def _duration_ms(started: float) -> int:
    elapsed = time.monotonic() - started
    millis = int(elapsed * 1000)
    return millis if millis >= 0 else 0


__all__ = ["ECHO_AGENT_TOOL_SPEC", "ECHO_LLM_TOOL_SPEC", "EchoToolExecutor"]

