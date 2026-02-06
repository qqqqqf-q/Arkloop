from __future__ import annotations

from datetime import datetime, timezone
from typing import Any, Mapping
import uuid

import anyio

from packages.agent_core import (
    DENY_REASON_TOOL_NOT_IN_ALLOWLIST,
    ERROR_CLASS_TOOL_EXECUTION_FAILED,
    ERROR_CLASS_TOOL_NOT_REGISTERED,
    POLICY_DENIED_CODE,
    DispatchingToolExecutor,
    RunEventEmitter,
    ToolAllowlist,
    ToolExecutionContext,
    ToolExecutionResult,
    ToolPolicyEnforcer,
    ToolRegistry,
    ToolSpec,
)


class _FakeEventIdFactory:
    def __init__(self) -> None:
        self._next_int = 1

    def __call__(self) -> uuid.UUID:
        value = uuid.UUID(int=self._next_int)
        self._next_int += 1
        return value


def _fixed_clock() -> datetime:
    return datetime(2025, 1, 1, 0, 0, 0, tzinfo=timezone.utc)


class _RecordingToolExecutor:
    def __init__(
        self,
        *,
        result_json: Mapping[str, Any] | None = None,
        usage: Mapping[str, Any] | None = None,
        error: Exception | None = None,
    ) -> None:
        self.calls: list[tuple[str, dict[str, Any]]] = []
        self._result_json = dict(result_json or {"ok": True})
        self._usage = dict(usage or {"tool_calls": 1})
        self._error = error

    async def execute(
        self,
        *,
        tool_name: str,
        args: dict[str, Any],
        context: ToolExecutionContext,
    ) -> ToolExecutionResult:
        _ = context
        self.calls.append((tool_name, dict(args)))
        if self._error is not None:
            raise self._error
        return ToolExecutionResult(
            result_json=self._result_json,
            usage=self._usage,
            duration_ms=1,
        )


def _build_context(trace_id: str = "a" * 32) -> ToolExecutionContext:
    run_id = uuid.uuid4()
    return ToolExecutionContext(
        run_id=run_id,
        trace_id=trace_id,
        org_id=uuid.uuid4(),
        timeout_ms=5_000,
        budget={"max_tokens": 1_000},
        emitter=RunEventEmitter(
            run_id=run_id,
            trace_id=trace_id,
            event_id_factory=_FakeEventIdFactory(),
            clock=_fixed_clock,
        ),
    )


def test_dispatching_tool_executor_dispatches_to_bound_executor() -> None:
    registry = ToolRegistry(
        specs=[ToolSpec(name="echo", version="1", description="回显", risk_level="low")]
    )
    allowlist = ToolAllowlist.from_names(["echo"])
    enforcer = ToolPolicyEnforcer(registry=registry, allowlist=allowlist)
    bound_executor = _RecordingToolExecutor(result_json={"message": "ok"})
    executor = DispatchingToolExecutor(
        registry=registry,
        policy_enforcer=enforcer,
        executors={"echo": bound_executor},
    )
    context = _build_context()

    async def _run() -> ToolExecutionResult:
        return await executor.execute(tool_name="echo", args={"q": "hi"}, context=context)

    result = anyio.run(_run)

    assert result.error is None
    assert result.result_json == {"message": "ok"}
    assert result.usage == {"tool_calls": 1}
    assert result.duration_ms >= 0
    assert bound_executor.calls == [("echo", {"q": "hi"})]
    assert [event.type for event in result.events] == ["tool.call"]


def test_dispatching_tool_executor_returns_clear_errors_for_unregistered_and_unbound_tools() -> None:
    registry = ToolRegistry(
        specs=[ToolSpec(name="echo", version="1", description="回显", risk_level="low")]
    )
    allowlist = ToolAllowlist.from_names(["echo", "missing"])
    enforcer = ToolPolicyEnforcer(registry=registry, allowlist=allowlist)
    executor = DispatchingToolExecutor(registry=registry, policy_enforcer=enforcer)
    context = _build_context()

    async def _run_unknown() -> ToolExecutionResult:
        return await executor.execute(tool_name="missing", args={"a": 1}, context=context)

    unknown_result = anyio.run(_run_unknown)
    assert unknown_result.error is not None
    assert unknown_result.error.error_class == POLICY_DENIED_CODE
    assert [event.type for event in unknown_result.events] == ["tool.call", "policy.denied"]

    async def _run_unbound() -> ToolExecutionResult:
        return await executor.execute(tool_name="echo", args={"a": 2}, context=context)

    unbound_result = anyio.run(_run_unbound)
    assert unbound_result.error is not None
    assert unbound_result.error.error_class == ERROR_CLASS_TOOL_NOT_REGISTERED
    assert [event.type for event in unbound_result.events] == ["tool.call"]
    assert unbound_result.error.details["tool_name"] == "echo"


def test_dispatching_tool_executor_blocked_by_policy_does_not_call_bound_executor() -> None:
    registry = ToolRegistry(
        specs=[ToolSpec(name="shell", version="1", description="执行命令", risk_level="high")]
    )
    allowlist = ToolAllowlist.from_names([])
    enforcer = ToolPolicyEnforcer(registry=registry, allowlist=allowlist)
    bound_executor = _RecordingToolExecutor()
    executor = DispatchingToolExecutor(
        registry=registry,
        policy_enforcer=enforcer,
        executors={"shell": bound_executor},
    )
    context = _build_context()

    async def _run() -> ToolExecutionResult:
        return await executor.execute(tool_name="shell", args={"cmd": "echo hi"}, context=context)

    result = anyio.run(_run)

    assert result.error is not None
    assert result.error.error_class == POLICY_DENIED_CODE
    assert result.error.details["deny_reason"] == DENY_REASON_TOOL_NOT_IN_ALLOWLIST
    assert result.duration_ms >= 0
    assert bound_executor.calls == []
    assert [event.type for event in result.events] == ["tool.call", "policy.denied"]


def test_dispatching_tool_executor_converts_executor_exception_to_stable_error() -> None:
    registry = ToolRegistry(
        specs=[ToolSpec(name="echo", version="1", description="回显", risk_level="low")]
    )
    allowlist = ToolAllowlist.from_names(["echo"])
    enforcer = ToolPolicyEnforcer(registry=registry, allowlist=allowlist)
    bound_executor = _RecordingToolExecutor(error=RuntimeError("secret text"))
    executor = DispatchingToolExecutor(
        registry=registry,
        policy_enforcer=enforcer,
        executors={"echo": bound_executor},
    )
    context = _build_context()

    async def _run() -> ToolExecutionResult:
        return await executor.execute(tool_name="echo", args={"k": "v"}, context=context)

    result = anyio.run(_run)

    assert result.error is not None
    assert result.error.error_class == ERROR_CLASS_TOOL_EXECUTION_FAILED
    assert result.error.details["exception_type"] == "RuntimeError"
    assert "secret text" not in str(result.error.details)
    assert result.duration_ms >= 0
    assert [event.type for event in result.events] == ["tool.call"]
