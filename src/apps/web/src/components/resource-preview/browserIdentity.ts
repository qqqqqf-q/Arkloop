export function normalizeBrowserUrl(value: string): string | null {
  const raw = value.trim()
  if (!raw) return null
  const withScheme = /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//.test(raw) ? raw : `http://${raw}`
  try {
    const parsed = new URL(withScheme)
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return null
    return parsed.toString()
  } catch {
    return null
  }
}

function decodePunycode(input: string): string {
  const base = 36
  const tMin = 1
  const tMax = 26
  const skew = 38
  const damp = 700
  const initialBias = 72
  const initialN = 128
  const delimiter = '-'

  const adapt = (delta: number, numPoints: number, firstTime: boolean): number => {
    delta = firstTime ? Math.floor(delta / damp) : delta >> 1
    delta += Math.floor(delta / numPoints)
    let k = 0
    while (delta > Math.floor(((base - tMin) * tMax) / 2)) {
      delta = Math.floor(delta / (base - tMin))
      k += base
    }
    return k + Math.floor(((base - tMin + 1) * delta) / (delta + skew))
  }

  const digit = (codePoint: number): number => {
    if (codePoint >= 48 && codePoint <= 57) return codePoint - 22
    if (codePoint >= 65 && codePoint <= 90) return codePoint - 65
    if (codePoint >= 97 && codePoint <= 122) return codePoint - 97
    return base
  }

  const output: number[] = []
  const basic = input.lastIndexOf(delimiter)
  if (basic >= 0) {
    for (let i = 0; i < basic; i += 1) output.push(input.charCodeAt(i))
  }
  let index = basic >= 0 ? basic + 1 : 0
  let n = initialN
  let i = 0
  let bias = initialBias

  while (index < input.length) {
    const oldI = i
    let w = 1
    for (let k = base; ; k += base) {
      if (index >= input.length) return input
      const d = digit(input.charCodeAt(index))
      index += 1
      if (d >= base) return input
      i += d * w
      const t = k <= bias ? tMin : k >= bias + tMax ? tMax : k - bias
      if (d < t) break
      w *= base - t
    }
    const length = output.length + 1
    bias = adapt(i - oldI, length, oldI === 0)
    n += Math.floor(i / length)
    i %= length
    output.splice(i, 0, n)
    i += 1
  }

  return String.fromCodePoint(...output)
}

export function displayBrowserHostname(hostname: string): string {
  return hostname
    .split('.')
    .map((label) => label.startsWith('xn--') ? decodePunycode(label.slice(4)) : label)
    .join('.')
}

export function displayBrowserUrl(url: string): string {
  try {
    const parsed = new URL(url)
    const host = `${displayBrowserHostname(parsed.hostname)}${parsed.port ? `:${parsed.port}` : ''}`
    return `${parsed.protocol}//${host}${parsed.pathname}${parsed.search}${parsed.hash}`
  } catch {
    return url
  }
}

export function browserTitleFromUrl(url: string): string {
  try {
    const parsed = new URL(url)
    return displayBrowserHostname(parsed.hostname) || url
  } catch {
    return url
  }
}

export function browserFaviconUrl(url: string): string | undefined {
  try {
    const parsed = new URL(url)
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return undefined
    return `https://www.google.com/s2/favicons?domain=${encodeURIComponent(parsed.hostname)}&sz=32`
  } catch {
    return undefined
  }
}
