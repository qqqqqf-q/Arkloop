from __future__ import annotations

import uuid

from packages.job_queue import RUN_EXECUTE_JOB_TYPE, RUN_EXECUTE_QUEUE_JOB_TYPE_GO_BRIDGE
from services.api import run_executor


def test_select_queue_job_type_defaults_to_python(monkeypatch) -> None:
    with monkeypatch.context() as m:
        m.delenv("ARKLOOP_WORKER_GO_TRAFFIC_PERCENT", raising=False)
        assert run_executor._select_queue_job_type(run_id=uuid.uuid4()) == RUN_EXECUTE_JOB_TYPE  # type: ignore[attr-defined]


def test_select_queue_job_type_all_go_when_100_percent(monkeypatch) -> None:
    with monkeypatch.context() as m:
        m.setenv("ARKLOOP_WORKER_GO_TRAFFIC_PERCENT", "100")
        assert (
            run_executor._select_queue_job_type(run_id=uuid.uuid4())  # type: ignore[attr-defined]
            == RUN_EXECUTE_QUEUE_JOB_TYPE_GO_BRIDGE
        )


def test_select_queue_job_type_uses_deterministic_bucket(monkeypatch) -> None:
    run_id_bucket_1 = uuid.UUID(int=1)
    run_id_bucket_99 = uuid.UUID(int=99)

    with monkeypatch.context() as m:
        m.setenv("ARKLOOP_WORKER_GO_TRAFFIC_PERCENT", "50")
        assert (
            run_executor._select_queue_job_type(run_id=run_id_bucket_1)  # type: ignore[attr-defined]
            == RUN_EXECUTE_QUEUE_JOB_TYPE_GO_BRIDGE
        )
        assert (
            run_executor._select_queue_job_type(run_id=run_id_bucket_99)  # type: ignore[attr-defined]
            == RUN_EXECUTE_JOB_TYPE
        )

