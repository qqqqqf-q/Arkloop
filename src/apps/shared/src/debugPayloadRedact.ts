/** 调试 UI 用：压掉 data URL 里极长的 base64，避免运行详情/事件列表不可滚动。 */

const DATA_URL_BASE64 =
  /data:([a-zA-Z0-9.+-]+\/[a-zA-Z0-9.+-]+);base64,([A-Za-z0-9+/=\s\r\n]+)/g

const MIN_BASE64_TO_REDACT = 80

export function redactDataUrlsInString(input: string): string {
  if (!input || input.length < 32) return input
  return input.replace(DATA_URL_BASE64, (_full, mime: string, b64: string) => {
    const compact = b64.replace(/\s+/g, '')
    if (compact.length < MIN_BASE64_TO_REDACT) {
      return `data:${mime};base64,${b64}`
    }
    return `[data:${mime};base64 redacted ~${compact.length} chars]`
  })
}

export function jsonStringifyForDebugDisplay(value: unknown, space = 2): string {
  const raw = JSON.stringify(
    value,
    (_key, v) => {
      if (typeof v === 'string') return redactDataUrlsInString(v)
      return v
    },
    space,
  )
  return redactDataUrlsInString(typeof raw === 'string' ? raw : String(raw))
}
