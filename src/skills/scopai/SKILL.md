---
name: scopai
description: Social media data analysis CLI — search, import, download comments/media, and run multi-step strategy analysis.
type: tool-use
---

# scopai Skill

You operate the `scopai` command-line tool for social media content analysis.

## System Requirements

- **Node.js** 20+
- **npm** or **pnpm** (for building from source)
- **@scopai/cli** installed globally: `pnpm add -g @scopai/cli`
- **opencli** installed and available in PATH

## Pre-execution Checks

Run these **in order** before any workflow:

1. **Verify CLI is executable**: `scopai --version`
2. **Ensure API server is running**: `scopai daemon status` → `scopai daemon start` if needed
3. **Read opencli skill** before using any `opencli` command
4. **Verify opencli**: `opencli --help` or `opencli doctor`

> The daemon is the API server (Fastify + in-process workers). `daemon start` launches it, and CLI commands communicate with it via HTTP. If daemon logs a health-check failure and exits, do **not** delete the database file. Ensure no other process holds the database lock, then restart the daemon.

---

## Capabilities by Phase

### Phase 1: Data Collection

| # | Tool | Command | When to Use |
|---|------|---------|-------------|
| 1 | **search_posts** | `opencli <site> <command> {query} --limit {limit} -f json` | Discover posts before importing. **Commands vary by platform** — (1) run `opencli list \| grep <keyword>` to find the platform, (2) run `opencli <platform> --help` to list available commands, (3) run `opencli <platform> <command> -h` to understand specific command usage.|
| 2 | **add_platform** | `scopai platform add --id {id} --name {name}` | Register a platform if not already in `scopai platform list`. |
| 3 | **import_posts** | `scopai post import --platform {id} --file {path} [--task-id {tid}]` | Import search results. **Do NOT manually fetch note details before import** — let `prepare-data` enrich posts via `fetch_note` template. Duplicates are updated, not skipped. |
| 4 | **import_comments** | `scopai comment import --platform {id} --post-id {id} --file {path}` | Import comments from JSON/JSONL after fetching. Duplicates skipped. |

### Phase 2: Task Setup

| # | Tool | Command | When to Use |
|---|------|---------|-------------|
| 5 | **create_task** | `scopai task create --name {name} [--cli-templates '{...}']` | Create task before adding steps. `--cli-templates` is optional — platforms (xhs, douyin) have built-in defaults for fetch_note, fetch_comments, and fetch_media. Only specify `--cli-templates` when you need to override the defaults. **Important:** If you override `fetch_media`, you MUST include `--output {download_dir}/{platform}` to ensure media files are saved to the correct directory. |
| 6 | **add_step_to_task** | `scopai task step add --task-id {tid} --strategy-id {sid} [--name {n}] [--order {n}]` | Add each strategy the user needs. |
| 7 | **list_strategies** | `scopai strategy list` | Check available strategy IDs before adding steps. |

### Phase 3: Data Preparation

| # | Tool | Command | When to Use |
|---|------|---------|-------------|
| 8 | **prepare_task_data** | `scopai task prepare-data {tid}` | Fetch full post details, comments, and media. Runs 3 steps per post: fetch_note (skipped if template empty, e.g. douyin where search already covers note data), fetch_comments (skipped if empty), fetch_media (uses platform defaults if not configured). **Resumable** — continues from unfinished posts on retry. |

### Phase 4: Analysis Execution

| # | Tool | Command | When to Use |
|---|------|---------|-------------|
| 9 | **run_all_steps** | `scopai task run-all-steps {tid}` | **Default `--wait`**: blocks until all steps complete, printing progress. Use `--no-wait` for fire-and-forget. |
| 10 | **run_task_step** | `scopai task step run --task-id {tid} --step-id {sid}` | Run a single step. **Default `--wait`**: blocks until completion. |
| 11 | **reset_task_step** | `scopai task step reset --task-id {tid} --step-id {sid}` | Reset a failed step to pending for retry. |
| 12 | **submit_analysis** | `scopai analyze submit --strategy-id {sid} --post-ids {ids} [--task-id {tid}] [--force]` | Simplified single-post analysis. Auto-creates task and step. Deduplicates by default; `--force` deletes old results+jobs and re-enqueues. |

