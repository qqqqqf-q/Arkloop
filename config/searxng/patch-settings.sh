#!/bin/sh
# Patch SearXNG settings to enable JSON API format.
# Runs as entrypoint wrapper: patches the settings file, then exec's the real entrypoint.

SETTINGS="${SEARXNG_SETTINGS_PATH:-/etc/searxng/settings.yml}"

if [ -f "$SETTINGS" ] && ! grep -q '^\s*- json' "$SETTINGS"; then
  sed -i '/^  formats:/a\    - json' "$SETTINGS"
fi

exec /usr/local/searxng/entrypoint.sh
