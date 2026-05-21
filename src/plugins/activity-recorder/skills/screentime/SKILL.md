# Screen Time

Use Screen Time when the user asks about macOS app usage, web usage, notifications, media usage, or daily attention patterns.

Prefer the Screen Time MCP server. It exposes a `screentime_sql` tool backed by DuckDB views over macOS `knowledgeC.db`.

Useful views include:

```sql
SELECT * FROM v_app_usage ORDER BY start_time DESC LIMIT 20;
SELECT * FROM v_web_usage ORDER BY start_time DESC LIMIT 20;
SELECT * FROM v_notifications ORDER BY notification_time DESC LIMIT 20;
```

If the query fails because the database cannot be opened, the host process needs Full Disk Access.
