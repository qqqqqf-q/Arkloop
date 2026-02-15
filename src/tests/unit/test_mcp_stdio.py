from __future__ import annotations

import asyncio
import json
import uuid

import anyio
import pytest

from packages.agent_core import (
    AgentRunContext,
    DispatchingToolExecutor,
    RunEventEmitter,
    ToolAllowlist,
    ToolExecutionContext,
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
from packages.mcp.client import McpStdioClient
from packages.mcp.config import McpConfig, McpServerConfig
from packages.mcp.executor import ERROR_CLASS_MCP_DISCONNECTED, ERROR_CLASS_MCP_TIMEOUT, McpToolExecutor
from packages.mcp.pool import McpStdioClientPool
from packages.mcp.registry import discover_mcp_tools


class _FakeReadStream:
    def __init__(self) -> None:
        self._queue: asyncio.Queue[bytes] = asyncio.Queue()

    async def readline(self) -> bytes:
        return await self._queue.get()

    def feed_json(self, payload: dict) -> None:
        raw = json.dumps(payload, ensure_ascii=False, separators=(",", ":"), sort_keys=True)
        self._queue.put_nowait(raw.encode("utf-8") + b"\n")

    def close(self) -> None:
        self._queue.put_nowait(b"")


class _FakeProcess:
    def __init__(self, *, server: "_FakeMcpServer") -> None:
        self.returncode: int | None = None
        self.stdout = _FakeReadStream()
        self.stderr = _FakeReadStream()
        self.stdin = _FakeWriteStream(server=server)
        server.attach(process=self)

    def terminate(self) -> None:
        self._finish(0)

    def kill(self) -> None:
        self._finish(-9)

    async def wait(self) -> int:
        return 0 if self.returncode is None else int(self.returncode)

    def crash(self) -> None:
        self._finish(1)

    def _finish(self, code: int) -> None:
        if self.returncode is not None:
            return
        self.returncode = int(code)
        self.stdout.close()
        self.stderr.close()


class _FakeWriteStream:
    def __init__(self, *, server: "_FakeMcpServer") -> None:
        self._server = server
        self._buffer = b""
        self._closed = False

    def write(self, data: bytes) -> None:
        if self._closed:
            raise BrokenPipeError("stdin 已关闭")
        self._buffer += data
        while b"\n" in self._buffer:
            line, self._buffer = self._buffer.split(b"\n", 1)
            raw = line.strip()
            if not raw:
                continue
            try:
                message = json.loads(raw.decode("utf-8", errors="replace"))
            except json.JSONDecodeError:
                continue
            if isinstance(message, dict):
                self._server.handle(message)

    async def drain(self) -> None:
        await asyncio.sleep(0)

    def close(self) -> None:
        self._closed = True


class _FakeMcpServer:
    def __init__(self, *, mode: str = "normal") -> None:
        self._mode = mode
        self._process: _FakeProcess | None = None
        self.initialize_calls = 0

    def attach(self, *, process: _FakeProcess) -> None:
        self._process = process

    def handle(self, request: dict) -> None:
        method = request.get("method")
        request_id = request.get("id")
        params = request.get("params") or {}

        if method == "initialize":
            self.initialize_calls += 1
            protocol_version = params.get("protocolVersion") or "2024-11-05"
            self._reply(
                request_id,
                {
                    "protocolVersion": protocol_version,
                    "capabilities": {"tools": {}},
                    "serverInfo": {"name": "fake-mcp-server", "version": "0"},
                },
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
            self._reply(request_id, {"tools": tools})
            return

        if method == "tools/call":
            if self._mode == "timeout_on_call":
                return
            if self._mode == "crash_on_call":
                if self._process is not None:
                    self._process.crash()
                return

            name = params.get("name")
            arguments = params.get("arguments") or {}
            if name != "echo":
                self._error(request_id, code=-32601, message="unknown tool")
                return
            self._reply(
                request_id,
                {
                    "content": [{"type": "text", "text": str(arguments.get("text", ""))}],
                    "isError": False,
                },
            )
            return

        self._error(request_id, code=-32601, message="unknown method")

    def _reply(self, request_id: object, result: dict) -> None:
        if not isinstance(request_id, int):
            return
        if self._process is None:
            return
        self._process.stdout.feed_json({"jsonrpc": "2.0", "id": request_id, "result": result})

    def _error(self, request_id: object, *, code: int, message: str) -> None:
        if not isinstance(request_id, int):
            return
        if self._process is None:
            return
        self._process.stdout.feed_json(
            {"jsonrpc": "2.0", "id": request_id, "error": {"code": int(code), "message": message}}
        )


def _server_config(*, server_id: str = "test", timeout_ms: int = 1000) -> McpServerConfig:
    return McpServerConfig(server_id=server_id, command="fake-mcp", call_timeout_ms=timeout_ms)


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


def _patch_fake_subprocess(monkeypatch: pytest.MonkeyPatch, *, mode: str) -> None:
    async def _fake_create_subprocess_exec(*args, **kwargs):  # type: ignore[no-untyped-def]
        _ = (args, kwargs)
        server = _FakeMcpServer(mode=mode)
        return _FakeProcess(server=server)

    monkeypatch.setattr(asyncio, "create_subprocess_exec", _fake_create_subprocess_exec)


def test_mcp_stdio_client_can_list_and_call_tool(monkeypatch: pytest.MonkeyPatch) -> None:
    _patch_fake_subprocess(monkeypatch, mode="normal")
    server = _server_config()

    async def _run():
        async with McpStdioClient(server=server) as client:
            tools = await client.list_tools()
            assert [tool.name for tool in tools] == ["echo"]
            result = await client.call_tool(name="echo", arguments={"text": "hi"})
            assert result.is_error is False
            assert result.content == [{"type": "text", "text": "hi"}]

    anyio.run(_run)


def test_mcp_stdio_executor_works_in_agent_loop(monkeypatch: pytest.MonkeyPatch) -> None:
    _patch_fake_subprocess(monkeypatch, mode="normal")
    server = _server_config()

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


def test_mcp_stdio_executor_timeout_returns_clear_error(monkeypatch: pytest.MonkeyPatch) -> None:
    _patch_fake_subprocess(monkeypatch, mode="timeout_on_call")
    server = _server_config(timeout_ms=500)

    async def _run():
        pool = McpStdioClientPool(ttl_seconds=None, max_clients_per_server=1)
        try:
            registration = await discover_mcp_tools(config=McpConfig(servers=(server,)), pool=pool)
            tool_name = registration.agent_specs[0].name
            executor = registration.executors[tool_name]
            result = await executor.execute(
                tool_name=tool_name,
                args={"text": "slow"},
                context=ToolExecutionContext(run_id=uuid.uuid4(), timeout_ms=50),
                tool_call_id="call_1",
            )
            assert result.error is not None
            assert result.error.error_class == ERROR_CLASS_MCP_TIMEOUT
        finally:
            await pool.close_all()

    anyio.run(_run)


def test_mcp_stdio_executor_crash_returns_clear_error(monkeypatch: pytest.MonkeyPatch) -> None:
    _patch_fake_subprocess(monkeypatch, mode="crash_on_call")
    server = _server_config(timeout_ms=200)

    async def _run():
        pool = McpStdioClientPool(ttl_seconds=None, max_clients_per_server=1)
        try:
            registration = await discover_mcp_tools(config=McpConfig(servers=(server,)), pool=pool)
            tool_name = registration.agent_specs[0].name
            executor = registration.executors[tool_name]
            result = await executor.execute(
                tool_name=tool_name,
                args={"text": "hi"},
                context=ToolExecutionContext(run_id=uuid.uuid4()),
                tool_call_id="call_1",
            )
            assert result.error is not None
            assert result.error.error_class == ERROR_CLASS_MCP_DISCONNECTED
        finally:
            await pool.close_all()

    anyio.run(_run)


def test_mcp_stdio_pool_reuses_session_without_reinitialize(monkeypatch: pytest.MonkeyPatch) -> None:
    created_servers: list[_FakeMcpServer] = []

    async def _fake_create_subprocess_exec(*args, **kwargs):  # type: ignore[no-untyped-def]
        _ = (args, kwargs)
        server = _FakeMcpServer(mode="normal")
        created_servers.append(server)
        return _FakeProcess(server=server)

    monkeypatch.setattr(asyncio, "create_subprocess_exec", _fake_create_subprocess_exec)

    async def _run():
        pool = McpStdioClientPool(ttl_seconds=None, max_clients_per_server=1)
        server_cfg = _server_config()
        executor = McpToolExecutor(
            server=server_cfg,
            remote_tool_name_by_tool_name={"echo": "echo"},
            pool=pool,
        )
        try:
            ctx = ToolExecutionContext(run_id=uuid.uuid4(), org_id=uuid.uuid4())
            first = await executor.execute(tool_name="echo", args={"text": "a"}, context=ctx, tool_call_id="c1")
            second = await executor.execute(tool_name="echo", args={"text": "b"}, context=ctx, tool_call_id="c2")
            assert first.error is None
            assert first.result_json == {"content": [{"type": "text", "text": "a"}]}
            assert second.error is None
            assert second.result_json == {"content": [{"type": "text", "text": "b"}]}
        finally:
            await pool.close_all()

    anyio.run(_run)
    assert len(created_servers) == 1
    assert created_servers[0].initialize_calls == 1


def test_mcp_stdio_pool_ttl_evicts_on_release(monkeypatch: pytest.MonkeyPatch) -> None:
    import packages.mcp.pool as mcp_pool

    created_servers: list[_FakeMcpServer] = []

    async def _fake_create_subprocess_exec(*args, **kwargs):  # type: ignore[no-untyped-def]
        _ = (args, kwargs)
        server = _FakeMcpServer(mode="normal")
        created_servers.append(server)
        return _FakeProcess(server=server)

    monotonic_values = iter([0.0, 2.0, 2.0, 2.0])

    def _fake_monotonic() -> float:
        return float(next(monotonic_values))

    monkeypatch.setattr(asyncio, "create_subprocess_exec", _fake_create_subprocess_exec)
    monkeypatch.setattr(mcp_pool, "_monotonic", _fake_monotonic)

    async def _run():
        pool = McpStdioClientPool(ttl_seconds=1.0, max_clients_per_server=1)
        server_cfg = _server_config()
        executor = McpToolExecutor(
            server=server_cfg,
            remote_tool_name_by_tool_name={"echo": "echo"},
            pool=pool,
        )
        try:
            ctx = ToolExecutionContext(run_id=uuid.uuid4(), org_id=uuid.uuid4())
            first = await executor.execute(tool_name="echo", args={"text": "a"}, context=ctx, tool_call_id="c1")
            second = await executor.execute(tool_name="echo", args={"text": "b"}, context=ctx, tool_call_id="c2")
            assert first.error is None
            assert second.error is None
        finally:
            await pool.close_all()

    anyio.run(_run)
    assert len(created_servers) == 2


def test_mcp_stdio_pool_crash_evicts_and_restarts_next_call(monkeypatch: pytest.MonkeyPatch) -> None:
    created_servers: list[_FakeMcpServer] = []
    modes = ["crash_on_call", "normal"]

    async def _fake_create_subprocess_exec(*args, **kwargs):  # type: ignore[no-untyped-def]
        _ = (args, kwargs)
        mode = modes.pop(0) if modes else "normal"
        server = _FakeMcpServer(mode=mode)
        created_servers.append(server)
        return _FakeProcess(server=server)

    monkeypatch.setattr(asyncio, "create_subprocess_exec", _fake_create_subprocess_exec)

    async def _run():
        pool = McpStdioClientPool(ttl_seconds=None, max_clients_per_server=1)
        server_cfg = _server_config()
        executor = McpToolExecutor(
            server=server_cfg,
            remote_tool_name_by_tool_name={"echo": "echo"},
            pool=pool,
        )
        try:
            ctx = ToolExecutionContext(run_id=uuid.uuid4(), org_id=uuid.uuid4())
            crashed = await executor.execute(tool_name="echo", args={"text": "x"}, context=ctx, tool_call_id="c1")
            assert crashed.error is not None
            assert crashed.error.error_class == ERROR_CLASS_MCP_DISCONNECTED
            ok = await executor.execute(tool_name="echo", args={"text": "ok"}, context=ctx, tool_call_id="c2")
            assert ok.error is None
            assert ok.result_json == {"content": [{"type": "text", "text": "ok"}]}
        finally:
            await pool.close_all()

    anyio.run(_run)
    assert len(created_servers) == 2
