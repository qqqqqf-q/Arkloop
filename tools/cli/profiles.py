from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path


def find_repo_root(start: Path) -> Path:
    current = start.resolve()
    for directory in (current, *current.parents):
        if (directory / "pyproject.toml").is_file() or (directory / ".git").exists():
            return directory
    return current


@dataclass(frozen=True, slots=True)
class ProfileInfo:
    name: str
    path: Path
    source: str


class ProfileLocator:
    def __init__(self, *, repo_root: Path | None = None, home: Path | None = None) -> None:
        self._repo_root = repo_root or find_repo_root(Path.cwd())
        self._home = home or Path.home()

    def list_profiles(self) -> list[ProfileInfo]:
        found: dict[str, ProfileInfo] = {}

        for item in self._iter_user_profiles():
            found.setdefault(item.name, item)

        for item in self._iter_repo_profiles():
            found.setdefault(item.name, item)

        return sorted(found.values(), key=lambda profile: profile.name)

    def resolve(self, name: str) -> ProfileInfo | None:
        target = name.strip()
        if not target:
            return None
        for profile in self.list_profiles():
            if profile.name == target:
                return profile
        return None

    def _iter_user_profiles(self) -> list[ProfileInfo]:
        directory = self._home / ".arkloop" / "profiles"
        if not directory.is_dir():
            return []

        profiles: list[ProfileInfo] = []
        for path in directory.glob("*.env"):
            if not path.is_file():
                continue
            name = path.stem.strip()
            if not name:
                continue
            profiles.append(ProfileInfo(name=name, path=path, source="user"))
        return profiles

    def _iter_repo_profiles(self) -> list[ProfileInfo]:
        directory = self._repo_root / "src" / "apps" / "cli" / "env"
        if not directory.is_dir():
            return []

        profiles: list[ProfileInfo] = []
        for path in directory.glob(".env.*"):
            if not path.is_file():
                continue
            filename = path.name
            name = filename[len(".env.") :].strip() if filename.startswith(".env.") else ""
            if not name:
                continue
            profiles.append(ProfileInfo(name=name, path=path, source="repo"))
        return profiles


__all__ = ["ProfileInfo", "ProfileLocator", "find_repo_root"]
