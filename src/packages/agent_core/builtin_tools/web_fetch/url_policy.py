from __future__ import annotations

from dataclasses import dataclass
import ipaddress
from typing import Mapping
from urllib.parse import urlsplit


@dataclass(frozen=True, slots=True)
class UrlPolicyDeniedError(ValueError):
    reason: str
    details: Mapping[str, object] | None = None


def ensure_url_allowed(url: str) -> None:
    parsed = urlsplit(url)
    scheme = (parsed.scheme or "").casefold()
    if scheme not in {"http", "https"}:
        raise UrlPolicyDeniedError(reason="unsupported_scheme", details={"scheme": scheme})

    hostname = parsed.hostname
    if not hostname:
        raise UrlPolicyDeniedError(reason="missing_hostname")

    lowered = hostname.casefold().strip(".")
    if lowered == "localhost" or lowered.endswith(".localhost"):
        raise UrlPolicyDeniedError(reason="localhost_denied", details={"hostname": hostname})

    ip = _try_parse_ip_address(hostname)
    if ip is None:
        return

    if _is_private_ip(ip):
        raise UrlPolicyDeniedError(reason="private_ip_denied", details={"ip": str(ip)})


def _try_parse_ip_address(hostname: str) -> ipaddress.IPv4Address | ipaddress.IPv6Address | None:
    candidate = hostname.strip()
    if "%" in candidate:
        candidate = candidate.split("%", 1)[0]
    try:
        return ipaddress.ip_address(candidate)
    except ValueError:
        return None


def _is_private_ip(ip: ipaddress.IPv4Address | ipaddress.IPv6Address) -> bool:
    return bool(
        ip.is_private
        or ip.is_loopback
        or ip.is_link_local
        or ip.is_multicast
        or ip.is_reserved
        or ip.is_unspecified
    )


__all__ = ["UrlPolicyDeniedError", "ensure_url_allowed"]