> **Progress output** (`--wait` mode):
> ```
> [10:23:45] Step: sentiment-analysis | running | 15/30 done, 1 failed
> [10:24:45] Steps progress: 2/2 completed
> ```

### Phase 5: Results & Management

| # | Tool | Command | When to Use |
|---|------|---------|-------------|
| 12 | **get_task_results** | `scopai task results {tid}` | After all steps complete. Shows result summary. |
| 13 | **get_task_status** | `scopai task show {tid}` | Show full task details including phases, steps, jobs, and recent failures. **Not needed when using `--wait` mode.** |
| 14 | **list_tasks** | `scopai task list [--status {s}] [--query {text}]` | View existing tasks. Filter by status or search by name. |
| 15 | **list_task_steps** | `scopai task step list {tid}` | Inspect step states before running. |
| 16 | **strategy_result_list** | `scopai strategy result list --task-id {tid} --strategy {sid}` | Inspect per-post results. |
| 17 | **strategy_result_export** | `scopai strategy result export --task-id {tid} --strategy {sid} [--format csv|json] [--output {path}]` | Export results to file. |
| 18 | **strategy_result_stats** | `scopai strategy result stats --task-id {tid} --strategy {sid}` | Show numeric and text field statistics. |
| 19 | **strategy_result_aggregate** | `scopai strategy result aggregate --task-id {tid} --strategy {sid} --group-by {field} [--agg count|sum|avg|min|max] [--format table|csv|json]` | Aggregate a specific result field. |

### Utility & Recovery

| # | Tool | Command | When to Use |
|---|------|---------|-------------|
| 20 | **retry_failed_queue_jobs** | `scopai queue retry [--task-id {tid}]` | Re-run only failed jobs. |
| 21 | **reset_queue_jobs** | `scopai queue reset [--task-id {tid}]` | **Blunt instrument**: force-reset all non-pending jobs. Prefer `queue retry`. |
| 22 | **list_queue_jobs** | `scopai queue list --task-id {tid} [--failed-only] [--limit {n}]` | Inspect queue job status. |
| 23 | **pause_task / resume_task / cancel_task** | `scopai task pause|resume|cancel {tid}` | Control running tasks. |
| 24 | **show_post** | `scopai post show {pid}` | Show post details (title, author, stats, content preview). |
| 25 | **list_posts / search_posts_db** | `scopai post list [--platform {id}] [--starred] [--label {name}]` / `scopai post search --platform {id} --query {text}` | Browse imported data. `--starred` filters starred posts; `--label` filters by label name. |
| 26 | **daemon management** | `scopai daemon start [--fg] [--verbose]` / `stop` / `restart` / `status` | Manage API server lifecycle. CLI auto-restarts if version mismatch. |
| 26 | **run_single_analysis** | `scopai analyze run --task-id {tid} --strategy-id {sid}` | Run a one-shot strategy analysis without task steps. |
| 27 | **submit_analysis** | `scopai analyze submit --strategy-id {sid} --post-ids {ids}` | Simplified analysis entry point. See Phase 4 #12. |
| 27 | **view_logs** | `scopai logs show [--lines {n}] [--date {d}]` | View recent API server log entries. |

### Labels & Stars

| # | Tool | Command | When to Use |
|---|------|---------|-------------|
| 28 | **star_post** | `scopai post star --id {pid}` | Star a post for quick access. |
| 29 | **unstar_post** | `scopai post star --id {pid} --unstar` | Remove star from a post. |
| 30 | **tag_post** | `scopai post tag --id {pid} --label-name {name}` or `--label-names {n1,n2}` | Add labels to a post. Labels are auto-created if they don't exist. |
| 31 | **untag_post** | `scopai post untag --id {pid} --label-id {lid}` | Remove a label from a post. |
| 32 | **list_labels** | `scopai label list` | List all labels with post counts. |
| 33 | **create_label** | `scopai label create --name {name} [--color {hex}]` | Create a label. |
| 34 | **delete_label** | `scopai label delete --id {lid}` | Delete a label and its post associations. |

