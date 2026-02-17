from __future__ import annotations

from dataclasses import dataclass, field
import json
import logging
import os
from typing import Any, AsyncIterator, Mapping, Optional, Sequence
import uuid

import anyio

from packages.config import load_dotenv_if_enabled

from .contract import (
    ERROR_CLASS_INTERNAL_ERROR,
    ERROR_CLASS_PROVIDER_NON_RETRYABLE,
    ERROR_CLASS_PROVIDER_RETRYABLE,
    LlmGatewayError,
    LlmGatewayRequest,
    LlmGatewayStreamEvent,
    LlmStreamLlmRequest,
    LlmStreamLlmResponseChunk,
    LlmStreamMessageDelta,
    LlmStreamProviderFallback,
    LlmStreamRunCompleted,
    LlmStreamRunFailed,
    LlmStreamToolCall,
    ToolSpec,
    LlmUsage,
)
from .gateway import LlmGateway

_OPENAI_API_KEY_ENV = "ARKLOOP_OPENAI_API_KEY"
_OPENAI_BASE_URL_ENV = "ARKLOOP_OPENAI_BASE_URL"
_OPENAI_API_MODE_ENV = "ARKLOOP_OPENAI_API_MODE"
_OPENAI_TOTAL_TIMEOUT_SECONDS_ENV = "ARKLOOP_OPENAI_TOTAL_TIMEOUT_SECONDS"
_LLM_DEBUG_EVENTS_ENV = "ARKLOOP_LLM_DEBUG_EVENTS"

_DEFAULT_OPENAI_BASE_URL = "https://api.openai.com/v1"
_DEFAULT_OPENAI_API_MODE = "auto"
_DEFAULT_OPENAI_TOTAL_TIMEOUT_SECONDS = 60.0

_SUPPORTED_OPENAI_API_MODES = ("auto", "responses", "chat_completions")
_MAX_ERROR_BODY_BYTES = 4096
_MAX_DEBUG_CHUNK_BYTES = 8192

_TRUTHY = {"1", "true", "yes", "y", "on"}
_FALSY = {"0", "false", "no", "n", "off"}


def _parse_non_negative_float(value: str) -> float:
    cleaned = value.strip()
    parsed = float(cleaned)
    if parsed < 0:
        raise ValueError("必须为非负数")
    return parsed


def _parse_bool(value: str) -> bool:
    cleaned = value.strip().casefold()
    if cleaned in _TRUTHY:
        return True
    if cleaned in _FALSY:
        return False
    raise ValueError("必须为布尔值（0/1、true/false）")


def _parse_openai_api_mode(value: str) -> str:
    cleaned = value.strip().casefold()
    if cleaned in _SUPPORTED_OPENAI_API_MODES:
        return cleaned
    raise ValueError(f"openai_api_mode 必须为 {', '.join(_SUPPORTED_OPENAI_API_MODES)}")


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


def _truncate_utf8(value: str, *, max_bytes: int) -> tuple[str, bool]:
    raw = value.encode("utf-8")
    if len(raw) <= max_bytes:
        return value, False
    truncated = raw[:max_bytes]
    while truncated and (truncated[-1] & 0xC0) == 0x80:
        truncated = truncated[:-1]
    return truncated.decode("utf-8", errors="ignore"), True


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

    input_tokens = _maybe_int(data.get("input_tokens") or data.get("prompt_tokens"))
    output_tokens = _maybe_int(data.get("output_tokens") or data.get("completion_tokens"))
    total_tokens = _maybe_int(data.get("total_tokens") or data.get("total"))
    if input_tokens is None and output_tokens is None and total_tokens is None:
        return None
    return LlmUsage(input_tokens=input_tokens, output_tokens=output_tokens, total_tokens=total_tokens)


def _provider_error_class_from_status(status_code: int) -> str:
    if status_code in {408, 425, 429}:
        return ERROR_CLASS_PROVIDER_RETRYABLE
    if 500 <= status_code <= 599:
        return ERROR_CLASS_PROVIDER_RETRYABLE
    return ERROR_CLASS_PROVIDER_NON_RETRYABLE


def _openai_error_message(error_json: Any | None, *, fallback: str) -> str:
    if isinstance(error_json, Mapping):
        error = error_json.get("error")
        if isinstance(error, Mapping):
            message = error.get("message")
            if isinstance(message, str) and message.strip():
                return message.strip()
    return fallback


