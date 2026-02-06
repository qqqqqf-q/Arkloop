from __future__ import annotations

from dataclasses import dataclass, field
import time
from typing import Any, Mapping, Protocol
import uuid

from .events import RunEvent, RunEventEmitter
from .tools import POLICY_DENIED_CODE, ToolPolicyEnforcer, ToolRegistry

ERROR_CLASS_TOOL_NOT_REGISTERED = "tool.not_registered"
ERROR_CLASS_TOOL_EXECUTION_FAILED = "tool.execution_failed"


@dataclass(frozen=True, slots=True)
class ToolExecutionContext:
    run_id: uuid.UUID
    trace_id: str | None = None
    org_id: uuid.UUID | None = None
    timeout_ms: int | None = None
    budget: Mapping[str, Any] = field(default_factory=dict)
    emitter: RunEventEmitter | None = None


@dataclass(frozen=True, slots=True)
class ToolExecutionError:
    error_class: str
    message: str
    details: Mapping[str, Any] = field(default_factory=dict)

    def to_json(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "error_class": self.error_class,
            "message": self.message,
        }
        if self.details:
            payload["details"] = dict(self.details)
        return payload


@dataclass(frozen=True, slots=True)
class ToolExecutionResult:
    result_json: Mapping[str, Any] | None = None
    error: ToolExecutionError | None = None
    duration_ms: int = 0
    usage: Mapping[str, Any] | None = None
    events: tuple[RunEvent, ...] = ()


class ToolExecutor(Protocol):
    async def execute(
        self,
        *,
        tool_name: str,
        args: dict[str, Any],
        context: ToolExecutionContext,
    ) -> ToolExecutionResult: ...


class DispatchingToolExecutor(ToolExecutor):
    def __init__(
        self,
        *,
        registry: ToolRegistry,
        policy_enforcer: ToolPolicyEnforcer,
        executors: Mapping[str, ToolExecutor] | None = None,
    ) -> None:
        self._registry = registry
        self._policy_enforcer = policy_enforcer
        self._executor_by_tool_name: dict[str, ToolExecutor] = {}
        if executors:
            for tool_name, executor in executors.items():
                self.bind(tool_name=tool_name, executor=executor)

    def bind(self, *, tool_name: str, executor: ToolExecutor) -> None:
        if self._registry.get(tool_name) is None:
            raise ValueError(f"工具未注册：{tool_name}")
        self._executor_by_tool_name[tool_name] = executor

    async def execute(
        self,
        *,
        tool_name: str,
        args: dict[str, Any],
        context: ToolExecutionContext,
    ) -> ToolExecutionResult:
        started = time.monotonic()
        emitter = context.emitter or RunEventEmitter(
            run_id=context.run_id,
            trace_id=context.trace_id,
        )
        decision = self._policy_enforcer.request_tool_call(
            emitter=emitter,
            tool_name=tool_name,
            args_json=args,
        )
        policy_events = tuple(decision.events)

        if not decision.allowed:
            denied_event = policy_events[-1]
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=POLICY_DENIED_CODE,
                    message="工具调用被策略拒绝",
                    details={
                        "tool_name": tool_name,
                        "tool_call_id": str(decision.tool_call_id),
                        "deny_reason": denied_event.data_json.get("deny_reason"),
                    },
                ),
                duration_ms=_duration_ms(started),
                events=policy_events,
            )

        executor = self._executor_by_tool_name.get(tool_name)
        if executor is None:
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=ERROR_CLASS_TOOL_NOT_REGISTERED,
                    message="工具未绑定执行器",
                    details={
                        "tool_name": tool_name,
                        "tool_call_id": str(decision.tool_call_id),
                    },
                ),
                duration_ms=_duration_ms(started),
                events=policy_events,
            )

        try:
            result = await executor.execute(tool_name=tool_name, args=args, context=context)
        except Exception as exc:
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=ERROR_CLASS_TOOL_EXECUTION_FAILED,
                    message="工具执行失败",
                    details={
                        "tool_name": tool_name,
                        "tool_call_id": str(decision.tool_call_id),
                        "exception_type": type(exc).__name__,
                    },
                ),
                duration_ms=_duration_ms(started),
                events=policy_events,
            )

        merged_events = policy_events + tuple(result.events)
        return ToolExecutionResult(
            result_json=dict(result.result_json) if result.result_json is not None else None,
            error=result.error,
            duration_ms=_duration_ms(started),
            usage=dict(result.usage) if result.usage is not None else None,
            events=merged_events,
        )


def _duration_ms(started: float) -> int:
    elapsed = time.monotonic() - started
    millis = int(elapsed * 1000)
    return millis if millis >= 0 else 0


__all__ = [
    "DispatchingToolExecutor",
    "ERROR_CLASS_TOOL_EXECUTION_FAILED",
    "ERROR_CLASS_TOOL_NOT_REGISTERED",
    "ToolExecutionContext",
    "ToolExecutionError",
    "ToolExecutionResult",
    "ToolExecutor",
]
