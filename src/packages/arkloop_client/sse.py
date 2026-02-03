from __future__ import annotations

import codecs
from dataclasses import dataclass


@dataclass(frozen=True, slots=True)
class SseEvent:
    event: str | None
    event_id: str | None
    data: str


class SseParser:
    def __init__(self) -> None:
        self._decoder = codecs.getincrementaldecoder("utf-8")()
        self._buffer = ""
        self._current_event: dict[str, object] = {
            "event": None,
            "event_id": None,
            "data_lines": [],
        }

    def feed(self, chunk: bytes) -> list[SseEvent]:
        text = self._decoder.decode(chunk)
        if text:
            self._buffer += text

        events: list[SseEvent] = []
        while True:
            line, found = self._pop_line()
            if not found:
                break
            parsed = self._handle_line(line)
            if parsed is not None:
                events.append(parsed)
        return events

    def _pop_line(self) -> tuple[str, bool]:
        if "\n" not in self._buffer:
            return "", False
        line, _, rest = self._buffer.partition("\n")
        self._buffer = rest
        if line.endswith("\r"):
            line = line[:-1]
        return line, True

    def _handle_line(self, line: str) -> SseEvent | None:
        if line.startswith(":"):
            return None

        if not line:
            return self._flush_event()

        field, sep, value = line.partition(":")
        if not sep:
            value = ""
        elif value.startswith(" "):
            value = value[1:]

        field = field.strip()
        if not field:
            return None

        if field == "event":
            self._current_event["event"] = value
            return None
        if field == "id":
            self._current_event["event_id"] = value
            return None
        if field == "data":
            data_lines = self._current_event.get("data_lines")
            if isinstance(data_lines, list):
                data_lines.append(value)
            return None

        return None

    def _flush_event(self) -> SseEvent | None:
        data_lines = self._current_event.get("data_lines")
        event = self._current_event.get("event")
        event_id = self._current_event.get("event_id")
        if not isinstance(data_lines, list):
            data_lines = []

        data = "\n".join(str(item) for item in data_lines) if data_lines else ""

        self._current_event = {
            "event": None,
            "event_id": None,
            "data_lines": [],
        }

        if not data:
            return None
        return SseEvent(
            event=str(event) if isinstance(event, str) and event else None,
            event_id=str(event_id) if isinstance(event_id, str) and event_id else None,
            data=data,
        )


__all__ = ["SseEvent", "SseParser"]
