from __future__ import annotations

import json
from pathlib import Path
import sys
import uuid

import anyio
import pytest

from packages.agent_core.executor import ToolExecutionContext
from packages.data import DatabaseConfig
from packages.mcp.pool import close_default_mcp_stdio_client_pool
from services.worker.composition import create_container
from services.worker.consumer_loop import WorkerLoopConfig

pytestmark = pytest.mark.integration


def _write_mcp_server(*, path: Path) -> None:
    code = r"""
import json
import sys

def send(payload):
    sys.stdout.write(json.dumps(payload, ensure_ascii=False, separators=(",", ":")) + "\n")
    sys.stdout.flush()

def handle(request):
    method = request.get("method")
    request_id = request.get("id")
    params = request.get("params") or {}

    if method == "initialize":
        protocol_version = params.get("protocolVersion") or "2024-11-05"
        send(
            {
                "jsonrpc": "2.0",
                "id": request_id,
                "result": {
                    "protocolVersion": protocol_version,
                    "capabilities": {"tools": {}},
                    "serverInfo": {"name": "test-mcp-server", "version": "0"},
                },
            }
        )
        return

    if method == "notifications/initialized":
        return

    if method == "tools/list":
        tools = [
            {
                "name": "echo",
                "title": "Echo",
                "description": "echo tool",
                "inputSchema": {
                    "type": "object",
                    "properties": {"text": {"type": "string"}},
                    "required": ["text"],
                    "additionalProperties": False,
                },
            }
        ]
        send({"jsonrpc": "2.0", "id": request_id, "result": {"tools": tools}})
        return

    if method == "tools/call":
        name = params.get("name")
        arguments = params.get("arguments") or {}
        if name != "echo":
            send({"jsonrpc": "2.0", "id": request_id, "error": {"code": -32601, "message": "unknown tool"}})
            return
        send(
            {
                "jsonrpc": "2.0",
                "id": request_id,
                "result": {
                    "content": [{"type": "text", "text": str(arguments.get("text", ""))}],
                    "isError": False,
                },
            }
        )
        return

    send({"jsonrpc": "2.0", "id": request_id, "error": {"code": -32601, "message": "unknown method"}})

for line in sys.stdin:
    raw = line.strip()
    if not raw:
        continue
    try:
        request = json.loads(raw)
    except json.JSONDecodeError:
        continue
    if not isinstance(request, dict):
        continue
    handle(request)
"""
    path.write_text(code.strip() + "\n", encoding="utf-8")


def _write_mcp_config(*, path: Path, script: Path) -> None:
    payload = {
        "mcpServers": {
            "test": {
                "command": sys.executable,
                "args": ["-u", str(script)],
                "callTimeoutMs": 1000,
            }
        }
    }
    path.write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")


def test_worker_startup_loads_mcp_tools_via_async_registry(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    server_script = tmp_path / "mcp_server.py"
    config_file = tmp_path / "mcp_config.json"
    _write_mcp_server(path=server_script)
    _write_mcp_config(path=config_file, script=server_script)

    with monkeypatch.context() as m:
        m.setenv("ARKLOOP_MCP_CONFIG_FILE", str(config_file))
        m.setenv("ARKLOOP_TOOL_ALLOWLIST", "mcp__test__echo")

        async def _run() -> None:
            database, loop = await create_container(
                database_config=config,
                loop_config=WorkerLoopConfig(concurrency=1, poll_seconds=0, heartbeat_seconds=0),
            )
            try:
                engine = getattr(loop._worker, "_engine", None)
                assert engine is not None
                runner = getattr(engine, "_runner", None)
                assert runner is not None
                tool_executor = getattr(runner, "_tool_executor", None)
                assert tool_executor is not None

                result = await tool_executor.execute(
                    tool_name="mcp__test__echo",
                    args={"text": "ping"},
                    context=ToolExecutionContext(run_id=uuid.uuid4(), trace_id="t" * 32),
                )
                assert result.error is None
                assert result.result_json is not None
                assert result.result_json["content"] == [{"type": "text", "text": "ping"}]
            finally:
                await close_default_mcp_stdio_client_pool()
                await database.dispose()

        anyio.run(_run)
