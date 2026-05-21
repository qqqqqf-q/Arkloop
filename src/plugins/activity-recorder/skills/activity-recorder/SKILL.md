---
name: activity-recorder
description: Combine local activity sources such as Screenpipe and ActivityWatch, then preserve durable findings in Memory.
---

# Activity Recorder

Use this skill when running background activity recording or when the user asks what the agent can learn from recent local computer activity.

## Source Skills

Load source-owned skills first:

- `aicontext`
- `catchme`
- `chrome-history`
- `clipboard`
- `screentime`
- `screenpipe-api`
- `screenpipe-health`
- `screenpipe-logs`

If a source has no Arkloop-installed skill, rely on its MCP tool descriptions and any query-example tools it exposes.

## Workflow

1. Check which activity MCP tools are available.
2. Load the source skill before querying that source.
3. Start with recent bounded windows.
4. Use Screenpipe for screen, audio, input, and accessibility context.
5. Use ActivityWatch for app/window/AFK timeline context.
6. Cross-check overlapping facts before writing Memory.

## Memory Rules

Write only durable context to `memory_write`:

- Stable preferences and habits.
- Ongoing projects and decisions.
- Important events with useful future context.
- Clear changes in workflow or priorities.

Do not write Notebook entries. Do not persist raw OCR, raw transcripts, app-switch logs, secrets, one-time codes, or unverified fragments.
