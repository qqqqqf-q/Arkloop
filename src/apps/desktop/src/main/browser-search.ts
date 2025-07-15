import * as http from 'http'
import * as net from 'net'
import { BrowserWindow, session } from 'electron'

export type BrowserSearchResult = {
  title: string
  url: string
  snippet: string
}

type SearchRequest = {
  query: string
  maxResults: number
  engine?: 'duckduckgo' | 'bing'
}

type SearchResponse =
  | { ok: true; results: BrowserSearchResult[] }
  | { ok: false; error: string; captcha?: boolean }

const SEARCH_TIMEOUT_MS = 20_000
const RESULT_POLL_INTERVAL_MS = 300

// JS injected into the search page to extract result links.
// DuckDuckGo HTML layout uses <a class="result__a"> for result links,
// and nearby <a class="result__snippet"> for snippets.
const DDG_EXTRACT_SCRIPT = `
(function() {
  var items = [];
  var links = document.querySelectorAll('a.result__a');
  for (var i = 0; i < links.length && items.length < 20; i++) {
    var a = links[i];
    var href = a.href || '';
    var title = (a.textContent || '').trim();
    if (!href || !title) continue;
    if (href.startsWith('https://duckduckgo.com') || href.startsWith('javascript:')) continue;
    var parent = a.closest('.result');
    var snippetEl = parent ? parent.querySelector('.result__snippet') : null;
    var snippet = snippetEl ? (snippetEl.textContent || '').trim() : '';
    items.push({ title: title, url: href, snippet: snippet });
  }
  return JSON.stringify(items);
})()
`

// Bing fallback extraction
const BING_EXTRACT_SCRIPT = `
(function() {
  var items = [];
  var links = document.querySelectorAll('#b_results .b_algo h2 a');
  for (var i = 0; i < links.length && items.length < 20; i++) {
    var a = links[i];
    var href = a.href || '';
    var title = (a.textContent || '').trim();
    if (!href || !title) continue;
    var li = a.closest('li.b_algo');
    var snippetEl = li ? li.querySelector('.b_caption p') : null;
    var snippet = snippetEl ? (snippetEl.textContent || '').trim() : '';
    items.push({ title: title, url: href, snippet: snippet });
  }
  return JSON.stringify(items);
})()
`

function buildSearchURL(query: string, engine: 'duckduckgo' | 'bing'): string {
  const q = encodeURIComponent(query)
  if (engine === 'bing') return `https://www.bing.com/search?q=${q}`
  return `https://duckduckgo.com/?q=${q}&ia=web`
}

async function extractSearchResults(
  win: BrowserWindow,
  engine: 'duckduckgo' | 'bing',
  maxResults: number,
): Promise<BrowserSearchResult[]> {
  const script = engine === 'bing' ? BING_EXTRACT_SCRIPT : DDG_EXTRACT_SCRIPT
  const deadline = Date.now() + SEARCH_TIMEOUT_MS

  while (Date.now() < deadline) {
    try {
      const raw = await win.webContents.executeJavaScript(script)
      if (typeof raw === 'string') {
        const parsed: BrowserSearchResult[] = JSON.parse(raw)
        if (parsed.length > 0) {
          return parsed.slice(0, maxResults)
        }
      }
    } catch {
      // Page still loading — keep polling
    }
    await new Promise<void>((r) => setTimeout(r, RESULT_POLL_INTERVAL_MS))
  }

  return []
}

async function detectCaptcha(win: BrowserWindow): Promise<boolean> {
  try {
    const result = await win.webContents.executeJavaScript(`
      (function() {
        var text = document.body ? document.body.innerText : '';
        return text.includes('CAPTCHA') || text.includes('captcha') ||
               text.includes('robot') || text.includes('unusual traffic') ||
               document.querySelector('[id*="captcha"], [class*="captcha"]') !== null;
      })()
    `)
    return result === true
  } catch {
    return false
  }
}

