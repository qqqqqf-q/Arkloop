from __future__ import annotations

from packages.arkloop_client.client import ArkloopClient


def test_arkloop_client_disables_trust_env_for_loopback() -> None:
    client = ArkloopClient(base_url="http://127.0.0.1:8001")
    assert client._trust_env is False  # noqa: SLF001


def test_arkloop_client_enables_trust_env_for_non_loopback() -> None:
    client = ArkloopClient(base_url="https://example.com")
    assert client._trust_env is True  # noqa: SLF001


def test_arkloop_client_allows_explicit_trust_env_override() -> None:
    client = ArkloopClient(base_url="http://127.0.0.1:8001", trust_env=True)
    assert client._trust_env is True  # noqa: SLF001