### Creator Subscription & Sync

| # | Tool | Command | When to Use |
|---|------|---------|-------------|
| 28 | **creator_add** | `scopai creator add --platform {id} --author-id {aid} [--name {name}]` | Subscribe to a creator/blogger. Auto-creates sync schedule. |
| 29 | **creator_list** | `scopai creator list [--platform {id}] [--status {s}]` | List subscribed creators. |
| 30 | **creator_show** | `scopai creator show --id {id}` | Show creator details and recent sync logs. |
| 31 | **creator_sync** | `scopai creator sync --id {id} [--initial]` | Trigger manual sync. `--initial` imports all historical posts. |
| 32 | **creator_pause/resume** | `scopai creator pause|resume --id {id}` | Pause/resume automatic sync. |
| 33 | **creator_remove** | `scopai creator remove --id {id}` | Unsubscribe from a creator. |

> **Creator sync pipeline**: Independent from task/queue pipeline. Worker polls `creator_sync_jobs` directly, fetches posts via `opencli` (e.g., `opencli xiaohongshu user {author_id} --format json`, `opencli douyin user-videos {author_id} --limit {limit} -f json`), normalizes via platform adapter fieldMap, and upserts into `posts` table. Profile sync uses adapter's profileFieldMap and homepageUrlTemplate.

### Hooks (Event Notifications)

Configure hooks in `~/.scopai/config.json` to receive notifications on task lifecycle events. Supports two hook types:

| Type | Description | Use When |
|------|-------------|----------|
| `command` | Executes a shell command with template variable substitution | Local notifications (osascript, terminal bell, custom scripts) |
| `http` | Sends a POST request with JSON payload to a URL | Remote notifications (Slack, Discord, Telegram, custom webhooks, n8n, Zapier) |

**Available events:**

| Event | Trigger |
|-------|---------|
| `TaskCompleted` | All task steps completed successfully |
| `TaskFailed` | Task failed or cancelled |
| `StepCompleted` | A strategy step completed |
| `StepFailed` | A strategy step has permanently failed jobs |
| `PrepareDataCompleted` | All prepare jobs completed (no failures) |
| `PrepareDataFailed` | Some prepare jobs failed |

**Template variables** (available in `command` hooks):

| Variable | Value |
|----------|-------|
| `$TASK_ID` | Task ID |
| `$TASK_NAME` | Task name |
| `$STEP_ID` | Step ID (Step events only) |
| `$STEP_NAME` | Step name (Step events only) |
| `$STRATEGY_ID` | Strategy ID (Step events only) |
| `$EVENT` | Event name |
| `$ERROR` | Error message (failure events only) |
| `$STATS_TOTAL` | Total job count |
| `$STATS_DONE` | Completed job count |
| `$STATS_FAILED` | Failed job count |

For `command` hooks, the full payload is also available via `$SCOPAI_HOOK_PAYLOAD` environment variable (JSON).

**HTTP webhook payload structure:**

Every `http` hook sends a `POST` request with `Content-Type: application/json`. The body is a `HookPayload` object:

```json
{
  "event": "TaskCompleted",
  "timestamp": "2026-05-05T12:30:00.000Z",
  "task_id": "abc123",
  "task_name": "上海美食分析",
  "step_id": null,
  "step_name": null,
  "strategy_id": null,
  "error": null,
  "stats": { "total": 10, "done": 10, "failed": 0 }
}
```

Fields vary by event:

