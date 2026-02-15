from __future__ import annotations

import asyncio
from dataclasses import dataclass, field
import os
import time
import uuid

from .client import McpStdioClient
from .config import McpServerConfig

_POOL_TTL_SECONDS_ENV = "ARKLOOP_MCP_POOL_TTL_SECONDS"
_POOL_MAX_CLIENTS_ENV = "ARKLOOP_MCP_POOL_MAX_CLIENTS_PER_SERVER"

_DEFAULT_MAX_CLIENTS_PER_SERVER = 1


@dataclass(frozen=True, slots=True)
class McpStdioPoolKey:
    org_id: str | None
    server_id: str

    @classmethod
    def from_context(cls, *, org_id: uuid.UUID | None, server_id: str) -> "McpStdioPoolKey":
        return cls(org_id=str(org_id) if org_id is not None else None, server_id=server_id)


@dataclass(slots=True)
class _Bucket:
    server: McpServerConfig
    condition: asyncio.Condition = field(default_factory=asyncio.Condition)
    available: list[McpStdioClient] = field(default_factory=list)
    created_at_by_client: dict[McpStdioClient, float] = field(default_factory=dict)
    in_use: int = 0

    @property
    def total(self) -> int:
        return len(self.created_at_by_client)


class McpStdioClientPool:
    def __init__(
        self,
        *,
        ttl_seconds: float | None = None,
        max_clients_per_server: int = _DEFAULT_MAX_CLIENTS_PER_SERVER,
    ) -> None:
        if max_clients_per_server <= 0:
            raise ValueError("max_clients_per_server 必须为正整数")

        self._ttl_seconds = float(ttl_seconds) if ttl_seconds is not None and ttl_seconds > 0 else None
        self._max_clients_per_server = int(max_clients_per_server)

        self._state_lock = asyncio.Lock()
        self._closed = False
        self._bucket_by_key: dict[McpStdioPoolKey, _Bucket] = {}

    @classmethod
    def from_env(cls) -> "McpStdioClientPool":
        ttl_seconds = _parse_optional_non_negative_int(_POOL_TTL_SECONDS_ENV)
        max_clients = _parse_optional_positive_int(_POOL_MAX_CLIENTS_ENV) or _DEFAULT_MAX_CLIENTS_PER_SERVER
        ttl_value = float(ttl_seconds) if ttl_seconds is not None and ttl_seconds > 0 else None
        return cls(ttl_seconds=ttl_value, max_clients_per_server=max_clients)

    async def borrow(self, *, key: McpStdioPoolKey, server: McpServerConfig) -> McpStdioClient:
        bucket = await self._get_bucket(key=key, server=server)
        async with bucket.condition:
            while True:
                if self._closed:
                    raise RuntimeError("MCP stdio 会话池已关闭")

                if bucket.available:
                    client = bucket.available.pop()
                    bucket.in_use += 1
                    return client

                if bucket.total < self._max_clients_per_server:
                    client = McpStdioClient(server=server)
                    bucket.created_at_by_client[client] = _monotonic()
                    bucket.in_use += 1
                    return client

                await bucket.condition.wait()

    async def release(self, *, key: McpStdioPoolKey, client: McpStdioClient) -> None:
        bucket = await self._lookup_bucket(key)
        if bucket is None:
            await client.close()
            return

        should_close = False
        async with bucket.condition:
            if bucket.in_use > 0:
                bucket.in_use -= 1

            created_at = bucket.created_at_by_client.get(client)
            if created_at is None:
                should_close = True
                bucket.created_at_by_client.pop(client, None)
                _discard_first(bucket.available, client)
            elif self._ttl_seconds is not None and (_monotonic() - created_at) >= self._ttl_seconds:
                should_close = True
                bucket.created_at_by_client.pop(client, None)
                _discard_first(bucket.available, client)
            else:
                bucket.available.append(client)

            bucket.condition.notify()

        if should_close:
            await client.close()

    async def evict(self, *, key: McpStdioPoolKey, client: McpStdioClient) -> None:
        bucket = await self._lookup_bucket(key)
        if bucket is None:
            await client.close()
            return

        async with bucket.condition:
            was_available = _discard_first(bucket.available, client)
            removed = bucket.created_at_by_client.pop(client, None) is not None
            if removed and not was_available and bucket.in_use > 0:
                bucket.in_use -= 1
            bucket.condition.notify()

        await client.close()

    async def close_all(self) -> None:
        async with self._state_lock:
            if self._closed:
                return
            self._closed = True
            buckets = list(self._bucket_by_key.values())

        clients: set[McpStdioClient] = set()
        for bucket in buckets:
            async with bucket.condition:
                clients.update(bucket.created_at_by_client.keys())
                bucket.available.clear()
                bucket.created_at_by_client.clear()
                bucket.in_use = 0
                bucket.condition.notify_all()

        for client in clients:
            try:
                await client.close()
            except Exception:
                pass

        async with self._state_lock:
            self._bucket_by_key.clear()

    async def _get_bucket(self, *, key: McpStdioPoolKey, server: McpServerConfig) -> _Bucket:
        async with self._state_lock:
            if self._closed:
                raise RuntimeError("MCP stdio 会话池已关闭")
            bucket = self._bucket_by_key.get(key)
            if bucket is None:
                bucket = _Bucket(server=server)
                self._bucket_by_key[key] = bucket
            return bucket

    async def _lookup_bucket(self, key: McpStdioPoolKey) -> _Bucket | None:
        async with self._state_lock:
            return self._bucket_by_key.get(key)


_DEFAULT_POOL: McpStdioClientPool | None = None


def get_default_mcp_stdio_client_pool() -> McpStdioClientPool:
    global _DEFAULT_POOL
    if _DEFAULT_POOL is None:
        _DEFAULT_POOL = McpStdioClientPool.from_env()
    return _DEFAULT_POOL


async def close_default_mcp_stdio_client_pool() -> None:
    global _DEFAULT_POOL
    pool = _DEFAULT_POOL
    _DEFAULT_POOL = None
    if pool is None:
        return
    await pool.close_all()


def _parse_optional_non_negative_int(name: str) -> int | None:
    raw = (os.getenv(name) or "").strip()
    if not raw:
        return None
    value = int(raw)
    if value < 0:
        raise ValueError(f"{name} 必须为非负整数")
    return value


def _parse_optional_positive_int(name: str) -> int | None:
    raw = (os.getenv(name) or "").strip()
    if not raw:
        return None
    value = int(raw)
    if value <= 0:
        raise ValueError(f"{name} 必须为正整数")
    return value


def _discard_first(items: list[McpStdioClient], client: McpStdioClient) -> bool:
    try:
        index = items.index(client)
    except ValueError:
        return False
    items.pop(index)
    return True


def _monotonic() -> float:
    return time.monotonic()


__all__ = [
    "McpStdioClientPool",
    "McpStdioPoolKey",
    "close_default_mcp_stdio_client_pool",
    "get_default_mcp_stdio_client_pool",
]