async function performBrowserSearch(
  query: string,
  maxResults: number,
  engine: 'duckduckgo' | 'bing' = 'duckduckgo',
): Promise<SearchResponse> {
  const searchSession = session.fromPartition('persist:browser-search', { cache: true })

  const win = new BrowserWindow({
    show: false,
    width: 1024,
    height: 768,
    webPreferences: {
      session: searchSession,
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
      javascript: true,
    },
  })

  try {
    const url = buildSearchURL(query, engine)

    const loadPromise = new Promise<void>((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error('page load timeout')),
        SEARCH_TIMEOUT_MS,
      )
      win.webContents.once('did-finish-load', () => {
        clearTimeout(timer)
        resolve()
      })
      win.webContents.once('did-fail-load', (_e, code, desc) => {
        clearTimeout(timer)
        reject(new Error(`page load failed: ${desc} (${code})`))
      })
    })

    win.loadURL(url, {
      userAgent:
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 '
        + '(KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36',
    })

    await loadPromise

    if (await detectCaptcha(win)) {
      return { ok: false, error: 'captcha_detected', captcha: true }
    }

    const results = await extractSearchResults(win, engine, maxResults)

    if (results.length === 0 && engine === 'duckduckgo') {
      // Retry with Bing on empty results
      win.destroy()
      return performBrowserSearch(query, maxResults, 'bing')
    }

    return { ok: true, results }
  } catch (err) {
    return {
      ok: false,
      error: err instanceof Error ? err.message : String(err),
    }
  } finally {
    if (!win.isDestroyed()) win.destroy()
  }
}

function findAvailablePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = net.createServer()
    server.listen(0, '127.0.0.1', () => {
      const addr = server.address()
      if (!addr || typeof addr === 'string') {
        server.close(() => reject(new Error('could not get port')))
        return
      }
      const port = addr.port
      server.close(() => resolve(port))
    })
    server.on('error', reject)
  })
}

let callbackServer: http.Server | null = null
let callbackPort: number | null = null

export async function startBrowserSearchServer(): Promise<string> {
  if (callbackServer && callbackPort) {
    return `127.0.0.1:${callbackPort}`
  }

  const port = await findAvailablePort()

  callbackServer = http.createServer((req, res) => {
    if (req.method !== 'POST' || req.url !== '/browser-search') {
      res.writeHead(404)
      res.end()
      return
    }

    const chunks: Buffer[] = []
    req.on('data', (c: Buffer) => chunks.push(c))
    req.on('end', () => {
      let body: SearchRequest
      try {
        body = JSON.parse(Buffer.concat(chunks).toString()) as SearchRequest
      } catch {
        res.writeHead(400)
        res.end(JSON.stringify({ ok: false, error: 'invalid_json' }))
        return
      }

      if (!body.query || typeof body.query !== 'string') {
        res.writeHead(400)
        res.end(JSON.stringify({ ok: false, error: 'missing_query' }))
        return
      }

      const maxResults = typeof body.maxResults === 'number' && body.maxResults > 0
        ? Math.min(body.maxResults, 20)
        : 10

      const engine =
        body.engine === 'bing' || body.engine === 'duckduckgo' ? body.engine : 'duckduckgo'

      performBrowserSearch(body.query, maxResults, engine)
        .then((result: SearchResponse) => {
          res.writeHead(200, { 'Content-Type': 'application/json' })
          res.end(JSON.stringify(result))
        })
        .catch((err: unknown) => {
          res.writeHead(500)
          res.end(JSON.stringify({
            ok: false,
            error: err instanceof Error ? err.message : 'internal_error',
          }))
        })
    })
  })

  await new Promise<void>((resolve, reject) => {
    callbackServer!.listen(port, '127.0.0.1', () => resolve())
    callbackServer!.on('error', reject)
  })

  callbackPort = port
  return `127.0.0.1:${port}`
}

export function stopBrowserSearchServer(): void {
  callbackServer?.close()
  callbackServer = null
  callbackPort = null
}

export function getBrowserSearchCallbackAddr(): string | null {
  return callbackPort ? `127.0.0.1:${callbackPort}` : null
}
