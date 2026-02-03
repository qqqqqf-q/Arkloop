from __future__ import annotations

import json
from typing import Any, AsyncIterator, Mapping
from urllib.parse import urlsplit

from .errors import ArkloopApiError, ArkloopClientTransportError
from .sse import SseParser

try:
    import httpx
except Exception as exc:  # pragma: no cover
    raise RuntimeError(
        "缺少 httpx 依赖，请安装 requirements-dev.txt 或补齐 requirements.txt"
    ) from exc


def _as_str(value: object, *, label: str) -> str:
    if not isinstance(value, str):
        raise TypeError(f"{label} 必须为字符串")
    cleaned = value.strip()
    if not cleaned:
        raise ValueError(f"{label} 不能为空")
    return cleaned


def _as_mapping(value: object, *, label: str) -> Mapping[str, Any]:
    if not isinstance(value, Mapping):
        raise TypeError(f"{label} 必须为对象")
    return value


def _is_loopback_host(hostname: str | None) -> bool:
    if hostname is None:
        return False
    normalized = hostname.strip().casefold()
    if normalized in {"localhost", "::1"}:
        return True
    return normalized.startswith("127.")


class ArkloopClient:
    def __init__(
        self,
        *,
        base_url: str,
        http: httpx.AsyncClient | None = None,
        trust_env: bool | None = None,
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._client = http
        self._owns_client = http is None
        if trust_env is None:
            hostname = urlsplit(self._base_url).hostname
            self._trust_env = not _is_loopback_host(hostname)
        else:
            self._trust_env = bool(trust_env)

    async def __aenter__(self) -> "ArkloopClient":
        if self._client is None:
            self._client = httpx.AsyncClient(base_url=self._base_url, trust_env=self._trust_env)
        return self

    async def __aexit__(self, exc_type, exc, tb) -> None:
        if self._owns_client and self._client is not None:
            await self._client.aclose()
        self._client = None

    async def login(self, *, login: str, password: str) -> str:
        payload = {"login": login, "password": password}
        data = await self._request_json("POST", "/v1/auth/login", json_body=payload)
        token = _as_str(data.get("access_token"), label="access_token")
        return token

    async def create_thread(self, *, token: str, title: str | None = None) -> str:
        payload: dict[str, object] = {}
        if title is not None:
            payload["title"] = title
        data = await self._request_json("POST", "/v1/threads", token=token, json_body=payload)
        thread_id = _as_str(data.get("id"), label="thread.id")
        return thread_id

    async def create_message(self, *, token: str, thread_id: str, content: str) -> str:
        payload = {"content": content}
        data = await self._request_json(
            "POST",
            f"/v1/threads/{thread_id}/messages",
            token=token,
            json_body=payload,
        )
        message_id = _as_str(data.get("id"), label="message.id")
        return message_id

    async def create_run(
        self,
        *,
        token: str,
        thread_id: str,
        route_id: str | None = None,
    ) -> tuple[str, str]:
        payload: dict[str, object] = {}
        if route_id is not None and route_id.strip():
            payload["route_id"] = route_id.strip()

        body: dict[str, object] | None = payload if payload else None
        data = await self._request_json(
            "POST",
            f"/v1/threads/{thread_id}/runs",
            token=token,
            json_body=body,
        )
        run_id = _as_str(data.get("run_id"), label="run_id")
        trace_id = _as_str(data.get("trace_id"), label="trace_id")
        return run_id, trace_id

    async def stream_run_events_once(
        self,
        *,
        token: str,
        run_id: str,
        after_seq: int = 0,
        follow: bool = True,
    ) -> AsyncIterator[Mapping[str, Any]]:
        client = self._require_client()
        headers = {"Authorization": f"Bearer {token}"}
        params = {"after_seq": int(after_seq), "follow": "1" if follow else "0"}
        parser = SseParser()

        try:
            async with client.stream(
                "GET",
                f"/v1/runs/{run_id}/events",
                headers=headers,
                params=params,
                timeout=httpx.Timeout(None),
            ) as resp:
                await self._raise_for_status(resp)
                async for chunk in resp.aiter_bytes():
                    for sse in parser.feed(chunk):
                        yield self._parse_event_data(sse.data)
        except ArkloopApiError:
            raise
        except Exception as exc:
            raise ArkloopClientTransportError(message="SSE 连接失败", detail=str(exc)) from exc

    def _parse_event_data(self, raw: str) -> Mapping[str, Any]:
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise ArkloopClientTransportError(
                message="SSE data 不是合法 JSON", detail=str(exc)
            ) from exc
        return _as_mapping(parsed, label="run_event")

    async def _request_json(
        self,
        method: str,
        path: str,
        *,
        token: str | None = None,
        json_body: object | None = None,
    ) -> Mapping[str, Any]:
        client = self._require_client()
        headers: dict[str, str] = {}
        if token is not None:
            headers["Authorization"] = f"Bearer {token}"

        try:
            resp = await client.request(method, path, headers=headers, json=json_body)
        except Exception as exc:
            raise ArkloopClientTransportError(message="HTTP 请求失败", detail=str(exc)) from exc

        await self._raise_for_status(resp)
        try:
            payload = resp.json()
        except Exception as exc:
            raise ArkloopClientTransportError(
                message="HTTP 响应不是合法 JSON", detail=str(exc)
            ) from exc
        return _as_mapping(payload, label="response")

    async def _raise_for_status(self, resp: httpx.Response) -> None:
        if 200 <= resp.status_code < 300:
            return
        text = None
        try:
            text = resp.text
        except Exception:
            text = None

        trace_id = resp.headers.get("X-Trace-Id")
        try:
            data = resp.json()
        except Exception:
            raise ArkloopApiError(
                status_code=resp.status_code,
                code="http_error",
                message=f"HTTP {resp.status_code}",
                trace_id=trace_id,
                response_text=text,
            )

        if isinstance(data, dict):
            code = data.get("code")
            message = data.get("message")
            details = data.get("details")
            trace_id = data.get("trace_id") or trace_id
            if isinstance(code, str) and isinstance(message, str):
                raise ArkloopApiError(
                    status_code=resp.status_code,
                    code=code,
                    message=message,
                    trace_id=str(trace_id) if isinstance(trace_id, str) else None,
                    details=details,
                    response_text=text,
                )

        raise ArkloopApiError(
            status_code=resp.status_code,
            code="http_error",
            message=f"HTTP {resp.status_code}",
            trace_id=trace_id,
            details=data,
            response_text=text,
        )

    def _require_client(self) -> httpx.AsyncClient:
        if self._client is None:
            raise RuntimeError("ArkloopClient 未初始化：请使用 async with ArkloopClient(...)")
        return self._client


__all__ = ["ArkloopClient"]
