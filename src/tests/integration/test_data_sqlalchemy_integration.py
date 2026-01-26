from __future__ import annotations

import anyio
import pytest

from packages.data import Database, DatabaseConfig

pytestmark = pytest.mark.integration


def test_select_1_succeeds_against_configured_database() -> None:
    config = DatabaseConfig.from_env(allow_fallback=True)
    if config is None:
        pytest.skip("未设置 ARKLOOP_DATABASE_URL（或兼容的 DATABASE_URL）")

    database = Database.from_config(config)

    async def _run() -> None:
        try:
            assert await database.select_one() == 1
        finally:
            await database.dispose()

    anyio.run(_run)
