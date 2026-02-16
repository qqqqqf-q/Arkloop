from __future__ import annotations

from collections.abc import AsyncIterator, Mapping
from contextlib import asynccontextmanager
import json
import logging
import os

from fastapi import FastAPI, HTTPException, Request
from starlette.responses import JSONResponse, Response

from packages.config import load_dotenv_if_enabled
from packages.data import Database, DatabaseConfig
from packages.mcp.pool import close_default_mcp_stdio_client_pool
from packages.observability.logging import configure_json_logging
from services.worker.composition import create_worker

_TOKEN_ENV = "ARKLOOP_WORKER_BRIDGE_TOKEN"


def configure_logging() -> None:
    configure_json_logging(component="worker_bridge")


def _load_bridge_token() -> str:
    raw = (os.getenv(_TOKEN_ENV) or "").strip()
    if not raw:
        raise RuntimeError(f"缺少环境变量 {_TOKEN_ENV}")
    return raw


def _extract_bearer_token(raw: str | None) -> str | None:
    if raw is None:
        return None
    cleaned = raw.strip()
    if not cleaned:
        return None
    prefix = "bearer "
    lowered = cleaned.casefold()
    if not lowered.startswith(prefix):
        return None
    return cleaned[len(prefix) :].strip() or None


def _validate_token(request: Request) -> None:
    expected = getattr(request.app.state, "bridge_token", None)
    if not isinstance(expected, str) or not expected:
        raise RuntimeError("bridge_token 未配置")

    token = _extract_bearer_token(request.headers.get("authorization"))
    if token is None or token != expected:
        raise HTTPException(status_code=401, detail="unauthorized")


@asynccontextmanager
async def _lifespan(app: FastAPI) -> AsyncIterator[None]:
    database_config = DatabaseConfig.from_env(required=True, allow_fallback=False)
    database = Database.from_config(database_config)
    worker = await create_worker(database=database)
    app.state.database = database
    app.state.worker = worker
    try:
        yield
    finally:
        await close_default_mcp_stdio_client_pool()
        await database.dispose()


def create_app() -> FastAPI:
    app = FastAPI(title="Arkloop Worker Bridge", lifespan=_lifespan)

    @app.get("/healthz")
    async def _healthz() -> dict[str, str]:
        return {"status": "ok"}

    @app.post("/internal/bridge/execute-run")
    async def _execute_run(request: Request) -> Response:
        _validate_token(request)

        try:
            payload = await request.json()
        except json.JSONDecodeError as exc:
            raise HTTPException(status_code=400, detail="body 必须为 JSON") from exc

        if not isinstance(payload, Mapping):
            raise HTTPException(status_code=400, detail="body 必须为对象")
        payload_json = payload.get("payload_json")
        if not isinstance(payload_json, Mapping):
            raise HTTPException(status_code=400, detail="payload_json 必须为对象")

        worker = getattr(request.app.state, "worker", None)
        if worker is None:
            raise RuntimeError("worker 未初始化")

        logger = logging.getLogger("arkloop.worker_bridge")
        try:
            await worker.handle_job(payload_json)
        except LookupError:
            return Response(status_code=404)
        except PermissionError:
            return Response(status_code=403)
        except ValueError as exc:
            logger.warning("bridge payload 不合法", extra={"error": str(exc)})
            raise HTTPException(status_code=400, detail=str(exc)) from exc
        except Exception:
            logger.exception("bridge 执行失败")
            return JSONResponse(status_code=500, content={"error": "execute failed"})

        return Response(status_code=204)

    return app


def configure_app() -> FastAPI:
    load_dotenv_if_enabled(override=False)
    configure_logging()
    app = create_app()
    app.state.bridge_token = _load_bridge_token()
    return app


__all__ = ["configure_app", "create_app"]
