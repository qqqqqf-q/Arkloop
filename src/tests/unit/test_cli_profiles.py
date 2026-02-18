from __future__ import annotations

from pathlib import Path

from apps.cli.profiles import ProfileLocator


def test_profile_locator_prefers_user_profiles(monkeypatch, tmp_path: Path) -> None:
    repo_root = tmp_path / "repo"
    home = tmp_path / "home"
    repo_root.mkdir(parents=True)
    home.mkdir(parents=True)

    user_dir = home / ".arkloop" / "profiles"
    user_dir.mkdir(parents=True)
    (user_dir / "llm_test.env").write_text(
        "ARKLOOP_API_BASE_URL=http://127.0.0.1:8001\n", encoding="utf-8"
    )

    repo_env_dir = repo_root / "src" / "apps" / "cli" / "env"
    repo_env_dir.mkdir(parents=True)
    (repo_env_dir / ".env.llm_test").write_text(
        "ARKLOOP_API_BASE_URL=http://example.invalid\n", encoding="utf-8"
    )

    locator = ProfileLocator(repo_root=repo_root, home=home)
    resolved = locator.resolve("llm_test")
    assert resolved is not None
    assert resolved.source == "user"
    assert resolved.path == user_dir / "llm_test.env"


def test_profile_locator_lists_repo_profiles_when_user_missing(tmp_path: Path) -> None:
    repo_root = tmp_path / "repo"
    home = tmp_path / "home"
    repo_root.mkdir(parents=True)
    home.mkdir(parents=True)

    repo_env_dir = repo_root / "src" / "apps" / "cli" / "env"
    repo_env_dir.mkdir(parents=True)
    (repo_env_dir / ".env.dev").write_text(
        "ARKLOOP_API_BASE_URL=http://127.0.0.1:8001\n", encoding="utf-8"
    )

    locator = ProfileLocator(repo_root=repo_root, home=home)
    profiles = locator.list_profiles()
    assert [item.name for item in profiles] == ["dev"]
    assert profiles[0].source == "repo"