def _openai_error_details(error_json: Any | None, *, status_code: int) -> dict[str, Any]:
    details: dict[str, Any] = {"status_code": int(status_code)}
    if not isinstance(error_json, Mapping):
        return details
    error = error_json.get("error")
    if not isinstance(error, Mapping):
        return details

    for key in ("type", "code", "param"):
        value = error.get(key)
        if value is None:
            continue
        if isinstance(value, (str, int, float, bool)):
            details[f"openai_error_{key}"] = value
            continue
        details[f"openai_error_{key}"] = str(value)
    return details


def _is_responses_endpoint_not_supported(
    *, status_code: int, error_json: Any | None, response_text: str | None
) -> bool:
    if status_code in {404, 405}:
        return True

    if status_code != 400:
        return False

    message = _openai_error_message(error_json, fallback="")
    joined = " ".join(part for part in (message, response_text) if part)
    lowered = joined.casefold()
    if "responses" in lowered and ("unknown" in lowered or "not found" in lowered):
        return True
    return False


def _to_openai_chat_messages(request: LlmGatewayRequest) -> list[dict[str, Any]]:
    messages: list[dict[str, Any]] = []
    for message in request.messages:
        text = "".join(part.text for part in message.content)
        if message.role == "assistant" and message.tool_calls:
            payload: dict[str, Any] = {
                "role": "assistant",
                "content": text,
                "tool_calls": [
                    {
                        "id": tool_call.tool_call_id,
                        "type": "function",
                        "function": {
                            "name": tool_call.tool_name,
                            "arguments": json.dumps(
                                dict(tool_call.arguments_json),
                                ensure_ascii=False,
                                separators=(",", ":"),
                                sort_keys=True,
                            ),
                        },
                    }
                    for tool_call in message.tool_calls
                ],
            }
            messages.append(payload)
            continue

        if message.role == "tool":
            parsed = _try_parse_json(text)
            if isinstance(parsed, Mapping):
                tool_call_id = parsed.get("tool_call_id")
                if isinstance(tool_call_id, str) and tool_call_id.strip():
                    messages.append(
                        {
                            "role": "tool",
                            "tool_call_id": tool_call_id.strip(),
                            "content": _tool_output_text_from_envelope(parsed),
                        }
                    )
                    continue
            messages.append({"role": "tool", "content": text})
            continue

        messages.append({"role": message.role, "content": text})
    return messages


def _to_openai_responses_input(request: LlmGatewayRequest) -> list[dict[str, Any]]:
    items: list[dict[str, Any]] = []
    for message in request.messages:
        text = "".join(part.text for part in message.content)
        content_type = "output_text" if message.role == "assistant" else "input_text"
        if message.role == "assistant" and message.tool_calls:
            if text:
                items.append(
                    {
                        "role": "assistant",
                        "content": [{"type": content_type, "text": text}],
                    }
                )
            for tool_call in message.tool_calls:
                items.append(
                    {
                        "type": "function_call",
                        "call_id": tool_call.tool_call_id,
                        "name": tool_call.tool_name,
                        "arguments": json.dumps(
                            dict(tool_call.arguments_json),
                            ensure_ascii=False,
                            separators=(",", ":"),
                            sort_keys=True,
                        ),
                    }
                )
            continue

        if message.role == "tool":
            parsed = _try_parse_json(text)
            if not isinstance(parsed, Mapping):
                raise ValueError("tool message 不是合法 JSON")
            tool_call_id = parsed.get("tool_call_id")
            if not isinstance(tool_call_id, str) or not tool_call_id.strip():
                raise ValueError("tool message 缺少 tool_call_id")
            items.append(
                {
                    "type": "function_call_output",
                    "call_id": tool_call_id.strip(),
                    "output": _tool_output_text_from_envelope(parsed),
                }
            )
            continue

        items.append(
            {
                "role": message.role,
                "content": [{"type": content_type, "text": text}],
            }
        )
    return items


