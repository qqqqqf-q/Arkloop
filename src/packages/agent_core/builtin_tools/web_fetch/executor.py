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

from .basic import BasicWebFetchHttpError, BasicWebFetchProvider
from .config import WebFetchConfig
from .provider import WebFetchProvider, WebFetchResult
from .url_policy import UrlPolicyDeniedError, ensure_url_allowed

_ERROR_CLASS_ARGS_INVALID = "tool.args_invalid"
_ERROR_CLASS_TIMEOUT = "tool.timeout"
_ERROR_CLASS_FETCH_FAILED = "tool.fetch_failed"
_ERROR_CLASS_URL_DENIED = "tool.url_denied"
_ERROR_CLASS_NOT_CONFIGURED = "tool.not_configured"

_DEFAULT_TIMEOUT_SECONDS = 15.0
_MAX_LENGTH_LIMIT = 200_000

WEB_FETCH_AGENT_TOOL_SPEC = AgentToolSpec(
    name="web_fetch",
    version="1",
    description="抓取网页内容并提取正文",
    risk_level="medium",
    side_effects=False,
)

WEB_FETCH_LLM_TOOL_SPEC = LlmToolSpec(
    name="web_fetch",
    description="抓取网页内容，返回 title/content（纯文本）",
    json_schema={
        "type": "object",
        "properties": {
            "url": {"type": "string", "minLength": 1},
            "max_length": {"type": "integer", "minimum": 1, "maximum": _MAX_LENGTH_LIMIT},
        },
        "required": ["url", "max_length"],
        "additionalProperties": False,
    },
)


def _provider_from_env() -> WebFetchProvider | None:
    config = WebFetchConfig.from_env()
    if config is None:
        return None

    if config.provider_kind == "basic":
        return BasicWebFetchProvider()

    raise ValueError(f"web_fetch provider 未实现：{config.provider_kind}")


class WebFetchToolExecutor:
    def __init__(
        self,
        *,
        provider: WebFetchProvider | None = None,
        provider_factory: Callable[[], WebFetchProvider | None] | None = None,
        fallback_provider_factory: Callable[[], WebFetchProvider] | None = None,
        timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS,
    ) -> None:
        self._provider = provider
        self._provider_factory = provider_factory or _provider_from_env
        self._fallback_provider_factory = fallback_provider_factory or BasicWebFetchProvider
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
        url, max_length, error = _parse_web_fetch_args(args)
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
                        message="web_fetch 配置无效",
                        details={"reason": str(exc)},
                    ),
                    duration_ms=_duration_ms(started),
                )
            if provider is None:
                provider = self._fallback_provider_factory()

        try:
            ensure_url_allowed(url)
        except UrlPolicyDeniedError as exc:
            details: dict[str, object] = {"reason": exc.reason}
            if exc.details:
                details.update(dict(exc.details))
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=_ERROR_CLASS_URL_DENIED,
                    message="web_fetch URL 被安全策略拒绝",
                    details=details,
                ),
                duration_ms=_duration_ms(started),
            )

        try:
            with anyio.fail_after(self._timeout_seconds):
                result = await provider.fetch(url=url, max_length=max_length)
        except TimeoutError:
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=_ERROR_CLASS_TIMEOUT,
                    message="web_fetch 超时",
                    details={"timeout_seconds": self._timeout_seconds},
                ),
                duration_ms=_duration_ms(started),
            )
        except BasicWebFetchHttpError as exc:
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=_ERROR_CLASS_FETCH_FAILED,
                    message="web_fetch 请求失败",
                    details={"status_code": exc.status_code},
                ),
                duration_ms=_duration_ms(started),
            )
        except Exception as exc:
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=_ERROR_CLASS_FETCH_FAILED,
                    message="web_fetch 执行失败",
                    details={"exception_type": type(exc).__name__},
                ),
                duration_ms=_duration_ms(started),
            )

        if not isinstance(result, WebFetchResult):
            return ToolExecutionResult(
                error=ToolExecutionError(
                    error_class=_ERROR_CLASS_FETCH_FAILED,
                    message="web_fetch 返回结果类型不正确",
                ),
                duration_ms=_duration_ms(started),
            )

        return ToolExecutionResult(result_json=result.to_json(), duration_ms=_duration_ms(started))


def _parse_web_fetch_args(
    args: dict[str, Any],
) -> tuple[str | None, int | None, ToolExecutionError | None]:
    unknown = [key for key in args.keys() if key not in {"url", "max_length"}]
    if unknown:
        return None, None, ToolExecutionError(
            error_class=_ERROR_CLASS_ARGS_INVALID,
            message="工具参数不支持额外字段",
            details={"unknown_fields": sorted(unknown)},
        )

    url = args.get("url")
    if not isinstance(url, str) or not url.strip():
        return None, None, ToolExecutionError(
            error_class=_ERROR_CLASS_ARGS_INVALID,
            message="参数 url 必须为非空字符串",
            details={"field": "url"},
        )

    max_length = args.get("max_length")
    if not isinstance(max_length, int):
        return None, None, ToolExecutionError(
            error_class=_ERROR_CLASS_ARGS_INVALID,
            message="参数 max_length 必须为整数",
            details={"field": "max_length"},
        )
    if max_length <= 0 or max_length > _MAX_LENGTH_LIMIT:
        return None, None, ToolExecutionError(
            error_class=_ERROR_CLASS_ARGS_INVALID,
            message=f"参数 max_length 必须在 1..{_MAX_LENGTH_LIMIT} 之间",
            details={"field": "max_length", "max": _MAX_LENGTH_LIMIT},
        )

    return url.strip(), int(max_length), None


def _duration_ms(started: float) -> int:
    elapsed = time.monotonic() - started
    millis = int(elapsed * 1000)
    return millis if millis >= 0 else 0


__all__ = [
    "WEB_FETCH_AGENT_TOOL_SPEC",
    "WEB_FETCH_LLM_TOOL_SPEC",
    "WebFetchToolExecutor",
]

