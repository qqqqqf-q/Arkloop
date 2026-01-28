from __future__ import annotations

from abc import ABC, abstractmethod

import bcrypt

_DEFAULT_BCRYPT_ROUNDS = 12


class PasswordHasher(ABC):
    @abstractmethod
    def hash_password(self, password: str) -> str: ...

    @abstractmethod
    def verify_password(self, password: str, password_hash: str) -> bool: ...


class BcryptPasswordHasher(PasswordHasher):
    def __init__(self, *, rounds: int = _DEFAULT_BCRYPT_ROUNDS) -> None:
        if rounds < 4 or rounds > 31:
            raise ValueError("bcrypt rounds 必须在 [4, 31] 范围内")
        self._rounds = rounds

    def hash_password(self, password: str) -> str:
        password_bytes = password.encode("utf-8")
        salt = bcrypt.gensalt(rounds=self._rounds)
        hashed = bcrypt.hashpw(password_bytes, salt)
        return hashed.decode("utf-8")

    def verify_password(self, password: str, password_hash: str) -> bool:
        try:
            return bcrypt.checkpw(password.encode("utf-8"), password_hash.encode("utf-8"))
        except (ValueError, TypeError):
            return False


__all__ = ["BcryptPasswordHasher", "PasswordHasher"]

