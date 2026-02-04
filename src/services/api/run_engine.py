from __future__ import annotations

import time
import uuid

from packages.agent_core import AgentRunContext, AgentRunner
from packages.data import Database
from packages.data.runs import RunNotFoundError, SqlAlchemyRunEventRepository
from packages.data.threads import SqlAlchemyMessageRepository

_EVENT_COMMIT_BATCH_SIZE = 20
_EVENT_COMMIT_MAX_INTERVAL_SECONDS = 0.2


class RunEngine:
    def __init__(self, *, database: Database, runner: AgentRunner) -> None:
        self._database = database
        self._runner = runner

    async def execute(self, *, run_id: uuid.UUID, trace_id: str) -> None:
        async with self._database.sessionmaker() as session:
            repo = SqlAlchemyRunEventRepository(session)
            run = await repo.get_run(run_id=run_id)
            if run is None:
                raise RunNotFoundError(run_id=run_id)

            started_event = await repo.list_events(run_id=run_id, after_seq=0, limit=1)
            input_json: dict[str, object] = {
                "org_id": str(run.org_id),
                "thread_id": str(run.thread_id),
            }
            if started_event:
                data_json = started_event[0].data_json
                if isinstance(data_json, dict):
                    route_id = data_json.get("route_id")
                    if isinstance(route_id, str) and route_id.strip():
                        input_json["route_id"] = route_id.strip()

            context = AgentRunContext(run_id=run_id, trace_id=trace_id, input_json=input_json)
            assistant_deltas: list[str] = []
            completed = False
            pending_events_since_commit = 0
            last_commit_at = time.monotonic()
            async for event in self._runner.run(context=context):
                await repo.append_event(
                    run_id=run_id,
                    ts=event.ts,
                    type=event.type,
                    data_json=event.data_json,
                    tool_name=event.tool_name,
                    error_class=event.error_class,
                )
                pending_events_since_commit += 1
                if event.type == "message.delta":
                    delta = _extract_assistant_delta(event.data_json)
                    if delta is not None:
                        assistant_deltas.append(delta)
                elif event.type == "run.completed":
                    completed = True
                    continue

                if event.type != "message.delta":
                    await session.commit()
                    pending_events_since_commit = 0
                    last_commit_at = time.monotonic()
                    continue

                now = time.monotonic()
                if (
                    pending_events_since_commit >= _EVENT_COMMIT_BATCH_SIZE
                    or (now - last_commit_at) >= _EVENT_COMMIT_MAX_INTERVAL_SECONDS
                ):
                    await session.commit()
                    pending_events_since_commit = 0
                    last_commit_at = now

            if completed:
                content = "".join(assistant_deltas)
                if content:
                    message_repo = SqlAlchemyMessageRepository(session)
                    await message_repo.create(
                        org_id=run.org_id,
                        thread_id=run.thread_id,
                        role="assistant",
                        content=content,
                    )
            await session.commit()


def _extract_assistant_delta(data_json: object) -> str | None:
    if not isinstance(data_json, dict):
        return None
    role = data_json.get("role")
    if role is not None and role != "assistant":
        return None
    delta = data_json.get("content_delta")
    return delta if isinstance(delta, str) and delta else None


__all__ = ["RunEngine"]
