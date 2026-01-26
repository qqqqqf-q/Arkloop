from __future__ import annotations

import contextvars
from dataclasses import dataclass
import logging
import time
from typing import Awaitable, Callable, Optional

from fastapi import FastAPI, Request
from starlette.background import BackgroundTask, BackgroundTasks
from starlette.responses import Response

from .context import new_trace_id, normalize_trace_id, reset_trace_id, set_trace_id

TRACE_ID_HEADER = "X-Trace-Id"


@dataclass(frozen=True)
class TraceIdMiddlewareConfig:
    trust_incoming_trace_id: bool = False


def _choose_trace_id(request: Request, *, config: TraceIdMiddlewareConfig) -> str:
    if config.trust_incoming_trace_id:
        incoming = normalize_trace_id(request.headers.get(TRACE_ID_HEADER))
        if incoming is not None:
            return incoming
    return new_trace_id()


def _attach_trace_reset(response: Response, *, token: contextvars.Token[Optional[str]]) -> None:
    async def _reset() -> None:
        reset_trace_id(token)

    if response.background is None:
        response.background = BackgroundTask(_reset)
        return

    if isinstance(response.background, BackgroundTasks):
        response.background.add_task(_reset)
        return

    existing = response.background
    tasks = BackgroundTasks()
    tasks.add_task(existing.func, *existing.args, **existing.kwargs)
    tasks.add_task(_reset)
    response.background = tasks


def install_trace_id_middleware(
    app: FastAPI,
    *,
    config: Optional[TraceIdMiddlewareConfig] = None,
) -> None:
    cfg = config or TraceIdMiddlewareConfig()
    logger = logging.getLogger("arkloop.http")

    @app.middleware("http")
    async def _trace_id_middleware(
        request: Request,
        call_next: Callable[[Request], Awaitable[Response]],
    ) -> Response:
        start = time.perf_counter()
        trace_id = _choose_trace_id(request, config=cfg)
        token = set_trace_id(trace_id)
        request.state.trace_id = trace_id
        try:
            response = await call_next(request)
        except Exception:
            reset_trace_id(token)
            raise
        _attach_trace_reset(response, token=token)
        response.headers[TRACE_ID_HEADER] = trace_id
        duration_ms = int((time.perf_counter() - start) * 1000)
        logger.info(
            "http request",
            extra={
                "method": request.method,
                "path": request.url.path,
                "status_code": response.status_code,
                "duration_ms": duration_ms,
            },
        )
        return response
