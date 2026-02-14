from __future__ import annotations

from typing import Mapping

from packages.agent_core.executor import ToolExecutor
from packages.agent_core.tools import ToolSpec as AgentToolSpec
from packages.llm_gateway import ToolSpec as LlmToolSpec

from .echo import ECHO_AGENT_TOOL_SPEC, ECHO_LLM_TOOL_SPEC, EchoToolExecutor
from .noop import NOOP_AGENT_TOOL_SPEC, NOOP_LLM_TOOL_SPEC, NoopToolExecutor
from .web_fetch import WEB_FETCH_AGENT_TOOL_SPEC, WEB_FETCH_LLM_TOOL_SPEC, WebFetchToolExecutor
from .web_search import WEB_SEARCH_AGENT_TOOL_SPEC, WEB_SEARCH_LLM_TOOL_SPEC, WebSearchToolExecutor


def builtin_agent_tool_specs() -> tuple[AgentToolSpec, ...]:
    return (ECHO_AGENT_TOOL_SPEC, NOOP_AGENT_TOOL_SPEC, WEB_SEARCH_AGENT_TOOL_SPEC, WEB_FETCH_AGENT_TOOL_SPEC)


def builtin_llm_tool_specs() -> tuple[LlmToolSpec, ...]:
    return (ECHO_LLM_TOOL_SPEC, NOOP_LLM_TOOL_SPEC, WEB_SEARCH_LLM_TOOL_SPEC, WEB_FETCH_LLM_TOOL_SPEC)


def create_builtin_tool_executors() -> Mapping[str, ToolExecutor]:
    return {
        ECHO_AGENT_TOOL_SPEC.name: EchoToolExecutor(),
        NOOP_AGENT_TOOL_SPEC.name: NoopToolExecutor(),
        WEB_SEARCH_AGENT_TOOL_SPEC.name: WebSearchToolExecutor(),
        WEB_FETCH_AGENT_TOOL_SPEC.name: WebFetchToolExecutor(),
    }


__all__ = [
    "builtin_agent_tool_specs",
    "builtin_llm_tool_specs",
    "create_builtin_tool_executors",
]