| Field | TaskCompleted / TaskFailed | StepCompleted / StepFailed | PrepareDataCompleted / PrepareDataFailed |
|-------|---------------------------|---------------------------|------------------------------------------|
| `event` | ✅ | ✅ | ✅ |
| `timestamp` | ✅ | ✅ | ✅ |
| `task_id` | ✅ | ✅ | ✅ |
| `task_name` | ✅ | ✅ | ✅ |
| `step_id` | ❌ | ✅ | ❌ |
| `step_name` | ❌ | ✅ | ❌ |
| `strategy_id` | ❌ | ✅ | ❌ |
| `error` | ❌ (TaskCompleted) / ✅ (TaskFailed) | ❌ (StepCompleted) / ✅ (StepFailed) | ❌ (PrepareDataCompleted) / ✅ (PrepareDataFailed) |
| `stats` | ✅ | ✅ | ✅ |

**HTTP hook configuration:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `type` | `"http"` | — | Hook type identifier |
| `url` | `string` | — | Target URL for POST request |
| `headers` | `object` | `{}` | Custom HTTP headers merged with `Content-Type: application/json` |
| `timeout_ms` | `number` | `5000` | Request timeout in milliseconds |

**Example config (macOS notifications + Slack webhook):**

```json
{
  "hooks": {
    "TaskCompleted": [
      { "type": "command", "command": "osascript -e 'display notification \"$TASK_NAME completed ($STATS_DONE/$STATS_TOTAL)\" with title \"scopai\"'" },
      { "type": "http", "url": "https://hooks.slack.com/services/T00/B00/xxx", "headers": {} }
    ],
    "TaskFailed": [
      { "type": "command", "command": "osascript -e 'display notification \"$TASK_NAME failed: $ERROR\" with title \"scopai\" sound name \"Sosumi\"'" }
    ],
    "StepCompleted": [
      { "type": "command", "command": "osascript -e 'display notification \"$STEP_NAME completed\" with title \"scopai\" subtitle \"$TASK_NAME\"'" }
    ],
    "StepFailed": [
      { "type": "command", "command": "osascript -e 'display notification \"$STEP_NAME failed\" with title \"scopai\" subtitle \"$TASK_NAME\" sound name \"Sosumi\"'" }
    ],
    "PrepareDataCompleted": [
      { "type": "command", "command": "osascript -e 'display notification \"$TASK_NAME data ready ($STATS_DONE/$STATS_TOTAL)\" with title \"scopai\"'" }
    ],
    "PrepareDataFailed": [
      { "type": "command", "command": "osascript -e 'display notification \"$TASK_NAME data prep failed ($STATS_FAILED)\" with title \"scopai\" sound name \"Sosumi\"'" }
    ]
  }
}
```

**Slack webhook example payload transformation:**

Slack expects a specific format. Use a middleware (n8n, Zapier) or write a small adapter script that receives the scopai payload and reformats it:

```bash
# Adapter script approach: command hook that calls curl with reformatted payload
{ "type": "command", "command": "curl -s -X POST https://hooks.slack.com/services/T00/B00/xxx -H 'Content-Type: application/json' -d '{\"text\":\"[$EVENT] $TASK_NAME — $STATS_DONE/$STATS_TOTAL done\"}'" }
```

**Discord webhook example:**

```json
{ "type": "http", "url": "https://discord.com/api/webhooks/123456/abcdef", "headers": {} }
```

Discord accepts the scopai payload directly — use the `content` field by wrapping with an adapter or middleware that maps `event` + `task_name` into a Discord message format.

### Advanced: Create Strategy

| # | Tool | Description |
|---|------|-------------|
| 34 | **create_strategy** | Generate a new analysis strategy via conversation. See JSON Rules below. |

---

## Execution Modes

Both `task prepare-data` and `task run-all-steps` / `task step run` support two execution modes. Choose based on whether the user wants to wait for completion or move on:

| Mode | Flag | Behavior | Use When |
|------|------|----------|----------|
| **Blocking (default)** | `--wait` | Command blocks until completion and prints live progress. Agent sees output and can report results immediately. | **Recommended for interactive workflows.** User wants real-time feedback. |
| **Non-blocking** | `--no-wait` | Command returns immediately after enqueueing jobs. Agent must check status later. | User wants to fire-and-forget, or is running multiple tasks in parallel. |

