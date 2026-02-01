from __future__ import annotations

import uuid

from packages.agent_core import AgentRunContext, AgentRunner
from packages.data import Database
from packages.data.runs import RunNotFoundError, SqlAlchemyRunEventRepository


class RunEngine:
    def __init__(self, *, database: Database, runner: AgentRunner) -> None:
        self._database = database
        self._runner = runner

    async def execute(self, *, run_id: uuid.UUID, trace_id: str) -> None:
        context = AgentRunContext(run_id=run_id, trace_id=trace_id)
        async with self._database.sessionmaker() as session:
            repo = SqlAlchemyRunEventRepository(session)
            run = await repo.get_run(run_id=run_id)
            if run is None:
                raise RunNotFoundError(run_id=run_id)

            async for event in self._runner.run(context=context):
                await repo.append_event(
                    run_id=run_id,
                    type=event.type,
                    data_json=event.data_json,
                    tool_name=event.tool_name,
                    error_class=event.error_class,
                )
            await session.commit()


__all__ = ["RunEngine"]
