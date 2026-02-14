from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol


@dataclass(frozen=True, slots=True)
class WebSearchResult:
    title: str
    url: str
    snippet: str

    def to_json(self) -> dict[str, str]:
        return {"title": self.title, "url": self.url, "snippet": self.snippet}


class WebSearchProvider(Protocol):
    async def search(self, *, query: str, max_results: int) -> list[WebSearchResult]: ...


__all__ = ["WebSearchProvider", "WebSearchResult"]

