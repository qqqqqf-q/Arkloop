import { BrowserWindow } from 'electron'
import * as http from 'http'
import * as net from 'net'

type BrowserSearchResult = {
  title: string
  url: string
  snippet: string
}

type BrowserSearchPayload = {
  results: BrowserSearchResult[]
  final_url: string
}

type BrowserSearchState = {
  readyState: string
  title: string
  finalUrl: string
  challenge: boolean
  results: BrowserSearchResult[]
}

type BrowserSearchServerState = {
  server: http.Server
  baseUrl: string
  token: string
}

type SearchLocale = {
  acceptLanguage: string
}

const SEARCH_TIMEOUT_MS = 25_000
const SEARCH_POLL_MS = 250
const DEFAULT_MAX_RESULTS = 5
const MAX_RESULTS = 50
const SEARCH_PARTITION = 'persist:arkloop-search'

let serverState: BrowserSearchServerState | null = null

export async function ensureBrowserSearchServer(token: string): Promise<string> {
  const cleanToken = token.trim()
  if (serverState?.token === cleanToken) {
    return serverState.baseUrl
  }
  if (serverState) {
    await closeBrowserSearchServer()
  }

  const server = http.createServer((req, res) => {
    void handleBrowserSearchRequest(req, res, cleanToken)
  })
  const baseUrl = await listenOnLoopback(server)
  serverState = { server, baseUrl, token: cleanToken }
  return baseUrl
}

export function getBrowserSearchBaseUrl(): string | null {
  return serverState?.baseUrl ?? null
}

export async function closeBrowserSearchServer(): Promise<void> {
  const current = serverState
  serverState = null
  if (!current) return
  await new Promise<void>((resolve) => {
    current.server.close(() => resolve())
  })
}

async function handleBrowserSearchRequest(req: http.IncomingMessage, res: http.ServerResponse, token: string): Promise<void> {
  if (req.method !== 'GET' || !req.url) {
    writeJSON(res, 405, { error: 'method_not_allowed' })
    return
  }
  if (token && req.headers.authorization !== `Bearer ${token}`) {
    writeJSON(res, 401, { error: 'unauthorized' })
    return
  }

  const requestURL = new URL(req.url, 'http://127.0.0.1')
  if (requestURL.pathname !== '/search') {
    writeJSON(res, 404, { error: 'not_found' })
    return
  }

  const query = (requestURL.searchParams.get('q') ?? '').trim()
  if (!query) {
    writeJSON(res, 400, { error: 'query_required' })
    return
  }
  const maxResults = normalizeMaxResults(requestURL.searchParams.get('max_results'))

  try {
    const payload = await searchStartpageInBrowser(query, maxResults)
    writeJSON(res, 200, payload)
  } catch (error) {
    const message = error instanceof Error ? error.message : 'search failed'
    writeJSON(res, message.includes('timed out') ? 504 : 502, {
      error: 'browser_search_failed',
      message,
    })
  }
}

async function searchStartpageInBrowser(query: string, maxResults: number): Promise<BrowserSearchPayload> {
  const win = new BrowserWindow({
    show: false,
    width: 1280,
    height: 900,
    webPreferences: {
      partition: SEARCH_PARTITION,
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      backgroundThrottling: false,
    },
  })

  try {
    win.webContents.setWindowOpenHandler(() => ({ action: 'deny' }))
    win.webContents.setUserAgent(browserSearchUserAgent())

    const locale = browserSearchLocale(query)
    await withTimeout(
      win.loadURL(buildStartpageSearchURL(query), {
        extraHeaders: `Accept-Language: ${locale.acceptLanguage}\n`,
      }),
      SEARCH_TIMEOUT_MS,
      'browser search load timed out',
    )
    return await waitForBrowserResults(win, maxResults)
  } finally {
    if (!win.isDestroyed()) {
      win.destroy()
    }
  }
}

async function waitForBrowserResults(win: BrowserWindow, maxResults: number): Promise<BrowserSearchPayload> {
  const deadline = Date.now() + SEARCH_TIMEOUT_MS
  let lastState: BrowserSearchState | null = null

  while (Date.now() < deadline) {
    const state = await readBrowserSearchState(win)
    lastState = state
    if (state.results.length > 0) {
      return {
        results: state.results.slice(0, maxResults),
        final_url: state.finalUrl,
      }
    }
    if (state.challenge) {
      throw new Error('browser search returned a challenge page')
    }
    await delay(SEARCH_POLL_MS)
  }

  const suffix = lastState?.title ? ` (${lastState.title})` : ''
  throw new Error(`browser search timed out${suffix}`)
}

