from __future__ import annotations

import os
from pathlib import Path
import re

import pytest


def _selects_marker(markexpr: str, marker: str) -> bool:
    return bool(re.search(rf"(?<![A-Za-z0-9_]){re.escape(marker)}(?![A-Za-z0-9_])", markexpr))

def _excludes_marker(markexpr: str, marker: str) -> bool:
    return bool(re.search(rf"(?<![A-Za-z0-9_])not\\s+{re.escape(marker)}(?![A-Za-z0-9_])", markexpr))


def pytest_configure(config) -> None:
    markexpr = getattr(config.option, "markexpr", "") or ""

    selects_integration = _selects_marker(markexpr, "integration")
    excludes_integration = _excludes_marker(markexpr, "integration")

    selects_functional = _selects_marker(markexpr, "functional")
    excludes_functional = _excludes_marker(markexpr, "functional")

    selects_external = (selects_integration and not excludes_integration) or (
        selects_functional and not excludes_functional
    )
    if selects_external:
        os.environ.setdefault("ARKLOOP_LOAD_DOTENV", "1")


def _layer_from_path(path: Path) -> str | None:
    parts = path.parts
    for i, part in enumerate(parts):
        if part == "tests" and i + 1 < len(parts):
            return parts[i + 1]
    return None


@pytest.hookimpl(tryfirst=True)
def pytest_collection_modifyitems(config, items) -> None:
    for item in items:
        path = getattr(item, "path", None)
        if path is None:
            path = Path(str(item.fspath))

        layer = _layer_from_path(Path(path))
        if layer == "integration":
            item.add_marker(pytest.mark.integration)
        elif layer == "functional":
            item.add_marker(pytest.mark.functional)
