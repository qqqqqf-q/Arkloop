from __future__ import annotations

from fastapi.testclient import TestClient

from services.api.main import configure_app


def test_healthz_returns_ok() -> None:
    app = configure_app()
    client = TestClient(app)
    response = client.get("/healthz")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}
