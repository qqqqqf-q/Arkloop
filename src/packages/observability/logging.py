from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import json
import logging
import sys
from typing import Any, Callable, Iterable, Mapping, Optional, Sequence

from .context import get_trace_id

_INSTALLED = False
_ORIGINAL_FACTORY: Optional[Callable[..., logging.LogRecord]] = None
_INSTALLED_COMPONENT: Optional[str] = None
_JSON_LOGGING_CONFIGURED = False

_BASE_RECORD_ATTRS = set(logging.LogRecord("x", 0, "", 0, "x", (), None).__dict__.keys())


@dataclass(frozen=True)
class RedactionRule:
    name: str
    match_key: Callable[[str], bool]
    action: str
    replacement: str = "<redacted>"


def _match_exact(keys: Iterable[str]) -> Callable[[str], bool]:
    lowered = {k.casefold() for k in keys}

    def _match(key: str) -> bool:
        return key.casefold() in lowered

    return _match


def _match_suffix(suffixes: Iterable[str]) -> Callable[[str], bool]:
    normalized = tuple(s.casefold() for s in suffixes)

    def _match(key: str) -> bool:
        k = key.casefold().replace("-", "_")
        return any(k.endswith(suf) for suf in normalized)

    return _match


def default_redaction_rules() -> list[RedactionRule]:
    return [
        RedactionRule(
            name="drop_http_secrets",
            match_key=_match_exact({"authorization", "cookie", "set-cookie"}),
            action="drop",
        ),
        RedactionRule(
            name="drop_system_prompt",
            match_key=_match_exact({"system_prompt", "system prompt", "system-prompt"}),
            action="drop",
        ),
        RedactionRule(
            name="mask_common_secrets",
            match_key=_match_suffix(("_key", "_token", "_secret", "_password")),
            action="mask",
        ),
        RedactionRule(
            name="mask_common_secret_keys",
            match_key=_match_exact(
                {
                    "key",
                    "api_key",
                    "apikey",
                    "access_key",
                    "secret_key",
                    "private_key",
                    "token",
                    "password",
                    "passwd",
                    "secret",
                }
            ),
            action="mask",
        ),
    ]


def _jsonable(value: Any) -> Any:
    if value is None or isinstance(value, (str, int, float, bool)):
        return value
    if isinstance(value, bytes):
        return value.decode("utf-8", errors="replace")
    if isinstance(value, Mapping):
        return {str(k): _jsonable(v) for k, v in value.items()}
    if isinstance(value, (list, tuple, set, frozenset)):
        return [_jsonable(v) for v in value]
    return str(value)


def _apply_redaction(value: Any, *, rules: Sequence[RedactionRule]) -> Any:
    if isinstance(value, Mapping):
        redacted: dict[str, Any] = {}
        for key, item in value.items():
            key_str = str(key)
            matched_rule = next((r for r in rules if r.match_key(key_str)), None)
            if matched_rule is None:
                redacted[key_str] = _apply_redaction(item, rules=rules)
                continue
            if matched_rule.action == "drop":
                continue
            if matched_rule.action == "mask":
                redacted[key_str] = matched_rule.replacement
                continue
            raise ValueError(f"未知脱敏动作: {matched_rule.action}")
        return redacted
    if isinstance(value, list):
        return [_apply_redaction(v, rules=rules) for v in value]
    return value


def _extract_extra(record: logging.LogRecord) -> dict[str, Any]:
    data: dict[str, Any] = {}
    for key, value in record.__dict__.items():
        if key in _BASE_RECORD_ATTRS or key in {"trace_id", "component"}:
            continue
        if key.startswith("_"):
            continue
        data[key] = _jsonable(value)
    return data


class JsonLineFormatter(logging.Formatter):
    def __init__(self, *, rules: Sequence[RedactionRule]) -> None:
        super().__init__()
        self._rules = rules

    def format(self, record: logging.LogRecord) -> str:
        ts = datetime.fromtimestamp(record.created, tz=timezone.utc).isoformat(timespec="milliseconds")
        payload: dict[str, Any] = {
            "ts": ts.replace("+00:00", "Z"),
            "level": record.levelname.lower(),
            "msg": record.getMessage(),
            "logger": record.name,
            "component": getattr(record, "component", None),
            "trace_id": getattr(record, "trace_id", None),
        }
        payload.update(_extract_extra(record))
        payload = _apply_redaction(payload, rules=self._rules)
        if record.exc_info:
            payload["exception"] = self.formatException(record.exc_info)
        return json.dumps(payload, ensure_ascii=False, separators=(",", ":"))


class StdoutHandler(logging.StreamHandler):
    def emit(self, record: logging.LogRecord) -> None:
        self.stream = sys.stdout
        super().emit(record)


def install_trace_log_record_factory(*, component: Optional[str] = None) -> None:
    global _INSTALLED
    global _ORIGINAL_FACTORY
    global _INSTALLED_COMPONENT
    if _INSTALLED:
        if component is not None and _INSTALLED_COMPONENT not in (None, component):
            raise ValueError("LogRecordFactory 已安装且 component 不一致")
        return

    _ORIGINAL_FACTORY = logging.getLogRecordFactory()
    _INSTALLED_COMPONENT = component

    def _record_factory(*args, **kwargs) -> logging.LogRecord:
        if _ORIGINAL_FACTORY is None:
            raise RuntimeError("LogRecordFactory 未初始化")
        record = _ORIGINAL_FACTORY(*args, **kwargs)
        record.trace_id = get_trace_id()
        if component is not None:
            record.component = component
        return record

    logging.setLogRecordFactory(_record_factory)
    _INSTALLED = True


def configure_json_logging(
    *,
    component: str,
    level: int = logging.INFO,
    rules: Optional[Sequence[RedactionRule]] = None,
) -> None:
    global _JSON_LOGGING_CONFIGURED
    if _JSON_LOGGING_CONFIGURED:
        return

    install_trace_log_record_factory(component=component)
    redaction_rules = list(rules) if rules is not None else default_redaction_rules()

    handler = StdoutHandler()
    handler.setFormatter(JsonLineFormatter(rules=redaction_rules))
    handler._arkloop_json = True  # type: ignore[attr-defined]

    root = logging.getLogger()
    root.setLevel(level)
    root.handlers = [h for h in root.handlers if not getattr(h, "_arkloop_json", False)]
    root.addHandler(handler)

    _JSON_LOGGING_CONFIGURED = True
