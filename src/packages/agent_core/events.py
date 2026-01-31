from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any, Callable, Mapping
import uuid

JsonObject = dict[str, Any]
Clock = Callable[[], datetime]
EventIdFactory = Callable[[], uuid.UUID]


def utc_now() -> datetime:
    return datetime.now(timezone.utc)


@dataclass(frozen=True, slots=True)
class RunEvent:
    event_id: uuid.UUID
    run_id: uuid.UUID
    seq: int
    ts: datetime
    type: str
    data_json: JsonObject
    tool_name: str | None = None
    error_class: str | None = None


class RunEventEmitter:
    def __init__(
        self,
        *,
        run_id: uuid.UUID,
        trace_id: str | None = None,
        start_seq: int = 1,
        event_id_factory: EventIdFactory = uuid.uuid4,
        clock: Clock = utc_now,
    ) -> None:
        if start_seq <= 0:
            raise ValueError("start_seq 必须为正整数")
        self._run_id = run_id
        self._trace_id = trace_id
        self._next_seq = start_seq
        self._event_id_factory = event_id_factory
        self._clock = clock

    def emit(
        self,
        *,
        type: str,
        data_json: Mapping[str, Any] | None = None,
        tool_name: str | None = None,
        error_class: str | None = None,
    ) -> RunEvent:
        payload: JsonObject = {} if data_json is None else dict(data_json)
        if self._trace_id is not None:
            payload["trace_id"] = self._trace_id
        event = RunEvent(
            event_id=self._event_id_factory(),
            run_id=self._run_id,
            seq=self._next_seq,
            ts=self._clock(),
            type=type,
            data_json=payload,
            tool_name=tool_name,
            error_class=error_class,
        )
        self._next_seq += 1
        return event


__all__ = ["Clock", "EventIdFactory", "JsonObject", "RunEvent", "RunEventEmitter", "utc_now"]