> `prepare-task-data` is always blocking (it has no `--no-wait` flag). `run-all-steps` and `step run` default to `--wait`.

---

## Standard Workflow

Data preparation and analysis are executed **back-to-back** in the standard flow:

```
search_posts(query) → add_platform(if new) → create_task(with fetch_note template)
  → import_posts(with --task-id) → add_step_to_task(for each strategy)
  → prepare_task_data(blocks until done)
  → run_all_steps --wait(blocks until all steps complete, prints progress)
  → get_task_results
```

### Template Discovery (Dynamic Query)

Since OpenCLI supports 100+ platforms and commands change over time, **always discover the correct commands dynamically** instead of relying on hard-coded examples.

**Step 1: Find the site name**
```bash
opencli list | grep -i <platform_name>
# e.g., opencli list | grep -i xiaohongshu → xiaohongshu
```

**Step 2: Discover available commands**
```bash
opencli <site> --help
# e.g., opencli xiaohongshu --help → search, note, comments, download, ...
```

**Step 3: Check command signature (critical!)**
```bash
opencli <site> <command> --help
# e.g., opencli xiaohongshu note --help
# Output: "note-id  Full Xiaohongshu note URL with xsec_token"
#         → requires {url} variable
```

**Step 4: Build templates based on command signature**
- If help says `"<note-id>"` or `"<post-id>"` (short ID) → use `{note_id}`
- If help says `"Full URL"` or `"URL"` → use `{url}` (always preferred when ambiguous)

### Template Variables

| Variable | Value | Use When |
|----------|-------|----------|
| `{post_id}` | Internal database post ID | Rarely needed by external commands |
| `{note_id}` | `metadata.note_id` → `url` → `post_id` (fallback chain) | Commands that accept short IDs |
| `{url}` | Full post URL from import data | **Default choice** — most OpenCLI commands accept full URLs |
| `{limit}` | Hardcoded `100` | Pagination limit for fetch_comments |
| `{download_dir}` | Configured download directory | Media file storage path |

> **Rule of thumb**: When in doubt, use `{url}`. It is the most universally accepted format across platforms.

### Example: Full Analysis Flow (Dynamic Discovery)

```bash
# 1. Discover platform commands
opencli list | grep -i xiaohongshu        # → xiaohongshu
opencli xiaohongshu --help                # → search, note, comments, ...
opencli xiaohongshu note --help           # → requires "Full note URL"

# 2. Search
opencli xiaohongshu search "上海美食" --limit 10 -f json > posts.json

# 3. Setup
scopai platform add --id xhs --name "小红书"
scopai task create --name "上海美食分析"
# --cli-templates is optional. Platforms have built-in defaults for fetch_note, fetch_comments, fetch_media.
# Only override when needed, e.g. to skip fetch_note for douyin (search results already contain full data):
#   scopai task create --name "..." --cli-templates '{"fetch_note":"","fetch_comments":"opencli douyin comments {url} -f json"}'
# If you override fetch_media, you MUST include --output {download_dir}/{platform}:
#   "fetch_media":"opencli xiaohongshu download {url} --output {download_dir}/{platform} -f json"

# 4. Import
scopai post import --platform xhs --file posts.json --task-id <task_id>

# 5. Add strategies
scopai task step add --task-id <task_id> --strategy-id sentiment-topics --name "情感分析"
scopai task step add --task-id <task_id> --strategy-id risk-detection --name "风险检测"

# 6. Prepare data (blocks, resumable)
scopai task prepare-data <task_id>

# 7. Run analysis (blocks with progress output)
scopai task run-all-steps <task_id>
# → [10:23:45] Steps progress: 0/2 completed | running: 情感分析
# → [10:24:12] Steps progress: 1/2 completed | running: 风险检测
# → [10:24:45] Steps progress: 2/2 completed

# 8. Results
scopai task results <task_id>
```

### Alternative: Non-blocking Mode

If the user wants to start the analysis and check back later (e.g., running multiple tasks in parallel):

