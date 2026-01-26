from __future__ import annotations

import logging

from fastapi.testclient import TestClient

from services.api.error_envelope import ApiError
from services.api.main import configure_app
from services.api.trace import TRACE_ID_HEADER


def _assert_has_trace_id(response) -> str:
    assert TRACE_ID_HEADER in response.headers
    trace_id = response.headers[TRACE_ID_HEADER]
    assert trace_id

    payload = response.json()
    assert payload["trace_id"] == trace_id
    return trace_id


def test_known_error_envelope_has_trace_id_header_and_body() -> None:
    app = configure_app()

    @app.get("/__test__/known-error")
    async def _known_error() -> None:
        raise ApiError(
            code="known_error",
            message="已知错误",
            status_code=400,
            details={"reason": "test"},
        )

    client = TestClient(app)
    response = client.get("/__test__/known-error", headers={TRACE_ID_HEADER: "client-trace"})
    assert response.status_code == 400

    trace_id = _assert_has_trace_id(response)
    assert trace_id != "client-trace"

    payload = response.json()
    assert payload["code"] == "known_error"
    assert payload["message"] == "已知错误"
    assert payload["details"] == {"reason": "test"}


def test_unhandled_error_envelope_has_trace_id_header_and_body() -> None:
    app = configure_app()

    @app.get("/__test__/crash")
    async def _crash() -> None:
        1 / 0

    client = TestClient(app)
    response = client.get("/__test__/crash")
    assert response.status_code == 500

    _assert_has_trace_id(response)

    payload = response.json()
    assert payload["code"] == "internal_error"
    assert payload["message"] == "内部错误"
    assert "details" not in payload


def test_trace_id_is_bound_to_log_records_within_same_request(caplog) -> None:
    app = configure_app()
    logger = logging.getLogger("tests.trace_id")

    @app.get("/__test__/log-and-known-error")
    async def _log_and_known_error() -> None:
        logger.info("测试日志")
        raise ApiError(code="known_error", message="已知错误", status_code=400)

    caplog.set_level(logging.INFO, logger="tests.trace_id")

    client = TestClient(app)
    response = client.get("/__test__/log-and-known-error")
    assert response.status_code == 400

    trace_id = _assert_has_trace_id(response)

    records = [r for r in caplog.records if r.name == "tests.trace_id" and r.getMessage() == "测试日志"]
    assert records
    assert all(getattr(r, "trace_id", None) == trace_id for r in records)