def _tool_output_text_from_envelope(envelope: Mapping[str, Any]) -> str:
    result = envelope.get("result")
    if result is not None:
        return json.dumps(result, ensure_ascii=False, separators=(",", ":"), sort_keys=True)

    error = envelope.get("error")
    if error is not None:
        return json.dumps({"error": error}, ensure_ascii=False, separators=(",", ":"), sort_keys=True)

    return json.dumps(dict(envelope), ensure_ascii=False, separators=(",", ":"), sort_keys=True)


def _to_openai_tools(*, specs: list[ToolSpec], api_mode: str) -> list[dict[str, Any]]:
    tools: list[dict[str, Any]] = []
    for spec in specs:
        schema = dict(spec.json_schema)
        if api_mode == "responses":
            payload: dict[str, Any] = {"type": "function", "name": spec.name, "parameters": schema}
            if spec.description is not None:
                payload["description"] = spec.description
            tools.append(payload)
            continue

        function: dict[str, Any] = {"name": spec.name, "parameters": schema}
        if spec.description is not None:
            function["description"] = spec.description
        tools.append({"type": "function", "function": function})
    return tools


async def _aiter_sse_data(lines: AsyncIterator[str]) -> AsyncIterator[str]:
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
        if not line.startswith("data:"):
            continue
        data_lines.append(line[len("data:") :].lstrip())

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


class _ResponsesNotSupportedError(RuntimeError):
    def __init__(self, *, status_code: int, message: str) -> None:
        super().__init__(message)
        self.status_code = status_code


@dataclass(frozen=True, slots=True)
class OpenAiGatewayConfig:
    api_key: str = field(repr=False)
    base_url: str = _DEFAULT_OPENAI_BASE_URL
    api_mode: str = _DEFAULT_OPENAI_API_MODE
    total_timeout_seconds: float = _DEFAULT_OPENAI_TOTAL_TIMEOUT_SECONDS
    emit_llm_debug_events: bool = False

    @classmethod
    def from_env(cls, *, required: bool = False) -> Optional["OpenAiGatewayConfig"]:
        load_dotenv_if_enabled(override=False)

        api_key = (os.getenv(_OPENAI_API_KEY_ENV) or "").strip()
        if not api_key:
            if required:
                raise ValueError(f"缺少环境变量 {_OPENAI_API_KEY_ENV}")
            return None

        base_url = _DEFAULT_OPENAI_BASE_URL
        raw_base_url = os.getenv(_OPENAI_BASE_URL_ENV)
        if raw_base_url:
            base_url = _normalize_base_url(raw_base_url)

        api_mode = _DEFAULT_OPENAI_API_MODE
        raw_api_mode = os.getenv(_OPENAI_API_MODE_ENV)
        if raw_api_mode:
            api_mode = _parse_openai_api_mode(raw_api_mode)

        total_timeout_seconds = _DEFAULT_OPENAI_TOTAL_TIMEOUT_SECONDS
        raw_timeout = os.getenv(_OPENAI_TOTAL_TIMEOUT_SECONDS_ENV)
        if raw_timeout:
            total_timeout_seconds = _parse_non_negative_float(raw_timeout)

        emit_llm_debug_events = False
        raw_debug = os.getenv(_LLM_DEBUG_EVENTS_ENV)
        if raw_debug:
            emit_llm_debug_events = _parse_bool(raw_debug)

        return cls(
            api_key=api_key,
            base_url=base_url,
            api_mode=api_mode,
            total_timeout_seconds=total_timeout_seconds,
            emit_llm_debug_events=emit_llm_debug_events,
        )


