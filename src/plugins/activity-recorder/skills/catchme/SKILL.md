# CatchMe

Use CatchMe when the user asks what they clicked, copied, typed, viewed, or did in local desktop activity history.

CatchMe runs as an event-driven recorder. It records window, keyboard, mouse, clipboard, idle, notification, and click/scroll screenshots into `~/.catchme/data.db`, and writes activity trees to `~/.catchme/trees/*_time.json`.

Prefer raw tree/session tools instead of `search_activity`, because `search_activity` uses CatchMe's own LLM credentials. Use `list_days`, then `get_tree(date)` or `get_session(session_id)`.

For direct inspection, query the local SQLite database:

```bash
sqlite3 ~/.catchme/data.db "SELECT datetime(ts, 'unixepoch', 'localtime'), kind, substr(data, 1, 300), blob FROM events_raw ORDER BY ts DESC LIMIT 20"
```

Use screenshot blob paths only when the user asks for visual evidence.
