/** 调试 UI 用：压掉 data URL 里极长的 base64，避免运行详情/事件列表不可滚动。 */

const DATA_URL_BASE64 =
  /data:([a-zA-Z0-9.+-]+\/[a-zA-Z0-9.+-]+);base64,([A-Za-z0-9+/=\s\r\n]+)/g

const MIN_BASE64_TO_REDACT = 80

function stripWhitespace(input: string): string {
  return input.replace(/\s+/g, '')
}

function redactBareBase64(input: string): string {
  if (!input || input.length < 32) return input
  const compact = stripWhitespace(input)
  if (compact.length < MIN_BASE64_TO_REDACT) return input
  if (!/^[A-Za-z0-9+/=]+$/.test(compact)) return input
  return `[base64 redacted ~${compact.length} chars]`
}

export function redactDataUrlsInString(input: string): string {
  if (!input || input.length < 32) return input
  const redacted = input.replace(DATA_URL_BASE64, (_full, mime: string, b64: string) => {
    const compact = stripWhitespace(b64)
    if (compact.length < MIN_BASE64_TO_REDACT) {
      return `data:${mime};base64,${b64}`
    }
    return `[data:${mime};base64 redacted ~${compact.length} chars]`
  })
  return redactBareBase64(redacted)
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
