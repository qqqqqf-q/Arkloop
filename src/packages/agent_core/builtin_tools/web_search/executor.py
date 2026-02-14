from __future__ import annotations

import time
from typing import Any, Callable

import anyio

from packages.agent_core.executor import (
    ToolExecutionContext,
    ToolExecutionError,
    ToolExecutionResult,
)
from packages.agent_core.tools import ToolSpec as AgentToolSpec
from packages.llm_gateway import ToolSpec as LlmToolSpec

from .config import WebSearchConfig
from .provider import WebSearchProvider, WebSearchResult
from .searxng import SearxngSearchHttpError, SearxngWebSearchProvider
from .tavily import TavilySearchHttpError, TavilyWebSearchProvider

_ERROR_CLASS_ARGS_INVALID = "tool.args_invalid"
_ERROR_CLASS_NOT_CONFIGURED = "tool.not_configured"
_ERROR_CLASS_TIMEOUT = "tool.timeout"
_ERROR_CLASS_SEARCH_FAILED = "tool.search_failed"

_DEFAULT_TIMEOUT_SECONDS = 10.0
_MAX_RESULTS_LIMIT = 20

WEB_SEARCH_AGENT_TOOL_SPEC = AgentToolSpec(
    name="web_search",
    version="1",
    description="搜索互联网并返回摘要结果",
    risk_level="low",
    side_effects=False,
)

WEB_SEARCH_LLM_TOOL_SPEC = LlmToolSpec(
    name="web_search",
    description="搜索互联网并返回标题/链接/摘要",
    json_schema={
        "type": "object",
        "properties": {
            "query": {"type": "string", "minLength": 1},
            "max_results": {"type": "integer", "minimum": 1, "maximum": _MAX_RESULTS_LIMIT},
        },
        "required": ["query", "max_results"],
        "additionalProperties": False,
    },
)


def _provider_from_env() -> WebSearchProvider | None:
    config = WebSearchConfig.from_env()
    if config is None:
        return None

    if config.provider_kind == "searxng":
        if not config.searxng_base_url:
            raise ValueError("SearXNG base_url 未配置")
        return SearxngWebSearchProvider(base_url=config.searxng_base_url)

    if config.provider_kind == "tavily":
        if not config.tavily_api_key:
            raise ValueError("Tavily api_key 未配置")
        return TavilyWebSearchProvider(api_key=config.tavily_api_key)

    raise ValueError(f"web_search provider 未实现：{config.provider_kind}")


class WebSearchToolExecutor:
    def __init__(
        self,
        *,
        provider: WebSearchProvider | None = None,
        provider_factory: Callable[[], WebSearchProvider | None] | None = None,
        timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS,
    ) -> None:
        self._provider = provider
        self._provider_factory = provider_factory or _provider_from_env
        self._timeout_seconds = float(timeout_seconds)

    async def execute(
        self,
        *,
        tool_name: str,
        args: dict[str, Any],
        context: ToolExecutionContext,
        tool_call_id: str | None = None,
    ) -> ToolExecutionResult:
        _ = (tool_name, context, tool_call_id)
        started = time.monotonic()
        query, max_results, error = _parse_web_search_args(args)
        if error is not None:
            return ToolExecutionResult(error=error, duration_ms=_duration_ms(started))

        provider = self._provider
        if provider is None:
            try:
                provider = self._provider_factory()
            except ValueError as exc:
                return ToolExecutionResult(
                    error=ToolExecutionError(
                        error_class=_ERROR_CLASS_NOT_CONFIGURED,
                        message="web_search 配置无效",
                        details={"reason": str(exc)},
                    ),
                    duration_ms=_duration_ms(started),
                )
            if provider is None:
                return ToolExecutionResult(
                    error=ToolExecutionError(
                        error_class=_ERROR_CLASS_NOT_CONFIGURED,
                        message="web_search 未配置 backend",
                    ),
                    duration_ms=_duration_ms(started),
                )

        try:
            with anyio.fail_after(self._timeout_seconds):
                results = await provider.search(query=query, max_results=max_results)
        except TimeoutError:
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=_ERROR_CLASS_TIMEOUT,
                    message="web_search 超时",
                    details={"timeout_seconds": self._timeout_seconds},
                ),
                duration_ms=_duration_ms(started),
            )
        except (SearxngSearchHttpError, TavilySearchHttpError) as exc:
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=_ERROR_CLASS_SEARCH_FAILED,
                    message="web_search 请求失败",
                    details={"status_code": exc.status_code},
                ),
                duration_ms=_duration_ms(started),
            )
        except Exception as exc:
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=_ERROR_CLASS_SEARCH_FAILED,
                    message="web_search 执行失败",
                    details={"exception_type": type(exc).__name__},
                ),
                duration_ms=_duration_ms(started),
            )

        if not isinstance(results, list) or not all(isinstance(item, WebSearchResult) for item in results):
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=_ERROR_CLASS_SEARCH_FAILED,
                    message="web_search 返回结果类型不正确",
                ),
                duration_ms=_duration_ms(started),
            )

        payload = {"results": [item.to_json() for item in results]}
        return ToolExecutionResult(result_json=payload, duration_ms=_duration_ms(started))


def _parse_web_search_args(
    args: dict[str, Any],
) -> tuple[str | None, int | None, ToolExecutionError | None]:
    unknown = [key for key in args.keys() if key not in {"query", "max_results"}]
    if unknown:
        return None, None, ToolExecutionError(
            error_class=_ERROR_CLASS_ARGS_INVALID,
            message="工具参数不支持额外字段",
            details={"unknown_fields": sorted(unknown)},
        )

    query = args.get("query")
    if not isinstance(query, str) or not query.strip():
        return None, None, ToolExecutionError(
            error_class=_ERROR_CLASS_ARGS_INVALID,
            message="参数 query 必须为非空字符串",
            details={"field": "query"},
        )

    max_results = args.get("max_results")
    if not isinstance(max_results, int):
        return None, None, ToolExecutionError(
            error_class=_ERROR_CLASS_ARGS_INVALID,
            message="参数 max_results 必须为整数",
            details={"field": "max_results"},
        )
    if max_results <= 0 or max_results > _MAX_RESULTS_LIMIT:
        return None, None, ToolExecutionError(
            error_class=_ERROR_CLASS_ARGS_INVALID,
            message=f"参数 max_results 必须在 1..{_MAX_RESULTS_LIMIT} 之间",
            details={"field": "max_results", "max": _MAX_RESULTS_LIMIT},
        )

    return query.strip(), int(max_results), None


def _duration_ms(started: float) -> int:
    elapsed = time.monotonic() - started
    millis = int(elapsed * 1000)
    return millis if millis >= 0 else 0


__all__ = [
    "WEB_SEARCH_AGENT_TOOL_SPEC",
    "WEB_SEARCH_LLM_TOOL_SPEC",
    "WebSearchToolExecutor",
]
