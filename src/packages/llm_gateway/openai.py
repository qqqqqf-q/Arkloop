from __future__ import annotations

from dataclasses import dataclass, field
import json
import logging
import os
from typing import Any, AsyncIterator, Mapping, Optional, Sequence

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
    LlmStreamProviderFallback,
    LlmStreamRunCompleted,
    LlmStreamRunFailed,
    LlmUsage,
)
from .gateway import LlmGateway

_OPENAI_API_KEY_ENV = "ARKLOOP_OPENAI_API_KEY"
_OPENAI_BASE_URL_ENV = "ARKLOOP_OPENAI_BASE_URL"
_OPENAI_API_MODE_ENV = "ARKLOOP_OPENAI_API_MODE"
_OPENAI_TOTAL_TIMEOUT_SECONDS_ENV = "ARKLOOP_OPENAI_TOTAL_TIMEOUT_SECONDS"

_DEFAULT_OPENAI_BASE_URL = "https://api.openai.com/v1"
_DEFAULT_OPENAI_API_MODE = "auto"
_DEFAULT_OPENAI_TOTAL_TIMEOUT_SECONDS = 60.0

_SUPPORTED_OPENAI_API_MODES = ("auto", "responses", "chat_completions")
_MAX_ERROR_BODY_BYTES = 4096


def _parse_non_negative_float(value: str) -> float:
    cleaned = value.strip()
    parsed = float(cleaned)
    if parsed < 0:
        raise ValueError("必须为非负数")
    return parsed


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
        messages.append({"role": message.role, "content": text})
    return messages


def _to_openai_responses_input(request: LlmGatewayRequest) -> list[dict[str, Any]]:
    items: list[dict[str, Any]] = []
    for message in request.messages:
        text = "".join(part.text for part in message.content)
        items.append(
            {
                "role": message.role,
                "content": [{"type": "input_text", "text": text}],
            }
        )
    return items


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

        return cls(
            api_key=api_key,
            base_url=base_url,
            api_mode=api_mode,
            total_timeout_seconds=total_timeout_seconds,
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

        with anyio.fail_after(self._config.total_timeout_seconds):
            async for item in self._stream_http(
                httpx=httpx,
                path=path,
                payload=payload,
                headers=headers,
                api_mode=api_mode,
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
    ) -> AsyncIterator[LlmGatewayStreamEvent]:
        try:
            async with client.stream("POST", path, json=dict(payload), headers=dict(headers)) as resp:
                if resp.status_code != 200:
                    body = await resp.aread()
                    body = body[:_MAX_ERROR_BODY_BYTES]
                    error_json = _try_parse_json(body)
                    response_text = body.decode("utf-8", errors="replace")

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
                async for data in _aiter_sse_data(resp.aiter_lines()):
                    if data.strip() == "[DONE]":
                        yield LlmStreamRunCompleted(usage=usage)
                        return

                    item = _try_parse_json(data)
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

                    delta, usage_update = _chat_completions_delta_from_chunk(item)
                    if usage_update is not None:
                        usage = usage_update
                    if delta is not None:
                        yield delta
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


def _chat_completions_delta_from_chunk(
    chunk: Any,
) -> tuple[LlmStreamMessageDelta | None, LlmUsage | None]:
    if not isinstance(chunk, Mapping):
        return None, None

    usage = None
    raw_usage = chunk.get("usage")
    if isinstance(raw_usage, Mapping):
        usage = _usage_from_mapping(raw_usage)

    choices = chunk.get("choices")
    if not isinstance(choices, list) or not choices:
        return None, usage

    first = choices[0]
    if not isinstance(first, Mapping):
        return None, usage

    delta = first.get("delta")
    if not isinstance(delta, Mapping):
        return None, usage

    content = delta.get("content")
    if not isinstance(content, str) or not content:
        return None, usage

    role = delta.get("role")
    if not isinstance(role, str) or not role:
        role = "assistant"

    return LlmStreamMessageDelta(content_delta=content, role=role), usage


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


def _import_httpx() -> Any:
    try:
        import httpx  # type: ignore[import-not-found]
    except ModuleNotFoundError as exc:  # pragma: no cover
        raise RuntimeError("缺少 httpx 依赖，请安装 requirements-dev.txt 或补齐 requirements.txt") from exc
    return httpx


__all__ = ["OpenAiGatewayConfig", "OpenAiLlmGateway"]
