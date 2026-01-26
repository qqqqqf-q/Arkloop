from __future__ import annotations

import json
import logging

from packages.observability.context import trace_id_context
from packages.observability.logging import configure_json_logging


def test_json_logging_redacts_authorization_by_default(capsys) -> None:
    configure_json_logging(component="api")
    capsys.readouterr()

    logger = logging.getLogger("tests.json_logging")
    secret = "Bearer should-not-appear"

    with trace_id_context("0" * 32):
        logger.info("脱敏测试", extra={"Authorization": secret, "foo": "bar"})

    output = capsys.readouterr().out.strip().splitlines()
    assert output

    payload = json.loads(output[-1])
    assert payload["ts"]
    assert payload["level"]
    assert payload["msg"] == "脱敏测试"
    assert payload["logger"] == "tests.json_logging"
    assert payload["component"] == "api"
    assert payload["trace_id"] == "0" * 32
    assert payload["foo"] == "bar"
    assert "Authorization" not in payload
    assert secret not in output[-1]

