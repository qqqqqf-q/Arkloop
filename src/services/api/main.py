from __future__ import annotations

from typing import Dict

from fastapi import APIRouter, FastAPI

from packages.observability.logging import configure_json_logging

from .error_envelope import install_error_handlers, install_unhandled_exception_middleware
from .trace import install_trace_id_middleware

_health_router = APIRouter()


@_health_router.get("/healthz")
async def healthz() -> Dict[str, str]:
    return {"status": "ok"}


def create_app() -> FastAPI:
    configure_json_logging(component="api")
    app = FastAPI(title="Arkloop API")
    install_unhandled_exception_middleware(app)
    install_error_handlers(app)
    install_trace_id_middleware(app)
    app.include_router(_health_router)
    return app


app = create_app()