async function readBrowserSearchState(win: BrowserWindow): Promise<BrowserSearchState> {
  const value = await win.webContents.executeJavaScript(`
(() => {
  const clean = (value) => String(value || '').replace(/\\s+/g, ' ').trim();
  const textFrom = (element) => {
    if (!element) return '';
    const clone = element.cloneNode(true);
    clone.querySelectorAll('style, script, noscript, svg').forEach((item) => item.remove());
    return clean(clone.textContent);
  };
  const toURL = (value) => {
    try { return new URL(value, location.href).toString(); } catch { return ''; }
  };
  const resultNodes = Array.from(document.querySelectorAll('[data-testid="web-result"], .result, li.b_algo'));
  const results = resultNodes.map((node) => {
    const anchor = node.querySelector('a.result-title[href], h2 a[href], a[href]');
    const titleNode = anchor ? anchor.querySelector('h1, h2, h3, .wgl-title, [data-testid="result-title"]') || anchor : null;
    const snippetNode = node.querySelector(
      '[data-testid="result-description"], .result-description, .wgl-description, .description, .b_caption p, .b_snippet, p'
    );
    return {
      title: textFrom(titleNode),
      url: toURL(anchor ? anchor.getAttribute('href') : ''),
      snippet: textFrom(snippetNode),
    };
  }).filter((item) => item.title && item.url);
  const challengeNode = document.querySelector(
    'iframe[src*="challenges.cloudflare.com"], script[src*="challenges.cloudflare.com"], #b_captcha, [id*="captcha"], [class*="captcha"]'
  );
  const bodyText = clean(document.body ? document.body.innerText : '').toLowerCase();
  return {
    readyState: document.readyState,
    title: document.title || '',
    finalUrl: location.href,
    challenge: !!challengeNode || bodyText.includes('verify you are human') || bodyText.includes('unusual traffic'),
    results,
  };
})()
`, true) as unknown
  return normalizeBrowserSearchState(value)
}

function normalizeBrowserSearchState(value: unknown): BrowserSearchState {
  const record = isRecord(value) ? value : {}
  const resultsRaw = Array.isArray(record.results) ? record.results : []
  return {
    readyState: typeof record.readyState === 'string' ? record.readyState : '',
    title: typeof record.title === 'string' ? record.title : '',
    finalUrl: typeof record.finalUrl === 'string' ? record.finalUrl : '',
    challenge: record.challenge === true,
    results: resultsRaw.map(normalizeBrowserSearchResult).filter((item): item is BrowserSearchResult => item !== null),
  }
}

function normalizeBrowserSearchResult(value: unknown): BrowserSearchResult | null {
  if (!isRecord(value)) return null
  const title = typeof value.title === 'string' ? normalizeInlineText(value.title, 160) : ''
  const url = typeof value.url === 'string' ? value.url.trim() : ''
  const snippet = typeof value.snippet === 'string' ? normalizeInlineText(value.snippet, 320) : ''
  if (!title || !url) return null
  return { title, url, snippet }
}

function buildStartpageSearchURL(query: string): string {
  const url = new URL('https://www.startpage.com/sp/search')
  url.searchParams.set('query', query)
  return url.toString()
}

function browserSearchLocale(query: string): SearchLocale {
  if (/\p{Script=Han}/u.test(query)) {
    return {
      acceptLanguage: 'zh-CN,zh;q=0.9,en-US;q=0.7,en;q=0.6',
    }
  }
  if (/[\p{Script=Hiragana}\p{Script=Katakana}]/u.test(query)) {
    return {
      acceptLanguage: 'ja-JP,ja;q=0.9,en-US;q=0.7,en;q=0.6',
    }
  }
  if (/\p{Script=Hangul}/u.test(query)) {
    return {
      acceptLanguage: 'ko-KR,ko;q=0.9,en-US;q=0.7,en;q=0.6',
    }
  }
  return {
    acceptLanguage: 'en-US,en;q=0.9',
  }
}

function browserSearchUserAgent(): string {
  const chrome = process.versions.chrome || '120.0.0.0'
  switch (process.platform) {
    case 'darwin':
      return `Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/${chrome} Safari/537.36`
    case 'win32':
      return `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/${chrome} Safari/537.36`
    default:
      return `Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/${chrome} Safari/537.36`
  }
}

function normalizeMaxResults(value: string | null): number {
  if (!value) return DEFAULT_MAX_RESULTS
  const parsed = Number.parseInt(value, 10)
  if (!Number.isInteger(parsed) || parsed <= 0) return DEFAULT_MAX_RESULTS
  return Math.min(parsed, MAX_RESULTS)
}

function normalizeInlineText(value: string, maxChars: number): string {
  const cleaned = value.trim().replace(/\s+/g, ' ')
  return cleaned.length > maxChars ? cleaned.slice(0, maxChars) : cleaned
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object'
}

function listenOnLoopback(server: http.Server): Promise<string> {
  return new Promise((resolve, reject) => {
    const onError = (error: Error): void => {
      server.off('error', onError)
      reject(error)
    }
    server.once('error', onError)
    server.listen(0, '127.0.0.1', () => {
      server.off('error', onError)
      const address = server.address()
      if (!isAddressInfo(address)) {
        reject(new Error('browser search server address unavailable'))
        return
      }
      resolve(`http://127.0.0.1:${address.port}`)
    })
  })
}

function isAddressInfo(address: string | net.AddressInfo | null): address is net.AddressInfo {
  return !!address && typeof address === 'object' && typeof address.port === 'number'
}

function writeJSON(res: http.ServerResponse, status: number, payload: unknown): void {
  res.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Cache-Control': 'no-store',
  })
  res.end(JSON.stringify(payload))
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

async function withTimeout<T>(promise: Promise<T>, timeoutMs: number, message: string): Promise<T> {
  let timer: NodeJS.Timeout | null = null
  try {
    return await Promise.race([
      promise,
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error(message)), timeoutMs)
      }),
    ])
  } finally {
    if (timer) clearTimeout(timer)
  }
}
