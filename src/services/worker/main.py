from __future__ import annotations

import asyncio
import logging

from packages.mcp.pool import close_default_mcp_stdio_client_pool
from packages.observability.logging import configure_json_logging

from .composition import create_container


async def _run() -> None:
    database, loop = await create_container()
    try:
        await loop.run_forever()
    finally:
        await close_default_mcp_stdio_client_pool()
        await database.dispose()


def run() -> None:
    configure_json_logging(component="worker")
    try:
        asyncio.run(_run())
    except KeyboardInterrupt:
        logging.getLogger("arkloop.worker").info("worker 退出")


if __name__ == "__main__":
    run()
