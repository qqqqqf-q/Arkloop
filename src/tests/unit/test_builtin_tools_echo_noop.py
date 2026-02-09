from __future__ import annotations

import uuid

import anyio

from packages.agent_core.builtin_tools.echo import EchoToolExecutor
from packages.agent_core.builtin_tools.noop import NoopToolExecutor
from packages.agent_core.executor import ToolExecutionContext


def _context() -> ToolExecutionContext:
    return ToolExecutionContext(run_id=uuid.uuid4(), trace_id="t" * 32)


def test_echo_tool_returns_text() -> None:
    executor = EchoToolExecutor()

    async def _run():
        return await executor.execute(
            tool_name="echo",
            args={"text": "hello"},
            context=_context(),
        )

    result = anyio.run(_run)
    assert result.error is None
    assert result.result_json == {"text": "hello"}

def test_echo_tool_rejects_blank_text() -> None:
    executor = EchoToolExecutor()

    async def _run():
        return await executor.execute(
            tool_name="echo",
            args={"text": "   "},
            context=_context(),
        )

    result = anyio.run(_run)
    assert result.error is not None
    assert result.error.error_class == "tool.args_invalid"
    assert result.error.details["field"] == "text"


def test_echo_tool_rejects_unknown_fields() -> None:
    executor = EchoToolExecutor()

    async def _run():
        return await executor.execute(
            tool_name="echo",
            args={"text": "hi", "extra": 1},
            context=_context(),
        )

    result = anyio.run(_run)
    assert result.error is not None
    assert result.error.error_class == "tool.args_invalid"
    assert result.error.details["unknown_fields"] == ["extra"]


def test_noop_tool_returns_ok() -> None:
    executor = NoopToolExecutor()

    async def _run():
        return await executor.execute(
            tool_name="noop",
            args={},
            context=_context(),
        )

    result = anyio.run(_run)
    assert result.error is None
    assert result.result_json == {"ok": True}


def test_noop_tool_rejects_any_arguments() -> None:
    executor = NoopToolExecutor()

    async def _run():
        return await executor.execute(
            tool_name="noop",
            args={"x": 1},
            context=_context(),
        )

    result = anyio.run(_run)
    assert result.error is not None
    assert result.error.error_class == "tool.args_invalid"
    assert result.error.details["unexpected_fields"] == ["x"]
