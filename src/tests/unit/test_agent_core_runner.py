from __future__ import annotations

from datetime import datetime, timezone
import uuid

import anyio

from packages.agent_core import AgentRunContext, AgentRunner, RunEventEmitter


class _FakeEventIdFactory:
    def __init__(self) -> None:
        self._next_int = 1

    def __call__(self) -> uuid.UUID:
        value = uuid.UUID(int=self._next_int)
        self._next_int += 1
        return value


def _fixed_clock() -> datetime:
    return datetime(2025, 1, 1, 0, 0, 0, tzinfo=timezone.utc)


class _StubRunner:
    def __init__(self) -> None:
        self._event_id_factory = _FakeEventIdFactory()

    async def run(self, *, context: AgentRunContext):
        emitter = RunEventEmitter(
            run_id=context.run_id,
            trace_id=context.trace_id,
            event_id_factory=self._event_id_factory,
            clock=_fixed_clock,
        )
        yield emitter.emit(type="run.started", data_json={"input_json": dict(context.input_json)})
        yield emitter.emit(
            type="message.delta",
            data_json={"content_delta": "chunk", "role": "assistant"},
        )
        yield emitter.emit(type="run.completed", data_json={})


def test_agent_runner_emits_standardized_event_sequence() -> None:
    run_id = uuid.UUID(int=42)
    context = AgentRunContext(run_id=run_id, trace_id="a" * 32, input_json={"prompt": "hi"})

    runner: AgentRunner = _StubRunner()

    async def _collect():
        events = []
        async for event in runner.run(context=context):
            events.append(event)
        return events

    events = anyio.run(_collect)

    assert [event.seq for event in events] == [1, 2, 3]
    assert [event.type for event in events] == ["run.started", "message.delta", "run.completed"]
    assert all(event.run_id == run_id for event in events)
    assert all(event.ts == _fixed_clock() for event in events)

    ids = [event.event_id for event in events]
    assert all(isinstance(value, uuid.UUID) for value in ids)
    assert len(set(ids)) == len(ids)

    assert all(event.data_json.get("trace_id") == context.trace_id for event in events)
    assert events[1].data_json.get("role") == "assistant"
    assert isinstance(events[1].data_json.get("content_delta"), str)
    assert events[1].data_json.get("content_delta")


def test_run_event_emitter_overrides_untrusted_trace_id() -> None:
    emitter = RunEventEmitter(
        run_id=uuid.UUID(int=1),
        trace_id="b" * 32,
        event_id_factory=_FakeEventIdFactory(),
        clock=_fixed_clock,
    )
    event = emitter.emit(type="run.started", data_json={"trace_id": "client", "k": "v"})
    assert event.data_json["trace_id"] == "b" * 32
    assert event.data_json["k"] == "v"

