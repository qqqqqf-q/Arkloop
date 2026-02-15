from __future__ import annotations

import asyncio
from dataclasses import dataclass
import hashlib
import logging
import re
from typing import Mapping

from packages.agent_core.executor import ToolExecutor
from packages.agent_core.tools import ToolSpec as AgentToolSpec
from packages.llm_gateway import ToolSpec as LlmToolSpec

from .client import McpStdioClient, McpTool
from .config import McpConfig, McpServerConfig
from .executor import McpToolExecutor
from .pool import McpStdioClientPool

_TOOL_NAME_SAFE_RE = re.compile(r"[^A-Za-z0-9_-]+")
_LOGGER = logging.getLogger("arkloop.mcp")


@dataclass(frozen=True, slots=True)
class McpToolRegistration:
    agent_specs: tuple[AgentToolSpec, ...]
    llm_specs: tuple[LlmToolSpec, ...]
    executors: Mapping[str, ToolExecutor]


async def discover_mcp_tools(*, config: McpConfig, pool: McpStdioClientPool | None = None) -> McpToolRegistration:
    agent_specs: list[AgentToolSpec] = []
    llm_specs: list[LlmToolSpec] = []
    executors: dict[str, ToolExecutor] = {}

    discovered_by_server: list[tuple[McpServerConfig, tuple[McpTool, ...]]] = []
    base_counts: dict[str, int] = {}

    for server in config.servers:
        try:
            discovered = await _discover_server_tools(server)
        except Exception as exc:
            _LOGGER.warning(
                "MCP server 工具发现失败，已跳过",
                extra={"server_id": server.server_id, "reason": str(exc)},
            )
            continue
        if not discovered:
            continue

        tools: tuple[McpTool, ...] = tuple(discovered)
        discovered_by_server.append((server, tools))
        for tool in tools:
            base = _mcp_tool_base_name(server_id=server.server_id, tool_name=tool.name)
            base_counts[base] = base_counts.get(base, 0) + 1

    used_names: set[str] = set()
    for server, tools in discovered_by_server:
        tool_map: dict[str, str] = {}
        for tool in tools:
            base = _mcp_tool_base_name(server_id=server.server_id, tool_name=tool.name)
            if base_counts.get(base, 0) > 1:
                raw = _mcp_tool_raw_name(server_id=server.server_id, tool_name=tool.name)
                internal_name = f"{base}__{_short_hash(raw)}"
            else:
                internal_name = base
            internal_name = _ensure_unique_tool_name(internal_name, used_names)

            tool_map[internal_name] = tool.name
            description = tool.description or tool.title or f"MCP 工具：{tool.name}"

            agent_specs.append(
                AgentToolSpec(
                    name=internal_name,
                    version="1",
                    description=description,
                    risk_level="high",
                    side_effects=True,
                )
            )
            llm_specs.append(
                LlmToolSpec(
                    name=internal_name,
                    description=description,
                    json_schema=dict(tool.input_schema),
                )
            )

        executor = McpToolExecutor(server=server, remote_tool_name_by_tool_name=tool_map, pool=pool)
        for internal_name in tool_map.keys():
            executors[internal_name] = executor

    return McpToolRegistration(
        agent_specs=tuple(agent_specs),
        llm_specs=tuple(llm_specs),
        executors=executors,
    )


def load_mcp_tool_registration_from_env(*, pool: McpStdioClientPool | None = None) -> McpToolRegistration:
    empty = McpToolRegistration(agent_specs=(), llm_specs=(), executors={})
    try:
        cfg = McpConfig.from_env()
    except Exception as exc:
        _LOGGER.warning("读取 MCP 配置失败，已禁用", extra={"reason": str(exc)})
        return empty

    if cfg is None or not cfg.servers:
        return empty

    try:
        asyncio.get_running_loop()
    except RuntimeError:
        pass
    else:
        _LOGGER.warning("检测到运行中的事件循环，跳过同步 MCP 工具发现")
        return empty

    try:
        return asyncio.run(discover_mcp_tools(config=cfg, pool=pool))
    except Exception as exc:
        _LOGGER.warning("MCP 工具发现失败，已禁用", extra={"reason": str(exc)})
        return empty


async def _discover_server_tools(server: McpServerConfig):
    async with McpStdioClient(server=server) as client:
        return await client.list_tools(timeout_ms=server.call_timeout_ms)


def _mcp_tool_name(*, server_id: str, tool_name: str) -> str:
    return _mcp_tool_base_name(server_id=server_id, tool_name=tool_name)


def _mcp_tool_raw_name(*, server_id: str, tool_name: str) -> str:
    return f"mcp__{server_id}__{tool_name}"


def _mcp_tool_base_name(*, server_id: str, tool_name: str) -> str:
    raw = _mcp_tool_raw_name(server_id=server_id, tool_name=tool_name)
    cleaned = _TOOL_NAME_SAFE_RE.sub("_", raw).strip("_")
    return cleaned if cleaned else "mcp_tool"


def _short_hash(value: str) -> str:
    return hashlib.sha256(value.encode("utf-8")).hexdigest()[:8]


def _ensure_unique_tool_name(name: str, used: set[str]) -> str:
    if name not in used:
        used.add(name)
        return name
    index = 2
    while True:
        candidate = f"{name}_{index}"
        if candidate not in used:
            used.add(candidate)
            return candidate
        index += 1


__all__ = ["McpToolRegistration", "discover_mcp_tools", "load_mcp_tool_registration_from_env"]
