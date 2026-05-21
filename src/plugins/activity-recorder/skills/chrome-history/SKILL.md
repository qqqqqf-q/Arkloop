# Chrome History

Use Chrome History when the user asks about visited pages, searches, downloads, or browsing chronology.

Prefer the Chrome History MCP server when available. It exposes SQL access to Chrome `urls` and `visits`.

Useful query shape:

```sql
SELECT u.url, u.title, v.visit_time, v.visit_duration
FROM visits v
JOIN urls u ON v.url = u.id
ORDER BY v.visit_time DESC
LIMIT 20;
```

Chrome timestamps are microseconds since 1601-01-01 UTC.
