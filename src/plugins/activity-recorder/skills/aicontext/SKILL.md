# AIContext

Use AIContext when the user asks about their browser history, Codex history, Claude Code history, or previously ingested local activity context.

AIContext stores data in `~/.aicontext/data/activity.db`.

Prefer read-only SQL queries through sqlite3:

```bash
sqlite3 ~/.aicontext/data/activity.db "SELECT timestamp, source, service, action, title, ref_type, ref_id FROM activity ORDER BY timestamp DESC LIMIT 20"
sqlite3 ~/.aicontext/data/activity.db "SELECT timestamp, source, service, action, title, ref_type, ref_id FROM activity WHERE unixepoch(timestamp) BETWEEN unixepoch('2026-05-20T06:00:21Z') AND unixepoch('2026-05-20T06:13:40Z') ORDER BY unixepoch(timestamp) ASC LIMIT 100"
```

Use `unixepoch(timestamp)` for time-window filters because stored timestamps may include local timezone offsets.

If the database is missing, report that AIContext has not been initialized yet.
