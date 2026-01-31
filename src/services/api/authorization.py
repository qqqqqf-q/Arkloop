from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass
import uuid

from .error_envelope import ApiError


@dataclass(frozen=True, slots=True)
class Actor:
    org_id: uuid.UUID
    user_id: uuid.UUID
    org_role: str


@dataclass(frozen=True, slots=True)
class Resource:
    org_id: uuid.UUID
    owner_user_id: uuid.UUID | None


class AuthorizationPolicy(ABC):
    @abstractmethod
    async def authorize(self, action: str, *, actor: Actor, resource: Resource) -> None: ...


class OwnerOnlyPolicy(AuthorizationPolicy):
    async def authorize(self, action: str, *, actor: Actor, resource: Resource) -> None:
        if actor.org_id != resource.org_id:
            raise ApiError(code="policy.denied", message="无权限", status_code=403, details={"action": action})

        if resource.owner_user_id is None:
            raise ApiError(code="policy.denied", message="无权限", status_code=403, details={"action": action})
        if resource.owner_user_id != actor.user_id:
            raise ApiError(code="policy.denied", message="无权限", status_code=403, details={"action": action})


class Authorizer:
    def __init__(self, *, policy: AuthorizationPolicy | None = None) -> None:
        self._policy = policy or OwnerOnlyPolicy()

    async def authorize(self, action: str, *, actor: Actor, resource: Resource) -> None:
        await self._policy.authorize(action, actor=actor, resource=resource)


__all__ = ["Actor", "AuthorizationPolicy", "Authorizer", "OwnerOnlyPolicy", "Resource"]
