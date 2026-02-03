from __future__ import annotations

from dataclasses import dataclass, field
import json
import logging
import os
import re
from typing import Any, AsyncIterator, Mapping, Optional

import anyio

from packages.config import load_dotenv_if_enabled

from .contract import (
    ERROR_CLASS_INTERNAL_ERROR,
    ERROR_CLASS_PROVIDER_NON_RETRYABLE,
    ERROR_CLASS_PROVIDER_RETRYABLE,
    LlmGatewayError,
    LlmGatewayRequest,
    LlmGatewayStreamEvent,
    LlmStreamMessageDelta,
    LlmStreamRunCompleted,
    LlmStreamRunFailed,
    LlmStreamToolCall,
    LlmUsage,
)
from .gateway import LlmGateway

_ANTHROPIC_API_KEY_ENV = "ARKLOOP_ANTHROPIC_API_KEY"
_ANTHROPIC_BASE_URL_ENV = "ARKLOOP_ANTHROPIC_BASE_URL"
_ANTHROPIC_VERSION_ENV = "ARKLOOP_ANTHROPIC_VERSION"
_ANTHROPIC_TOTAL_TIMEOUT_SECONDS_ENV = "ARKLOOP_ANTHROPIC_TOTAL_TIMEOUT_SECONDS"
_ANTHROPIC_ADVANCED_JSON_ENV = "ARKLOOP_ANTHROPIC_ADVANCED_JSON"

_DEFAULT_ANTHROPIC_BASE_URL = "https://api.anthropic.com/v1"
_DEFAULT_ANTHROPIC_VERSION = "2023-06-01"
_DEFAULT_ANTHROPIC_TOTAL_TIMEOUT_SECONDS = 60.0

_DEFAULT_MAX_OUTPUT_TOKENS = 1024
_MAX_ERROR_BODY_BYTES = 4096

_ALLOWED_ADVANCED_KEYS = {"extra_headers", "extra_query", "timeout_ms"}
_MAX_ADVANCED_FIELDS = 20
_MAX_ADVANCED_VALUE_LENGTH = 256
_ADVANCED_KEY_RE = re.compile(r"^[A-Za-z0-9-]+$")
_SENSITIVE_KEY_FRAGMENTS = (
    "authorization",
    "proxy-authorization",
    "cookie",
    "set-cookie",
    "x-api-key",
    "api-key",
    "api_key",
    "apikey",
    "token",
    "secret",
    "password",
    "passwd",
    "session",
)


def _parse_non_negative_float(value: str) -> float:
    cleaned = value.strip()
    parsed = float(cleaned)
    if parsed < 0:
        raise ValueError("必须为非负数")
    return parsed


def _normalize_base_url(value: str) -> str:
    cleaned = value.strip()
    if not cleaned:
        raise ValueError("base_url 不能为空")
    return cleaned.rstrip("/")


def _try_parse_json(raw: str | bytes) -> Any | None:
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return None


def _usage_from_mapping(data: Mapping[str, Any]) -> LlmUsage | None:
    def _maybe_int(value: Any) -> int | None:
        if value is None:
            return None
        if isinstance(value, bool):
            return None
        try:
            return int(value)
        except (TypeError, ValueError):
            return None

    input_tokens = _maybe_int(data.get("input_tokens"))
    output_tokens = _maybe_int(data.get("output_tokens"))
    total_tokens = _maybe_int(data.get("total_tokens") or data.get("total"))
    if input_tokens is None and output_tokens is None and total_tokens is None:
        return None
    return LlmUsage(input_tokens=input_tokens, output_tokens=output_tokens, total_tokens=total_tokens)


def _merge_usage(current: LlmUsage | None, update: LlmUsage | None) -> LlmUsage | None:
    if update is None:
        return current
    if current is None:
        return update
    merged = LlmUsage(
        input_tokens=update.input_tokens if update.input_tokens is not None else current.input_tokens,
        output_tokens=update.output_tokens
        if update.output_tokens is not None
        else current.output_tokens,
        total_tokens=update.total_tokens if update.total_tokens is not None else current.total_tokens,
    )
    if (
        merged.total_tokens is None
        and merged.input_tokens is not None
        and merged.output_tokens is not None
    ):
        return LlmUsage(
            input_tokens=merged.input_tokens,
            output_tokens=merged.output_tokens,
            total_tokens=merged.input_tokens + merged.output_tokens,
        )
    return merged


