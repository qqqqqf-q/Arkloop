from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Mapping


class ArkloopClientError(Exception):
    pass


@dataclass(frozen=True, slots=True)
class ArkloopApiError(ArkloopClientError):
    status_code: int
    code: str
    message: str
    trace_id: str | None = None
    details: Any | None = None
    response_text: str | None = None

    def to_json(self) -> Mapping[str, Any]:
        payload: dict[str, Any] = {
            "status_code": int(self.status_code),
            "code": self.code,
            "message": self.message,
        }
        if self.trace_id:
            payload["trace_id"] = self.trace_id
        if self.details is not None:
            payload["details"] = self.details
        return payload


@dataclass(frozen=True, slots=True)
class ArkloopClientTransportError(ArkloopClientError):
    message: str
    detail: str | None = None

    def to_json(self) -> Mapping[str, Any]:
        payload: dict[str, Any] = {"message": self.message}
        if self.detail:
            payload["detail"] = self.detail
        return payload


__all__ = ["ArkloopApiError", "ArkloopClientError", "ArkloopClientTransportError"]
