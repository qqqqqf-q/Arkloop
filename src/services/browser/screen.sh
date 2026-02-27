#!/usr/bin/env bash
# 开发者工具：快速截图验收
# 用法：./screen.sh <url>
# 示例：./screen.sh https://example.com
#        BROWSER_SERVICE_URL=http://localhost:3100 ./screen.sh https://github.com

URL=${1:?"用法: ./screen.sh <url>"}
BASE=${BROWSER_SERVICE_URL:-http://localhost:3100}
SESSION="dev-screen-$$"

echo "[browser] -> $URL"

if ! RESPONSE=$(curl -sf -X POST "$BASE/v1/navigate" \
  -H "Content-Type: application/json" \
  -H "X-Session-ID: $SESSION" \
  -H "X-Org-ID: dev" \
  -H "X-Run-ID: dev" \
  -d "{\"url\":\"$URL\",\"wait_until\":\"load\"}"); then
  echo "无法连接 Browser Service ($BASE)，请先运行: docker compose up browser minio -d"
  exit 1
fi

SCREENSHOT_URL=$(echo "$RESPONSE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('screenshot_url',''))" 2>/dev/null || true)

if [ -z "$SCREENSHOT_URL" ]; then
  echo "失败："
  echo "$RESPONSE"
  exit 1
fi

PAGE_TITLE=$(echo "$RESPONSE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('page_title',''))" 2>/dev/null || true)
PAGE_URL=$(echo "$RESPONSE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('page_url',''))" 2>/dev/null || true)

echo "Title:      $PAGE_TITLE"
echo "Final URL:  $PAGE_URL"
echo "Screenshot: $SCREENSHOT_URL"

TMP="/tmp/browser-screen-$(date +%s).jpeg"
if ! curl -sf "$SCREENSHOT_URL" -o "$TMP"; then
  echo "截图下载失败，MinIO 地址是否可访问？($SCREENSHOT_URL)"
  exit 1
fi
echo "已保存: $TMP"

case "$(uname -s)" in
  Darwin) open "$TMP" ;;
  Linux)  xdg-open "$TMP" 2>/dev/null || echo "手动打开: $TMP" ;;
esac

# 清理 browser session
curl -sf -X DELETE "$BASE/v1/sessions/$SESSION" \
  -H "X-Session-ID: $SESSION" -H "X-Org-ID: dev" -H "X-Run-ID: dev" > /dev/null 2>&1 || true