def _provider_error_class_from_status(status_code: int) -> str:
    if status_code in {408, 425, 429}:
        return ERROR_CLASS_PROVIDER_RETRYABLE
    if 500 <= status_code <= 599:
        return ERROR_CLASS_PROVIDER_RETRYABLE
    return ERROR_CLASS_PROVIDER_NON_RETRYABLE


def _looks_sensitive_key(key: str) -> bool:
    lowered = key.casefold()
    return any(fragment in lowered for fragment in _SENSITIVE_KEY_FRAGMENTS)


def _validated_advanced_json(raw: Mapping[str, Any] | None) -> tuple[dict[str, str], dict[str, str], float | None]:
    if not raw:
        return {}, {}, None

    unknown = set(raw.keys()) - _ALLOWED_ADVANCED_KEYS
    if unknown:
        joined = ", ".join(sorted(str(key) for key in unknown))
        raise ValueError(f"高级 JSON 配置包含未允许的键: {joined}")

    extra_headers: dict[str, str] = {}
    extra_query: dict[str, str] = {}
    timeout_seconds_override: float | None = None

    raw_headers = raw.get("extra_headers")
    if raw_headers is not None:
        if not isinstance(raw_headers, Mapping):
            raise ValueError("extra_headers 必须为 JSON 对象")
        if len(raw_headers) > _MAX_ADVANCED_FIELDS:
            raise ValueError("extra_headers 字段数过多")
        for key, value in raw_headers.items():
            if not isinstance(key, str) or not key.strip():
                raise ValueError("extra_headers 的键必须为非空字符串")
            if not _ADVANCED_KEY_RE.match(key):
                raise ValueError(f"extra_headers 键不合法: {key}")
            if _looks_sensitive_key(key):
                raise ValueError(f"extra_headers 不允许包含敏感键: {key}")
            if not isinstance(value, str):
                raise ValueError(f"extra_headers[{key}] 必须为字符串")
            cleaned_value = value.strip()
            if not cleaned_value:
                raise ValueError(f"extra_headers[{key}] 不能为空")
            if len(cleaned_value) > _MAX_ADVANCED_VALUE_LENGTH:
                raise ValueError(f"extra_headers[{key}] 过长")
            extra_headers[key] = cleaned_value

    raw_query = raw.get("extra_query")
    if raw_query is not None:
        if not isinstance(raw_query, Mapping):
            raise ValueError("extra_query 必须为 JSON 对象")
        if len(raw_query) > _MAX_ADVANCED_FIELDS:
            raise ValueError("extra_query 字段数过多")
        for key, value in raw_query.items():
            if not isinstance(key, str) or not key.strip():
                raise ValueError("extra_query 的键必须为非空字符串")
            if not _ADVANCED_KEY_RE.match(key):
                raise ValueError(f"extra_query 键不合法: {key}")
            if _looks_sensitive_key(key):
                raise ValueError(f"extra_query 不允许包含敏感键: {key}")

            if isinstance(value, bool):
                rendered = "true" if value else "false"
            elif isinstance(value, (int, float)):
                rendered = str(value)
            elif isinstance(value, str):
                rendered = value.strip()
            else:
                raise ValueError(f"extra_query[{key}] 必须为字符串/数字/布尔值")

            if not rendered:
                raise ValueError(f"extra_query[{key}] 不能为空")
            if len(rendered) > _MAX_ADVANCED_VALUE_LENGTH:
                raise ValueError(f"extra_query[{key}] 过长")
            extra_query[key] = rendered

    raw_timeout_ms = raw.get("timeout_ms")
    if raw_timeout_ms is not None:
        if isinstance(raw_timeout_ms, bool):
            raise ValueError("timeout_ms 必须为整数")
        try:
            parsed_timeout_ms = int(raw_timeout_ms)
        except (TypeError, ValueError) as exc:
            raise ValueError("timeout_ms 必须为整数") from exc
        if parsed_timeout_ms <= 0:
            raise ValueError("timeout_ms 必须为正整数")
        timeout_seconds_override = parsed_timeout_ms / 1000.0

    return extra_headers, extra_query, timeout_seconds_override


def _anthropic_error_message(error_json: Any | None, *, fallback: str) -> str:
    if isinstance(error_json, Mapping):
        error = error_json.get("error")
        if isinstance(error, Mapping):
            message = error.get("message")
            if isinstance(message, str) and message.strip():
                return message.strip()
        message = error_json.get("message")
        if isinstance(message, str) and message.strip():
            return message.strip()
    return fallback


