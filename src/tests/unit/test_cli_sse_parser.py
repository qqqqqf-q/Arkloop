from __future__ import annotations

from packages.arkloop_client.sse import SseParser


def test_sse_parser_ignores_comments_and_parses_events() -> None:
    parser = SseParser()

    chunks = [
        b": ping\n\n",
        b'id: 1\nevent: run.started\ndata: {"seq":1,"type":"run.started","data":{}}\n\n',
        b'id: 2\nevent: message.delta\ndata: {"seq":2,"type":"message.delta","data":{"content_delta":"hi"}}\n\n',
    ]

    events = []
    for chunk in chunks:
        events.extend(parser.feed(chunk))

    assert [event.event_id for event in events] == ["1", "2"]
    assert [event.event for event in events] == ["run.started", "message.delta"]
    assert '"type":"run.started"' in events[0].data
    assert '"content_delta":"hi"' in events[1].data


def test_sse_parser_supports_multiline_data() -> None:
    parser = SseParser()
    chunk = b"event: x\ndata: line1\ndata: line2\n\n"
    events = parser.feed(chunk)
    assert len(events) == 1
    assert events[0].event == "x"
    assert events[0].data == "line1\nline2"
