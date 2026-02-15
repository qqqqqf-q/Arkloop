from __future__ import annotations

from pathlib import Path
import sys
import uuid

import anyio

from packages.agent_core import (
    AgentRunContext,
    DispatchingToolExecutor,
    RunEventEmitter,
    ToolAllowlist,
    ToolPolicyEnforcer,
    ToolRegistry,
)
from packages.agent_core.loop import AgentLoop
from packages.llm_gateway import (
    LlmGatewayRequest,
    LlmMessage,
    LlmStreamMessageDelta,
    LlmStreamRunCompleted,
    LlmStreamToolCall,
    LlmTextPart,
    ToolSpec as LlmToolSpec,
)
from packages.mcp.config import McpConfig, McpServerConfig
from packages.mcp.pool import McpStdioClientPool
from packages.mcp.registry import discover_mcp_tools


def _write_mcp_server(*, path: Path) -> None:
    code = r"""
import json
import os
import sys
import time

MODE = os.getenv("MCP_TEST_MODE", "normal")

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
        if MODE == "sleep_on_call":
            time.sleep(0.2)
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


def _server_config(*, script: Path, mode: str = "normal", timeout_ms: int = 1000) -> McpServerConfig:
    return McpServerConfig(
        server_id="test",
        command=sys.executable,
        args=("-u", str(script)),
        env={"MCP_TEST_MODE": mode},
        call_timeout_ms=timeout_ms,
    )


class _ScriptedGateway:
    def __init__(self, *, turns: list[list[object]]) -> None:
        self._turns = turns
        self.requests: list[LlmGatewayRequest] = []

    async def stream(self, *, request: LlmGatewayRequest):
        self.requests.append(request)
        index = len(self.requests) - 1
        for item in self._turns[index] if index < len(self._turns) else []:
            yield item


def _collect_events(*, loop: AgentLoop, context: AgentRunContext, request: LlmGatewayRequest) -> list:
    emitter = RunEventEmitter(run_id=context.run_id, trace_id=context.trace_id)

    async def _collect():
        events = []
        async for event in loop.run(context=context, emitter=emitter, request=request):
            events.append(event)
        return events

    return anyio.run(_collect)


def test_mcp_stdio_integration_can_call_tool_via_agent_loop(tmp_path: Path) -> None:
    server_script = tmp_path / "mcp_server.py"
    _write_mcp_server(path=server_script)
    server = _server_config(script=server_script)

    async def _run():
        pool = McpStdioClientPool(ttl_seconds=None, max_clients_per_server=1)
        try:
            registration = await discover_mcp_tools(config=McpConfig(servers=(server,)), pool=pool)
            assert len(registration.agent_specs) == 1
            tool_name = registration.agent_specs[0].name

            registry = ToolRegistry(specs=registration.agent_specs)
            allowlist = ToolAllowlist.from_names([tool_name])
            dispatcher = DispatchingToolExecutor(
                registry=registry,
                policy_enforcer=ToolPolicyEnforcer(registry=registry, allowlist=allowlist),
                executors=registration.executors,
            )

            gateway = _ScriptedGateway(
                turns=[
                    [
                        LlmStreamToolCall(
                            tool_call_id="call_1",
                            tool_name=tool_name,
                            arguments_json={"text": "ping"},
                        ),
                        LlmStreamRunCompleted(),
                    ],
                    [
                        LlmStreamMessageDelta(content_delta="done", role="assistant"),
                        LlmStreamRunCompleted(),
                    ],
                ]
            )
            loop = AgentLoop(gateway=gateway, tool_executor=dispatcher)
            context = AgentRunContext(run_id=uuid.uuid4(), trace_id="t" * 32)

            request = LlmGatewayRequest(
                model="stub-model",
                messages=[LlmMessage(role="user", content=[LlmTextPart(text="hi")])],
                tools=[LlmToolSpec(name=tool_name, description="mcp echo", json_schema={"type": "object"})],
            )

            emitter = RunEventEmitter(run_id=context.run_id, trace_id=context.trace_id)
            events = []
            async for event in loop.run(context=context, emitter=emitter, request=request):
                events.append(event)
            return tool_name, events
        finally:
            await pool.close_all()

    tool_name, events = anyio.run(_run)

    assert [event.type for event in events] == ["tool.call", "tool.result", "message.delta", "run.completed"]
    tool_result = events[1].data_json
    assert tool_result["tool_name"] == tool_name
    assert tool_result["tool_call_id"] == "call_1"
    assert tool_result["result"]["content"] == [{"type": "text", "text": "ping"}]
