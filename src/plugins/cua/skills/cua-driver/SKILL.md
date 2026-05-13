---
name: cua-driver
description: Drive real macOS applications through the CUA Driver MCP server when the user asks to inspect, operate, or automate visible desktop UI.
platforms:
  - darwin
---

# CUA Driver

Use the CUA Driver MCP tools as the only action surface for desktop automation. Do not use shell, AppleScript, `open`, `cliclick`, screenshots outside CUA, or raw cursor/key APIs to drive macOS UI.

## Runtime Context

- Plugin id: `arkloop.plugins.cua`.
- Runtime helper app: `CuaDriver.app`.
- Runtime binary: `CuaDriver.app/Contents/MacOS/cua-driver`.
- Accessibility and Screen Recording permissions belong to the helper app bundle, not to Arkloop.

## Required Loop

1. Resolve the app with `list_apps`. Match localized names, English names, romanized names, bundle identifiers, and common abbreviations. Prefer `bundle_id` as the stable identity.
2. Start or reuse the target with `launch_app({ bundle_id })`. Use the returned `pid` when available.
3. Inspect windows with `list_windows({ pid })` when the launch result lacks a usable window.
4. Before every UI action, snapshot with `get_window_state({ pid, window_id })`.
5. Act with the narrowest matching tool: `click`, `right_click`, `double_click`, `drag`, `scroll`, `type_text`, `type_text_chars`, `press_key`, `hotkey`, `set_value`, `page`, or `launch_app` with `urls`.
6. Snapshot again after every state-changing action and verify visible evidence such as selected state, changed text, playback progress, new panels, highlighted rows, or updated window content.

Element indices are valid only for the latest `get_window_state` result from the same `pid` and `window_id`. Re-snapshot when an index is missing, stale, or from another window.

## Permissions

Call `check_permissions` before the first desktop task. If Accessibility or Screen Recording is missing, tell the user to grant both permissions to `CuaDriver.app`, then stop until the permission state changes.

## Sparse UI Fallback

Some media, browser, and Electron apps expose shallow accessibility trees while still showing actionable pixels. Use this order:

1. Retry `get_window_state({ pid, window_id })` once.
2. For browser-like or Electron windows, use `page` or relaunch with `launch_app({ bundle_id, electron_debugging_port: 9222 })` when DOM access is more reliable than pixels.
3. Use `screenshot({ window_id })` for broad visual confirmation when the current overlay or window content is unclear.
4. Use at most one `zoom({ pid, window_id, x1, y1, x2, y2 })` for dense text or icons.
5. Use pixel coordinates only from the latest full-window screenshot or the single zoom result.
6. Re-snapshot and compare after each action.

Ask the user only when visible candidates are ambiguous, the requested action is destructive, or the target is outside the visible window.

## Navigation Patterns

- Launch apps with `launch_app({ bundle_id })`.
- Open files or URLs with `launch_app({ bundle_id, urls: [...] })`.
- For browser-like apps, prefer separate windows via `launch_app({ bundle_id, urls: [...] })` so each target has a stable `window_id`.
- Do not use omnibox shortcuts such as `cmd+l` for navigation. Use `launch_app` with `urls`.
- Use visible in-window controls before menu-bar actions. Use menu-bar actions only when the target app is already frontmost and the menu state is visible through the CUA snapshot.

## Agent Cursor

Use `get_agent_cursor_state` to inspect the cursor overlay. Use `set_agent_cursor_enabled`, `set_agent_cursor_motion`, or `set_agent_cursor_style` only when the user asks to show, hide, animate, or restyle the agent cursor.