class OpenAiLlmGateway(LlmGateway):
    def __init__(self, *, config: OpenAiGatewayConfig, client: Any | None = None) -> None:
        self._config = config
        self._client = client
        self._logger = logging.getLogger("arkloop.llm_gateway.openai")

    async def stream(self, *, request: LlmGatewayRequest) -> AsyncIterator[LlmGatewayStreamEvent]:
        mode_sequence = self._mode_sequence()
        for index, mode in enumerate(mode_sequence):
            try:
                async for item in self._stream_once(request=request, api_mode=mode):
                    yield item
                return
            except _ResponsesNotSupportedError as exc:
                if self._config.api_mode != "auto" or mode != "responses":
                    error = LlmGatewayError(
                        error_class=ERROR_CLASS_PROVIDER_NON_RETRYABLE,
                        message=str(exc),
                        details={"status_code": exc.status_code},
                    )
                    yield LlmStreamRunFailed(error=error)
                    return

                fallback = LlmStreamProviderFallback(
                    provider_kind="openai",
                    from_api_mode="responses",
                    to_api_mode="chat_completions",
                    reason="responses_endpoint_not_supported",
                    status_code=exc.status_code,
                )
                self._logger.info(
                    "OpenAI responses 不可用，回退到 chat_completions",
                    extra={"status_code": exc.status_code, "base_url": self._config.base_url},
                )
                yield fallback
                if index == len(mode_sequence) - 1:
                    error = LlmGatewayError(
                        error_class=ERROR_CLASS_PROVIDER_NON_RETRYABLE,
                        message="OpenAI responses 不可用，且没有可回退的 API 模式",
                        details={"status_code": exc.status_code},
                    )
                    yield LlmStreamRunFailed(error=error)
                    return
                continue
            except TimeoutError:
                error = LlmGatewayError(
                    error_class=ERROR_CLASS_PROVIDER_RETRYABLE,
                    message="OpenAI 请求超时",
                )
                yield LlmStreamRunFailed(error=error)
                return
            except Exception:
                self._logger.exception("OpenAI 流式请求异常")
                error = LlmGatewayError(
                    error_class=ERROR_CLASS_INTERNAL_ERROR,
                    message="OpenAI 流式请求异常",
                )
                yield LlmStreamRunFailed(error=error)
                return

    def _mode_sequence(self) -> Sequence[str]:
        mode = self._config.api_mode
        if mode == "auto":
            return ("responses", "chat_completions")
        return (mode,)

    async def _stream_once(
        self, *, request: LlmGatewayRequest, api_mode: str
    ) -> AsyncIterator[LlmGatewayStreamEvent]:
        httpx = _import_httpx()

        llm_call_id = uuid.uuid4().hex

        headers = {
            "Authorization": f"Bearer {self._config.api_key}",
            "Accept": "text/event-stream",
        }

        path = "/responses" if api_mode == "responses" else "/chat/completions"
        payload: dict[str, Any] = {"model": request.model, "stream": True}

        if request.temperature is not None:
            payload["temperature"] = float(request.temperature)

        if api_mode == "responses":
            payload["input"] = _to_openai_responses_input(request)
            if request.max_output_tokens is not None:
                payload["max_output_tokens"] = int(request.max_output_tokens)
        else:
            payload["messages"] = _to_openai_chat_messages(request)
            if request.max_output_tokens is not None:
                payload["max_tokens"] = int(request.max_output_tokens)

        if request.tools:
            payload["tools"] = _to_openai_tools(specs=request.tools, api_mode=api_mode)
            payload["tool_choice"] = "auto"

        if self._config.emit_llm_debug_events:
            yield LlmStreamLlmRequest(
                llm_call_id=llm_call_id,
                provider_kind="openai",
                api_mode=api_mode,
                base_url=self._config.base_url,
                path=path,
                payload_json=dict(payload),
            )

        stream = self._stream_http(
            httpx=httpx,
            path=path,
            payload=payload,
            headers=headers,
            api_mode=api_mode,
            llm_call_id=llm_call_id,
        )
        async for item in _iter_with_total_timeout(
            stream, total_timeout_seconds=self._config.total_timeout_seconds
        ):
            yield item

    async def _stream_http(
        self,
        *,
        httpx: Any,
        path: str,
        payload: Mapping[str, Any],
        headers: Mapping[str, str],
        api_mode: str,
        llm_call_id: str,
    ) -> AsyncIterator[LlmGatewayStreamEvent]:
        client = self._client
        if client is None:
            timeout = httpx.Timeout(None)
            async with httpx.AsyncClient(base_url=self._config.base_url, timeout=timeout) as client:
                async for item in self._stream_http_with_client(
                    httpx=httpx,
                    client=client,
                    path=path,
                    payload=payload,
                    headers=headers,
                    api_mode=api_mode,
                    llm_call_id=llm_call_id,
                ):
                    yield item
            return

        async for item in self._stream_http_with_client(
            httpx=httpx,
            client=client,
            path=path,
            payload=payload,
            headers=headers,
            api_mode=api_mode,
            llm_call_id=llm_call_id,
        ):
            yield item

    async def _stream_http_with_client(
        self,
        *,
        httpx: Any,
        client: Any,
        path: str,
        payload: Mapping[str, Any],
        headers: Mapping[str, str],
        api_mode: str,
        llm_call_id: str,
    ) -> AsyncIterator[LlmGatewayStreamEvent]:
        try:
            async with client.stream("POST", path, json=dict(payload), headers=dict(headers)) as resp:
                if resp.status_code != 200:
                    body = await resp.aread()
                    body = body[:_MAX_ERROR_BODY_BYTES]
                    error_json = _try_parse_json(body)
                    response_text = body.decode("utf-8", errors="replace")

                    if self._config.emit_llm_debug_events:
                        raw, truncated = _truncate_utf8(
                            response_text,
                            max_bytes=_MAX_DEBUG_CHUNK_BYTES,
                        )
                        yield LlmStreamLlmResponseChunk(
                            llm_call_id=llm_call_id,
                            provider_kind="openai",
                            api_mode=api_mode,
                            raw=raw,
                            chunk_json=error_json,
                            status_code=int(resp.status_code),
                            truncated=truncated,
                        )

                    if api_mode == "responses" and self._config.api_mode == "auto":
                        if _is_responses_endpoint_not_supported(
                            status_code=resp.status_code,
                            error_json=error_json,
                            response_text=response_text,
                        ):
                            raise _ResponsesNotSupportedError(
                                status_code=resp.status_code,
                                message="OpenAI responses 端点不可用",
                            )

                    error_class = _provider_error_class_from_status(int(resp.status_code))
                    message = _openai_error_message(
                        error_json,
                        fallback=f"OpenAI 返回 {resp.status_code}",
                    )
                    details = _openai_error_details(error_json, status_code=int(resp.status_code))
                    yield LlmStreamRunFailed(
                        error=LlmGatewayError(
                            error_class=error_class,
                            message=message,
                            details=details,
                        )
                    )
                    return

                usage: LlmUsage | None = None
                tool_call_buffer = _OpenAiChatToolCallBuffer()
                async for data in _aiter_sse_data(resp.aiter_lines()):
                    item = _try_parse_json(data)
                    if self._config.emit_llm_debug_events:
                        raw, truncated = _truncate_utf8(data, max_bytes=_MAX_DEBUG_CHUNK_BYTES)
                        yield LlmStreamLlmResponseChunk(
                            llm_call_id=llm_call_id,
                            provider_kind="openai",
                            api_mode=api_mode,
                            raw=raw,
                            chunk_json=item,
                            truncated=truncated,
                        )

                    if data.strip() == "[DONE]":
                        if api_mode != "responses":
                            try:
                                for tool_call in tool_call_buffer.drain():
                                    yield tool_call
                            except ValueError as exc:
                                yield LlmStreamRunFailed(
                                    error=LlmGatewayError(
                                        error_class=ERROR_CLASS_PROVIDER_NON_RETRYABLE,
                                        message="OpenAI tool_call 参数解析失败",
                                        details={"reason": str(exc)},
                                    )
                                )
                                return
                        yield LlmStreamRunCompleted(usage=usage)
                        return

                    if item is None:
                        continue

                    if api_mode == "responses":
                        async for event in _events_from_openai_responses_stream_event(item):
                            if isinstance(event, LlmStreamRunCompleted):
                                yield event
                                return
                            if isinstance(event, LlmStreamRunFailed):
                                yield event
                                return
                            yield event
                        continue

                    tool_call_buffer.update_from_chunk(item)
                    delta, usage_update, should_flush_tool_calls = _chat_completions_delta_from_chunk(item)
                    if usage_update is not None:
                        usage = usage_update
                    if delta is not None:
                        yield delta
                    if should_flush_tool_calls:
                        try:
                            for tool_call in tool_call_buffer.drain():
                                yield tool_call
                        except ValueError as exc:
                            yield LlmStreamRunFailed(
                                error=LlmGatewayError(
                                    error_class=ERROR_CLASS_PROVIDER_NON_RETRYABLE,
                                    message="OpenAI tool_call 参数解析失败",
                                    details={"reason": str(exc)},
                                )
                            )
                            return
        except _ResponsesNotSupportedError:
            raise
        except Exception as exc:
            if isinstance(exc, getattr(httpx, "TimeoutException", ())):
                raise TimeoutError from exc
            if isinstance(exc, getattr(httpx, "NetworkError", ())):
                error = LlmGatewayError(
                    error_class=ERROR_CLASS_PROVIDER_RETRYABLE,
                    message="OpenAI 网络错误",
                )
                yield LlmStreamRunFailed(error=error)
                return
            raise