def _anthropic_error_details(error_json: Any | None, *, status_code: int) -> dict[str, Any]:
    details: dict[str, Any] = {"status_code": int(status_code)}
    if not isinstance(error_json, Mapping):
        return details
    error = error_json.get("error")
    if not isinstance(error, Mapping):
        return details
    typ = error.get("type")
    if isinstance(typ, str) and typ.strip():
        details["anthropic_error_type"] = typ.strip()
    return details


def _to_anthropic_messages(request: LlmGatewayRequest) -> tuple[str | None, list[dict[str, Any]]]:
    system_parts: list[str] = []
    messages: list[dict[str, Any]] = []
    for message in request.messages:
        text = "".join(part.text for part in message.content)
        if message.role == "system":
            if text:
                system_parts.append(text)
            continue
        messages.append({"role": message.role, "content": [{"type": "text", "text": text}]})
    system = "\n".join(system_parts) if system_parts else None
    return system, messages


async def _aiter_sse_events(lines: AsyncIterator[str]) -> AsyncIterator[str]:
    data_lines: list[str] = []
    async for raw_line in lines:
        line = raw_line.rstrip("\r")
        if not line:
            if data_lines:
                yield "\n".join(data_lines)
                data_lines.clear()
            continue
        if line.startswith(":"):
            continue
        if line.startswith("data:"):
            data_lines.append(line[len("data:") :].lstrip())
            continue
    if data_lines:
        yield "\n".join(data_lines)


async def _iter_with_total_timeout(
    stream: AsyncIterator[LlmGatewayStreamEvent], *, total_timeout_seconds: float
) -> AsyncIterator[LlmGatewayStreamEvent]:
    close = getattr(stream, "aclose", None)
    iterator = stream.__aiter__()
    deadline = anyio.current_time() + float(total_timeout_seconds)
    try:
        while True:
            remaining = deadline - anyio.current_time()
            if remaining <= 0:
                raise TimeoutError
            with anyio.fail_after(remaining):
                try:
                    item = await iterator.__anext__()
                except StopAsyncIteration:
                    return
            yield item
    finally:
        if close is not None:
            try:
                await close()
            except Exception:
                pass


@dataclass(frozen=True, slots=True)
class AnthropicGatewayConfig:
    api_key: str = field(repr=False)
    base_url: str = _DEFAULT_ANTHROPIC_BASE_URL
    anthropic_version: str = _DEFAULT_ANTHROPIC_VERSION
    total_timeout_seconds: float = _DEFAULT_ANTHROPIC_TOTAL_TIMEOUT_SECONDS
    advanced_json: Mapping[str, Any] = field(default_factory=dict)

    @classmethod
    def from_env(cls, *, required: bool = False) -> Optional["AnthropicGatewayConfig"]:
        load_dotenv_if_enabled(override=False)

        api_key = (os.getenv(_ANTHROPIC_API_KEY_ENV) or "").strip()
        if not api_key:
            if required:
                raise ValueError(f"缺少环境变量 {_ANTHROPIC_API_KEY_ENV}")
            return None

        base_url = _DEFAULT_ANTHROPIC_BASE_URL
        raw_base_url = os.getenv(_ANTHROPIC_BASE_URL_ENV)
        if raw_base_url:
            base_url = _normalize_base_url(raw_base_url)

        anthropic_version = _DEFAULT_ANTHROPIC_VERSION
        raw_version = os.getenv(_ANTHROPIC_VERSION_ENV)
        if raw_version:
            cleaned = raw_version.strip()
            if not cleaned:
                raise ValueError(f"环境变量 {_ANTHROPIC_VERSION_ENV} 不能为空")
            anthropic_version = cleaned

        total_timeout_seconds = _DEFAULT_ANTHROPIC_TOTAL_TIMEOUT_SECONDS
        raw_timeout = os.getenv(_ANTHROPIC_TOTAL_TIMEOUT_SECONDS_ENV)
        if raw_timeout:
            total_timeout_seconds = _parse_non_negative_float(raw_timeout)

        advanced_json: Mapping[str, Any] = {}
        raw_advanced_json = os.getenv(_ANTHROPIC_ADVANCED_JSON_ENV)
        if raw_advanced_json:
            parsed = _try_parse_json(raw_advanced_json)
            if parsed is None:
                raise ValueError(f"环境变量 {_ANTHROPIC_ADVANCED_JSON_ENV} 不是合法 JSON")
            if not isinstance(parsed, Mapping):
                raise ValueError(f"环境变量 {_ANTHROPIC_ADVANCED_JSON_ENV} 必须为 JSON 对象")
            advanced_json = parsed

        return cls(
            api_key=api_key,
            base_url=base_url,
            anthropic_version=anthropic_version,
            total_timeout_seconds=total_timeout_seconds,
            advanced_json=advanced_json,
        )


