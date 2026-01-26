from __future__ import annotations

from pathlib import Path
import re


def _repo_root() -> Path:
    current = Path(__file__).resolve()
    for parent in current.parents:
        if (parent / "pyproject.toml").exists():
            return parent
    raise AssertionError("未找到仓库根目录（pyproject.toml）")


def test_compose_yaml_defines_postgres_without_hardcoded_password() -> None:
    compose_path = _repo_root() / "compose.yaml"
    assert compose_path.exists()

    content = compose_path.read_text(encoding="utf-8")
    assert "services:" in content
    assert re.search(r"(?m)^\s{2}postgres:\s*$", content)
    assert re.search(r"(?m)^\s{4}image:\s*postgres:", content)

    password_line = re.search(r"(?m)^\s{6}POSTGRES_PASSWORD:\s*(.+)$", content)
    assert password_line, "compose.yaml 必须通过变量注入 POSTGRES_PASSWORD"
    assert password_line.group(1).strip().startswith(
        "${ARKLOOP_POSTGRES_PASSWORD",
    ), "禁止在仓库写死真实密码"


def test_env_example_and_gitignore_for_dotenv() -> None:
    root = _repo_root()

    env_example = (root / ".env.example").read_text(encoding="utf-8")
    for key in (
        "ARKLOOP_POSTGRES_USER",
        "ARKLOOP_POSTGRES_PASSWORD",
        "ARKLOOP_POSTGRES_DB",
        "ARKLOOP_POSTGRES_PORT",
        "ARKLOOP_DATABASE_URL",
    ):
        assert re.search(rf"(?m)^{re.escape(key)}=", env_example)

    gitignore = (root / ".gitignore").read_text(encoding="utf-8")
    assert re.search(r"(?m)^\.env$", gitignore)
    assert re.search(r"(?m)^!\.env\.example$", gitignore)