@dataclass(slots=True)
class _OpenAiChatToolCallParts:
    tool_call_id: str | None = None
    tool_name: str | None = None
    arguments_parts: list[str] = field(default_factory=list)


class _OpenAiChatToolCallBuffer:
    def __init__(self) -> None:
        self._calls: dict[int, _OpenAiChatToolCallParts] = {}
        self._emitted: set[int] = set()

    def update_from_chunk(self, chunk: Any) -> None:
        if not isinstance(chunk, Mapping):
            return
        choices = chunk.get("choices")
        if not isinstance(choices, list) or not choices:
            return
        first = choices[0]
        if not isinstance(first, Mapping):
            return
        delta = first.get("delta")
        if not isinstance(delta, Mapping):
            return
        tool_calls = delta.get("tool_calls")
        if not isinstance(tool_calls, list) or not tool_calls:
            return
        for raw in tool_calls:
            if not isinstance(raw, Mapping):
                continue
            index = raw.get("index")
            if not isinstance(index, int):
                continue
            parts = self._calls.setdefault(index, _OpenAiChatToolCallParts())

            tool_call_id = raw.get("id")
            if isinstance(tool_call_id, str) and tool_call_id.strip():
                parts.tool_call_id = tool_call_id.strip()

            function = raw.get("function")
            if not isinstance(function, Mapping):
                continue
            tool_name = function.get("name")
            if isinstance(tool_name, str) and tool_name.strip():
                parts.tool_name = tool_name.strip()
            arguments = function.get("arguments")
            if isinstance(arguments, str) and arguments:
                parts.arguments_parts.append(arguments)

    def drain(self) -> list[LlmStreamToolCall]:
        emitted: list[LlmStreamToolCall] = []
        invalid: list[tuple[int, list[str]]] = []
        for index in sorted(self._calls.keys()):
            if index in self._emitted:
                continue
            parts = self._calls[index]
            tool_call_id = (parts.tool_call_id or "").strip()
            tool_name = (parts.tool_name or "").strip()
            if not tool_call_id or not tool_name:
                missing: list[str] = []
                if not tool_call_id:
                    missing.append("id")
                if not tool_name:
                    missing.append("function.name")
                invalid.append((index, missing))
                self._emitted.add(index)
                continue
            arguments_json = _parse_json_object("".join(parts.arguments_parts))
            emitted.append(
                LlmStreamToolCall(
                    tool_call_id=tool_call_id,
                    tool_name=tool_name,
                    arguments_json=arguments_json,
                )
            )
            self._emitted.add(index)

        for index in self._emitted:
            self._calls.pop(index, None)
        self._emitted.clear()

        if invalid:
            rendered = ", ".join(
                f"tool_calls[{index}] 缺少 {', '.join(missing)}" for index, missing in invalid[:3]
            )
            if len(invalid) > 3:
                rendered = f"{rendered} 等"
            raise ValueError(rendered)
        return emitted


