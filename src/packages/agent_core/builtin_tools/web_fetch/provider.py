from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol


@dataclass(frozen=True, slots=True)
class WebFetchResult:
    url: str
    content: str
    title: str
    truncated: bool

    def to_json(self) -> dict[str, object]:
        return {
            "content": self.content,
            "title": self.title,
            "url": self.url,
            "truncated": self.truncated,
        }


class WebFetchProvider(Protocol):
    async def fetch(self, *, url: str, max_length: int) -> WebFetchResult: ...


__all__ = ["WebFetchProvider", "WebFetchResult"]

