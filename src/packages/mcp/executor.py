from __future__ import annotations

import asyncio
import time
from typing import Any, Mapping

from packages.agent_core.executor import (
    ToolExecutionContext,
    ToolExecutionError,
    ToolExecutionResult,
)

from .client import (
    McpClientError,
    McpDisconnectedError,
    McpRpcError,
    McpStdioClient,
    McpTimeoutError,
)
from .config import McpServerConfig
from .pool import McpStdioClientPool, McpStdioPoolKey, get_default_mcp_stdio_client_pool

ERROR_CLASS_MCP_TIMEOUT = "mcp.timeout"
ERROR_CLASS_MCP_DISCONNECTED = "mcp.disconnected"
ERROR_CLASS_MCP_RPC_ERROR = "mcp.rpc_error"
ERROR_CLASS_MCP_PROTOCOL_ERROR = "mcp.protocol_error"
ERROR_CLASS_MCP_TOOL_ERROR = "mcp.tool_error"


class McpToolExecutor:
    def __init__(
        self,
        *,
        server: McpServerConfig,
        remote_tool_name_by_tool_name: Mapping[str, str],
        pool: McpStdioClientPool | None = None,
    ) -> None:
        self._server = server
        self._remote_tool_name_by_tool_name = dict(remote_tool_name_by_tool_name)
        self._pool = pool

    async def execute(
        self,
        *,
        tool_name: str,
        args: dict[str, Any],
        context: ToolExecutionContext,
        tool_call_id: str | None = None,
    ) -> ToolExecutionResult:
        _ = (context, tool_call_id)
        started = time.monotonic()
        key = McpStdioPoolKey.from_context(org_id=context.org_id, server_id=self._server.server_id)
        pool = self._pool or get_default_mcp_stdio_client_pool()

        remote_name = self._remote_tool_name_by_tool_name.get(tool_name)
        if remote_name is None:
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=ERROR_CLASS_MCP_PROTOCOL_ERROR,
                    message="MCP 工具未注册",
                    details={"tool_name": tool_name, "server_id": self._server.server_id},
                ),
                duration_ms=_duration_ms(started),
            )

        timeout_ms = context.timeout_ms or self._server.call_timeout_ms

        client: McpStdioClient | None = None
        should_evict = False
        error_result: ToolExecutionResult | None = None
        result = None

        try:
            client = await pool.borrow(key=key, server=self._server)
            result = await client.call_tool(name=remote_name, arguments=args, timeout_ms=timeout_ms)
        except McpTimeoutError as exc:
            error_result = ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=ERROR_CLASS_MCP_TIMEOUT,
                    message=str(exc),
                    details={"tool_name": tool_name, "server_id": self._server.server_id},
                ),
                duration_ms=_duration_ms(started),
            )
        except McpDisconnectedError as exc:
            should_evict = True
            error_result = ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=ERROR_CLASS_MCP_DISCONNECTED,
                    message=str(exc),
                    details={"tool_name": tool_name, "server_id": self._server.server_id},
                ),
                duration_ms=_duration_ms(started),
            )
        except McpRpcError as exc:
            error_result = ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=ERROR_CLASS_MCP_RPC_ERROR,
                    message=str(exc),
                    details={
                        "tool_name": tool_name,
                        "server_id": self._server.server_id,
                        "code": exc.code,
                        "data": exc.data,
                    },
                ),
                duration_ms=_duration_ms(started),
            )
        except McpClientError as exc:
            should_evict = True
            error_result = ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=ERROR_CLASS_MCP_PROTOCOL_ERROR,
                    message=str(exc),
                    details={"tool_name": tool_name, "server_id": self._server.server_id},
                ),
                duration_ms=_duration_ms(started),
            )
        except asyncio.CancelledError:
            should_evict = True
            raise
        except Exception as exc:
            should_evict = True
            error_result = ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=ERROR_CLASS_MCP_PROTOCOL_ERROR,
                    message="MCP 工具调用失败",
                    details={
                        "tool_name": tool_name,
                        "server_id": self._server.server_id,
                        "exception_type": type(exc).__name__,
                    },
                ),
                duration_ms=_duration_ms(started),
            )
        finally:
            if client is None:
                pass
            elif should_evict:
                await _safe_pool_evict(pool=pool, key=key, client=client)
            else:
                await _safe_pool_release(pool=pool, key=key, client=client)

        if error_result is not None:
            return error_result

        if result is None:
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=ERROR_CLASS_MCP_PROTOCOL_ERROR,
                    message="MCP 工具返回空结果",
                    details={"tool_name": tool_name, "server_id": self._server.server_id},
                ),
                duration_ms=_duration_ms(started),
            )

        if result.is_error:
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=ERROR_CLASS_MCP_TOOL_ERROR,
                    message="MCP 工具返回错误",
                    details={
                        "tool_name": tool_name,
                        "server_id": self._server.server_id,
                        "content": result.content,
                    },
                ),
                duration_ms=_duration_ms(started),
            )

        return ToolExecutionResult(
            result_json={"content": result.content},
            duration_ms=_duration_ms(started),
        )


def _duration_ms(started: float) -> int:
    elapsed = time.monotonic() - started
    millis = int(elapsed * 1000)
    return millis if millis >= 0 else 0


async def _safe_pool_release(*, pool: McpStdioClientPool, key: McpStdioPoolKey, client: McpStdioClient) -> None:
    try:
        await pool.release(key=key, client=client)
    except Exception:
        pass


async def _safe_pool_evict(*, pool: McpStdioClientPool, key: McpStdioPoolKey, client: McpStdioClient) -> None:
    try:
        await pool.evict(key=key, client=client)
    except Exception:
        pass


__all__ = [
    "ERROR_CLASS_MCP_DISCONNECTED",
    "ERROR_CLASS_MCP_PROTOCOL_ERROR",
    "ERROR_CLASS_MCP_RPC_ERROR",
    "ERROR_CLASS_MCP_TIMEOUT",
    "ERROR_CLASS_MCP_TOOL_ERROR",
    "McpToolExecutor",
]