def _parse_json_object(value: str) -> Mapping[str, Any]:
    cleaned = value.strip()
    if not cleaned:
        return {}
    parsed = json.loads(cleaned)
    if not isinstance(parsed, Mapping):
        raise ValueError("arguments 必须是 JSON object")
    return dict(parsed)


def _chat_completions_delta_from_chunk(
    chunk: Any,
) -> tuple[LlmStreamMessageDelta | None, LlmUsage | None, bool]:
    if not isinstance(chunk, Mapping):
        return None, None, False

    usage = None
    raw_usage = chunk.get("usage")
    if isinstance(raw_usage, Mapping):
        usage = _usage_from_mapping(raw_usage)

    choices = chunk.get("choices")
    if not isinstance(choices, list) or not choices:
        return None, usage, False

    first = choices[0]
    if not isinstance(first, Mapping):
        return None, usage, False

    delta = first.get("delta")
    if not isinstance(delta, Mapping):
        return None, usage, False

    finish_reason = first.get("finish_reason")
    should_flush_tool_calls = (
        isinstance(finish_reason, str) and finish_reason.strip().casefold() == "tool_calls"
    )

    content = delta.get("content")
    if not isinstance(content, str) or not content:
        return None, usage, should_flush_tool_calls

    role = delta.get("role")
    if not isinstance(role, str) or not role:
        role = "assistant"

    return LlmStreamMessageDelta(content_delta=content, role=role), usage, should_flush_tool_calls


