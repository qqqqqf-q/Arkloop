from __future__ import annotations

import logging

import anyio
import pytest

from packages.mcp.registry import (
    load_mcp_tool_registration_from_env,
    load_mcp_tool_registration_from_env_async,
)


def test_mcp_registry_sync_loader_raises_inside_event_loop() -> None:
    async def _run() -> None:
        with pytest.raises(RuntimeError) as excinfo:
            load_mcp_tool_registration_from_env()
        assert "load_mcp_tool_registration_from_env_async" in str(excinfo.value)

    anyio.run(_run)


def test_mcp_registry_async_loader_logs_config_error(monkeypatch: pytest.MonkeyPatch, caplog: pytest.LogCaptureFixture) -> None:
    monkeypatch.setenv("ARKLOOP_MCP_CONFIG_FILE", "/tmp/arkloop_mcp_missing.json")

    async def _run() -> None:
        caplog.set_level(logging.WARNING, logger="arkloop.mcp")
        registration = await load_mcp_tool_registration_from_env_async()
        assert registration.agent_specs == ()
        assert "读取 MCP 配置失败" in caplog.text

    anyio.run(_run)

