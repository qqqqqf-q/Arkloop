from __future__ import annotations

import importlib
import sys

import fastapi

import packages.observability.logging as observability_logging


def test_import_services_api_main_has_no_startup_side_effects(monkeypatch) -> None:
    def _forbid_configure_json_logging(*_args, **_kwargs) -> None:
        raise AssertionError("不应在 import 阶段配置日志")

    def _forbid_fastapi_app(*_args, **_kwargs):
        raise AssertionError("不应在 import 阶段创建 FastAPI 实例")

    with monkeypatch.context() as m:
        m.setattr(
            observability_logging,
            "configure_json_logging",
            _forbid_configure_json_logging,
        )
        m.setattr(fastapi, "FastAPI", _forbid_fastapi_app)

        sys.modules.pop("services.api.main", None)
        importlib.import_module("services.api.main")

    sys.modules.pop("services.api.main", None)