class AnthropicLlmGateway(LlmGateway):
    def __init__(self, *, config: AnthropicGatewayConfig, client: Any | None = None) -> None:
        self._config = config
        self._client = client
        self._logger = logging.getLogger("arkloop.llm_gateway.anthropic")

        extra_headers, extra_query, timeout_override = _validated_advanced_json(config.advanced_json)
        reserved_headers = {"x-api-key", "anthropic-version", "accept", "content-type"}
        for key in extra_headers:
            if key.casefold() in reserved_headers:
                raise ValueError(f"extra_headers 不允许覆盖内置头: {key}")
        self._extra_headers = extra_headers
        self._extra_query = extra_query
        self._timeout_seconds_override = timeout_override

    async def stream(self, *, request: LlmGatewayRequest) -> AsyncIterator[LlmGatewayStreamEvent]:
        try:
            async for item in self._stream_once(request=request):
                yield item
        except TimeoutError:
            error = LlmGatewayError(
                error_class=ERROR_CLASS_PROVIDER_RETRYABLE,
                message="Anthropic 请求超时",
            )
            yield LlmStreamRunFailed(error=error)
        except Exception:
            self._logger.exception("Anthropic 流式请求异常")
            error = LlmGatewayError(
                error_class=ERROR_CLASS_INTERNAL_ERROR,
                message="Anthropic 流式请求异常",
            )
            yield LlmStreamRunFailed(error=error)

    async def _stream_once(self, *, request: LlmGatewayRequest) -> AsyncIterator[LlmGatewayStreamEvent]:
        httpx = _import_httpx()

        timeout_seconds = (
            self._timeout_seconds_override
            if self._timeout_seconds_override is not None
            else self._config.total_timeout_seconds
        )

        headers = {
            "x-api-key": self._config.api_key,
            "anthropic-version": self._config.anthropic_version,
            "Accept": "text/event-stream",
            "Content-Type": "application/json",
        }
        headers.update(self._extra_headers)

        system, messages = _to_anthropic_messages(request)
        max_tokens = (
            int(request.max_output_tokens)
            if request.max_output_tokens is not None
            else _DEFAULT_MAX_OUTPUT_TOKENS
        )
        if max_tokens <= 0:
            raise ValueError("max_output_tokens 必须为正整数")

        payload: dict[str, Any] = {
            "model": request.model,
            "stream": True,
            "messages": messages,
            "max_tokens": max_tokens,
        }
        if system is not None:
            payload["system"] = system
        if request.temperature is not None:
            payload["temperature"] = float(request.temperature)

        stream = self._stream_http(httpx=httpx, payload=payload, headers=headers)
        async for item in _iter_with_total_timeout(stream, total_timeout_seconds=timeout_seconds):
            yield item

    async def _stream_http(
        self,
        *,
        httpx: Any,
        payload: Mapping[str, Any],
        headers: Mapping[str, str],
    ) -> AsyncIterator[LlmGatewayStreamEvent]:
        client = self._client
        if client is None:
            timeout = httpx.Timeout(None)
            async with httpx.AsyncClient(base_url=self._config.base_url, timeout=timeout) as client:
                async for item in self._stream_http_with_client(
                    httpx=httpx,
                    client=client,
                    payload=payload,
                    headers=headers,
                ):
                    yield item
            return

        async for item in self._stream_http_with_client(
            httpx=httpx,
            client=client,
            payload=payload,
            headers=headers,
        ):
            yield item

    async def _stream_http_with_client(
        self,
        *,
        httpx: Any,
        client: Any,
        payload: Mapping[str, Any],
        headers: Mapping[str, str],
    ) -> AsyncIterator[LlmGatewayStreamEvent]:
        try:
            async with client.stream(
                "POST",
                "/messages",
                params=dict(self._extra_query),
                json=dict(payload),
                headers=dict(headers),
            ) as resp:
                if resp.status_code != 200:
                    body = await resp.aread()
                    body = body[:_MAX_ERROR_BODY_BYTES]
                    error_json = _try_parse_json(body)
                    error_class = _provider_error_class_from_status(int(resp.status_code))
                    message = _anthropic_error_message(
                        error_json,
                        fallback=f"Anthropic 返回 {resp.status_code}",
                    )
                    details = _anthropic_error_details(error_json, status_code=int(resp.status_code))
                    yield LlmStreamRunFailed(
                        error=LlmGatewayError(
                            error_class=error_class,
                            message=message,
                            details=details,
                        )
                    )
                    return

                usage: LlmUsage | None = None
                async for data in _aiter_sse_events(resp.aiter_lines()):
                    if data.strip() == "[DONE]":
                        yield LlmStreamRunCompleted(usage=usage)
                        return

                    item = _try_parse_json(data)
                    if item is None:
                        continue
                    if not isinstance(item, Mapping):
                        continue

                    raw_type = item.get("type")
                    typ = raw_type if isinstance(raw_type, str) else ""

                    if typ == "content_block_delta":
                        raw_delta = item.get("delta")
                        if not isinstance(raw_delta, Mapping):
                            continue
                        if raw_delta.get("type") != "text_delta":
                            continue
                        text = raw_delta.get("text")
                        if isinstance(text, str) and text:
                            yield LlmStreamMessageDelta(content_delta=text, role="assistant")
                        continue

                    if typ == "content_block_start":
                        block = item.get("content_block")
                        if not isinstance(block, Mapping):
                            continue
                        if block.get("type") != "tool_use":
                            continue
                        tool_call_id = block.get("id")
                        tool_name = block.get("name")
                        arguments = block.get("input")
                        if not isinstance(tool_call_id, str) or not tool_call_id.strip():
                            continue
                        if not isinstance(tool_name, str) or not tool_name.strip():
                            continue
                        if not isinstance(arguments, Mapping):
                            continue
                        yield LlmStreamToolCall(
                            tool_call_id=tool_call_id.strip(),
                            tool_name=tool_name.strip(),
                            arguments_json=dict(arguments),
                        )
                        continue

                    if typ == "message_start":
                        message = item.get("message")
                        if isinstance(message, Mapping):
                            raw_usage = message.get("usage")
                            if isinstance(raw_usage, Mapping):
                                usage = _merge_usage(usage, _usage_from_mapping(raw_usage))
                        continue

                    if typ == "message_delta":
                        raw_usage = item.get("usage")
                        if isinstance(raw_usage, Mapping):
                            usage = _merge_usage(usage, _usage_from_mapping(raw_usage))
                        continue

                    if typ == "message_stop":
                        yield LlmStreamRunCompleted(usage=usage)
                        return

                    if typ == "error":
                        error = item.get("error")
                        message = "Anthropic 返回错误"
                        details: dict[str, Any] = {}
                        if isinstance(error, Mapping):
                            msg = error.get("message")
                            if isinstance(msg, str) and msg.strip():
                                message = msg.strip()
                            typ = error.get("type")
                            if isinstance(typ, str) and typ.strip():
                                details["anthropic_error_type"] = typ.strip()
                        yield LlmStreamRunFailed(
                            error=LlmGatewayError(
                                error_class=ERROR_CLASS_PROVIDER_NON_RETRYABLE,
                                message=message,
                                details=details,
                            )
                        )
                        return
        except Exception as exc:
            if isinstance(exc, getattr(httpx, "TimeoutException", ())):
                raise TimeoutError from exc
            if isinstance(exc, getattr(httpx, "NetworkError", ())):
                error = LlmGatewayError(
                    error_class=ERROR_CLASS_PROVIDER_RETRYABLE,
                    message="Anthropic 网络错误",
                )
                yield LlmStreamRunFailed(error=error)
                return
            raise


def _import_httpx() -> Any:
    try:
        import httpx  # type: ignore[import-not-found]
    except ModuleNotFoundError as exc:  # pragma: no cover
        raise RuntimeError("缺少 httpx 依赖，请安装 requirements-dev.txt 或补齐 requirements.txt") from exc
    return httpx


__all__ = ["AnthropicGatewayConfig", "AnthropicLlmGateway"]
