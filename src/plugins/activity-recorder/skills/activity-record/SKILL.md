---
name: activity-record
description: Query and orchestrate Arkloop Activity Record local activity data. Covers browser history, search terms, screen time, bluetooth, shell commands, window focus, keyboard, mouse, clipboard, Codex sessions, and optional Screenpipe integration.
---

# Activity Record

Activity Record stores normalized local activity facts in:

```bash
~/.Arkloop/activity-record/activity.db
```

Use bounded SQL queries. Always include a time range or `LIMIT`.

## Tables

### `activity_events`

| Column | Description |
|--------|-------------|
| `occurred_at` | RFC3339 timestamp |
| `source` | Source key (see Sources below) |
| `source_event_id` | Stable dedup key |
| `app` | App or profile name |
| `window_title` | Active window title when available |
| `url` | URL when available |
| `action` | Event action |
| `title` | Short event title |
| `text` | Longer text when available |
| `metadata_json` | Source-specific JSON |
| `ref_kind`, `ref_key` | Optional reference pointer |

### `source_cursors`

Incremental sync state per source. Diagnostics only.

### `activity_refs`

Optional reference files.

## Sources and Actions

### `chrome` — Chromium Browser History

Covers Chrome, Chrome Canary, Chromium, Edge, Brave. Multi-profile.

| Action | Title | Metadata |
|--------|-------|----------|
| `visited` | Page title | `profile`, `duration_sec`, `foreground_sec` |
| `downloaded` | Filename | `profile`, `path`, `size_bytes`, `mime_type` |
| `searched` | Search term | `profile`, `normalized_term` |

### `safari` — Safari History (macOS)

| Action | Title | Metadata |
|--------|-------|----------|
| `visited` | Page title | `domain`, `visit_count`, `load_successful`, `score` |

### `screentime` — macOS Screen Time (knowledgeC.db)

| Action | Title | Metadata |
|--------|-------|----------|
| `app_used` | App name | `bundle_id`, `source_bundle`, `device_id`, `duration_sec` |
| `screen_on` | "screen on" | `duration_sec` |
| `screen_off` | "screen off" | `duration_sec` |
| `notification_received` | App name | `bundle_id` |
| `media_used` | App name | `bundle_id`, `duration_sec`, `url`, `media_url` |
| `media_playing` | Track title or app | `bundle_id`, `duration_sec`, `playing`, `artist`, `album`, `genre` |
| `web_used` | Domain | `bundle_id`, `web_domain`, `duration_sec` |
| `bluetooth_connected` | Device name | `device_name`, `device_type`, `is_apple_audio_device`, `duration_sec` |
| `bluetooth_disconnected` | Device name | Same as above |

### `shell` — Shell Command History

Reads `~/.zsh_history` and `~/.bash_history`. Deduplicated by command hash.

| Action | Title | Metadata |
|--------|-------|----------|
| `command` | Command (truncated) | `shell`, `line_num`. Full command in `text` column. |

### `codex` — Claude Code Sessions

| Action | Title | Metadata |
|--------|-------|----------|
| `prompted` | Prompt text | `session_id`, `cwd` |
| `received` | Response text | `session_id`, `cwd`, `model` |

### `window` — Active Window Tracking (daemon)

| Action | Title | Metadata |
|--------|-------|----------|
| `focused` | "App - Window Title" | `duration_sec`, `pid` |
| `idle_start` | "idle" | `idle_threshold_sec` |
| `idle_end` | "idle" | — |

### `keyboard` — Keystroke Counting (daemon)

| Action | Title | Metadata |
|--------|-------|----------|
| `keystroke_count` | "N keystrokes in App" | `count`, `interval_sec`, `app`, `window_title` |

### `mouse` — Mouse Activity (daemon)

| Action | Title | Metadata |
|--------|-------|----------|
| `mouse_activity` | "N clicks, N scrolls in App" | `clicks`, `scrolls`, `interval_sec`, `app`, `window_title` |

### `clipboard` — Clipboard Changes (daemon)

| Action | Title | Metadata |
|--------|-------|----------|
| `clipboard_changed` | Content preview | `length`. Full text in `text` column when enabled. |

## Examples

Recent activity across all sources:

```sql
SELECT occurred_at, source, action, title, url
FROM activity_events
ORDER BY occurred_at DESC
LIMIT 50;
```

What apps were used today (Screen Time):

```sql
SELECT title AS app, COUNT(*) AS sessions,
       SUM(json_extract(metadata_json, '$.duration_sec')) AS total_sec
FROM activity_events
WHERE source = 'screentime' AND action = 'app_used'
  AND occurred_at >= datetime('now', '-1 day')
GROUP BY title ORDER BY total_sec DESC
LIMIT 20;
```

Recent browser searches:

```sql
SELECT occurred_at, title AS search_term, url
FROM activity_events
WHERE source = 'chrome' AND action = 'searched'
  AND occurred_at >= datetime('now', '-7 days')
ORDER BY occurred_at DESC
LIMIT 50;
```

Recent shell commands:

```sql
SELECT occurred_at, text AS command
FROM activity_events
WHERE source = 'shell' AND action = 'command'
ORDER BY occurred_at DESC
LIMIT 30;
```

Bluetooth device connections:

```sql
SELECT occurred_at, action, title AS device,
       json_extract(metadata_json, '$.duration_sec') AS duration
FROM activity_events
WHERE source = 'screentime' AND action LIKE 'bluetooth%'
ORDER BY occurred_at DESC;
```

Music playing history:

```sql
SELECT occurred_at, title,
       json_extract(metadata_json, '$.artist') AS artist,
       json_extract(metadata_json, '$.album') AS album
FROM activity_events
WHERE source = 'screentime' AND action = 'media_playing'
ORDER BY occurred_at DESC
LIMIT 20;
```

Window focus timeline:

```sql
SELECT occurred_at, app, window_title,
       json_extract(metadata_json, '$.duration_sec') AS sec
FROM activity_events
WHERE source = 'window' AND action = 'focused'
  AND occurred_at >= datetime('now', '-1 day')
ORDER BY occurred_at DESC
LIMIT 50;
```

## Screenpipe Integration

When Screenpipe is enabled, use `screenpipe-api` skill for screen capture, audio transcription, and UI element context. Start with the local SQLite database for structured facts; escalate to Screenpipe for raw screen/audio content.

## Memory Rules

Write only durable context to `memory_write`:

- Stable preferences and habits.
- Ongoing projects and decisions.
- Important events with useful future context.
- Clear changes in workflow or priorities.

Do not write Notebook entries. Do not persist raw OCR, raw transcripts, app-switch logs, secrets, one-time codes, or unverified fragments.
