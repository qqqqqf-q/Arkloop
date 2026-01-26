from __future__ import annotations

from pathlib import Path

from packages.data import DatabaseConfig


def test_load_dotenv_when_enabled(monkeypatch, tmp_path: Path) -> None:
    env_file = tmp_path / ".env"
    env_file.write_text(
        "ARKLOOP_DATABASE_URL=postgresql://arkloop:pass@127.0.0.1:5432/arkloop\n",
        encoding="utf-8",
    )

    monkeypatch.delenv("ARKLOOP_DATABASE_URL", raising=False)
    monkeypatch.setenv("ARKLOOP_DOTENV_FILE", str(env_file))
    monkeypatch.setenv("ARKLOOP_LOAD_DOTENV", "1")

    config = DatabaseConfig.from_env(allow_fallback=False)
    assert config is not None
    assert config.url.startswith("postgresql+asyncpg://")


def test_dotenv_does_not_override_existing_env(monkeypatch, tmp_path: Path) -> None:
    env_file = tmp_path / ".env"
    env_file.write_text(
        "ARKLOOP_DATABASE_URL=postgresql+asyncpg://arkloop:dotenv@127.0.0.1:5432/arkloop\n",
        encoding="utf-8",
    )

    monkeypatch.setenv(
        "ARKLOOP_DATABASE_URL",
        "postgresql+asyncpg://arkloop:env@127.0.0.1:5432/arkloop",
    )
    monkeypatch.setenv("ARKLOOP_DOTENV_FILE", str(env_file))
    monkeypatch.setenv("ARKLOOP_LOAD_DOTENV", "1")

    config = DatabaseConfig.from_env(allow_fallback=False)
    assert config is not None
    assert config.url == "postgresql+asyncpg://arkloop:env@127.0.0.1:5432/arkloop"


def test_dotenv_is_not_loaded_when_disabled(monkeypatch, tmp_path: Path) -> None:
    env_file = tmp_path / ".env"
    env_file.write_text(
        "ARKLOOP_DATABASE_URL=postgresql://arkloop:pass@127.0.0.1:5432/arkloop\n",
        encoding="utf-8",
    )

    monkeypatch.delenv("ARKLOOP_DATABASE_URL", raising=False)
    monkeypatch.delenv("ARKLOOP_LOAD_DOTENV", raising=False)
    monkeypatch.setenv("ARKLOOP_DOTENV_FILE", str(env_file))

    config = DatabaseConfig.from_env(allow_fallback=False)
    assert config is None