async def _events_from_openai_responses_stream_event(
    event: Any,
) -> AsyncIterator[LlmGatewayStreamEvent]:
    if not isinstance(event, Mapping):
        return

    raw_type = event.get("type")
    typ = raw_type if isinstance(raw_type, str) else ""

    if typ.endswith(".delta"):
        delta = event.get("delta")
        if isinstance(delta, str) and delta:
            yield LlmStreamMessageDelta(content_delta=delta, role="assistant")
        return

    if typ == "response.completed":
        response = event.get("response")
        usage = None
        if isinstance(response, Mapping):
            raw_usage = response.get("usage")
            if isinstance(raw_usage, Mapping):
                usage = _usage_from_mapping(raw_usage)
            output = response.get("output")
            if isinstance(output, list):
                try:
                    for tool_call in _tool_calls_from_openai_responses_output(output):
                        yield tool_call
                except ValueError as exc:
                    yield LlmStreamRunFailed(
                        error=LlmGatewayError(
                            error_class=ERROR_CLASS_PROVIDER_NON_RETRYABLE,
                            message="OpenAI responses tool_call 参数解析失败",
                            details={"reason": str(exc)},
                        )
                    )
                    return
        yield LlmStreamRunCompleted(usage=usage)
        return

    if typ in {"response.failed", "response.error"}:
        error = event.get("error")
        message = "OpenAI responses 失败"
        if isinstance(error, Mapping):
            msg = error.get("message")
            if isinstance(msg, str) and msg.strip():
                message = msg.strip()
        yield LlmStreamRunFailed(
            error=LlmGatewayError(
                error_class=ERROR_CLASS_PROVIDER_NON_RETRYABLE,
                message=message,
            )
        )
        return

    embedded_error = event.get("error")
    if isinstance(embedded_error, Mapping):
        msg = embedded_error.get("message")
        message = msg.strip() if isinstance(msg, str) and msg.strip() else "OpenAI responses 返回错误"
        yield LlmStreamRunFailed(
            error=LlmGatewayError(
                error_class=ERROR_CLASS_PROVIDER_NON_RETRYABLE,
                message=message,
            )
        )


def _tool_calls_from_openai_responses_output(output: list[Any]) -> list[LlmStreamToolCall]:
    tool_calls: list[LlmStreamToolCall] = []
    for raw in output:
        if not isinstance(raw, Mapping):
            continue
        if raw.get("type") != "function_call":
            continue

        tool_call_id = raw.get("id") or raw.get("call_id")
        tool_name = raw.get("name") or raw.get("tool_name")
        if not isinstance(tool_call_id, str) or not tool_call_id.strip():
            raise ValueError("缺少 function_call.id")
        if not isinstance(tool_name, str) or not tool_name.strip():
            raise ValueError("缺少 function_call.name")

        arguments = raw.get("arguments")
        if arguments is None:
            arguments_json: Mapping[str, Any] = {}
        elif isinstance(arguments, Mapping):
            arguments_json = dict(arguments)
        elif isinstance(arguments, str):
            arguments_json = _parse_json_object(arguments)
        else:
            raise ValueError("function_call.arguments 类型不支持")

        tool_calls.append(
            LlmStreamToolCall(
                tool_call_id=tool_call_id.strip(),
                tool_name=tool_name.strip(),
                arguments_json=arguments_json,
            )
        )
    return tool_calls


def _import_httpx() -> Any:
    try:
        import httpx  # type: ignore[import-not-found]
    except ModuleNotFoundError as exc:  # pragma: no cover
        raise RuntimeError("缺少 httpx 依赖，请安装 requirements-dev.txt 或补齐 requirements.txt") from exc
    return httpx


__all__ = ["OpenAiGatewayConfig", "OpenAiLlmGateway"]