```bash
# Data preparation still blocks
scopai task prepare-data <task_id>

# But analysis runs in background
scopai task run-all-steps <task_id> --no-wait
# → "All steps processed" (returns immediately)

# Check status later
scopai task show <task_id>
```

### Recovery from Failure

```bash
# If a step fails after all retries:
scopai task step reset --task-id <tid> --step-id <sid>
scopai task step run --task-id <tid> --step-id <sid> --wait
```

> `step reset` automatically clears old queue jobs and strategy results for that step, then recreates them on the next run. No manual cleanup needed.

---

## JSON Rules for create_strategy

**Required fields:**

- `id`: lowercase `a-z0-9_-`, e.g. `monetization-v1`
- `name`, `version` (default `"1.0.0"`)
- `target`: `"post"` only
- `needs_media`: media upload configuration (see table below)
- `prompt`: must include `{{content}}`; include `{{media_urls}}` if `needs_media.enabled`

**`needs_media` fields:**

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | `boolean` | If `true`, media files are fetched, filtered, and uploaded as base64 content blocks to the LLM for multimodal analysis |
| `media_types` | `string[]` | Filter which media types to process, e.g. `["image"]`, `["image", "video"]` |
| `max_media` | `number` | Maximum number of media files to upload (per post) |
| `mode` | `string` | `"all"` = use all filtered media; `"best_quality"` = sort by resolution and pick highest |
- `output_schema`: standard JSON Schema, `type: "object"`, each property needs `type` and `title` (human-readable Chinese label)

**Prompt variables (whitelist only):**

- `{{content}}` (required), `{{title}}`, `{{author_name}}`, `{{platform}}`, `{{published_at}}`, `{{tags}}`, `{{media_urls}}`

> Do NOT use `{{likes}}`, `{{collects}}`, `{{comments}}`, etc. They are not substituted.

**Prompt quality:** Append a JSON output-format hint to ensure pure JSON response (no markdown code blocks).

**Example:**

```json
{
  "id": "monetization-v1",
  "name": "Monetization Potential",
  "version": "1.0.0",
  "target": "post",
  "needs_media": { "enabled": true, "media_types": ["image"], "max_media": 5, "mode": "all" },
  "prompt": "Analyze the monetization potential of this post.\n\nContent: {{content}}\nAuthor: {{author_name}}\n\n{{media_urls}}\n\nReturn pure JSON only: { \"score\": number, \"category\": string }",
  "output_schema": {
    "type": "object",
    "properties": {
      "score": { "type": "number", "title": "变现潜力评分" },
      "category": { "type": "string", "title": "变现类别" }
    }
  }
}
```

**User confirmation before import:**

Before running `scopai strategy import`, present the `output_schema` to the user in a **table** for confirmation. The table must have two columns:

| 字段名 | 数据类型 |
|--------|----------|
| `title` (Chinese) | `type` (e.g., string, number, boolean, array) |

Only proceed with import after the user explicitly approves. Example presentation:

| 字段名 | 数据类型 |
|--------|----------|
| 变现潜力评分 | number |
| 变现类别 | string |

**Error recovery for strategy import:**

- Validation fails → read exact error, fix field, retry (max 2 retries)
- Same version exists → ask to bump version or change ID
- After approval: `scopai strategy import --json '<json>'`, then `scopai strategy show --id <id>`

---

## Global Rules

1. **Never write temporary polling scripts** looping `scopai task status`. Use built-in `--wait` mode.
2. **Never use direct database access** (e.g., `node -e` scripts opening DuckDB). Always use CLI commands.
3. **Rate limit (429) recovery**: workers auto-requeue with exponential backoff. Only intervene when status becomes `failed` after all retries.
4. **Platform check first**: always `scopai platform list` before `platform add` to avoid "already exists" errors.
5. **Do NOT manually fetch note details before import** (for xhs): let `prepare-data` handle enrichment via the `fetch_note` template. For douyin, search results already contain full data so `fetch_note` can be empty.
6. **Daemon = API server**: `scopai daemon start` launches the unified API server (Fastify + in-process workers). CLI commands communicate via HTTP, not IPC.
