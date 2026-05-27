import { useRef, useEffect, useCallback, useState } from 'react'
import { AppBridge, PostMessageTransport } from '@modelcontextprotocol/ext-apps/app-bridge'
import type { CallToolResult } from '@modelcontextprotocol/sdk/types.js'
import type { McpAppCsp } from '../storage'
import { apiBaseUrl } from '@arkloop/shared/api'
import { useTheme } from '../contexts/ThemeContext'
import { useLocale } from '../contexts/LocaleContext'
import { isDesktop } from '@arkloop/shared/desktop'

function buildCSP(csp?: McpAppCsp): string {
  const resourceDomains = csp?.resourceDomains ?? []
  const connectDomains = csp?.connectDomains ?? []
  const frameDomains = csp?.frameDomains ?? []
  const baseUriDomains = csp?.baseUriDomains ?? []

  const resourceSrc = resourceDomains.join(' ')

  return [
    "default-src 'none'",
    `script-src 'self' 'unsafe-inline' ${resourceSrc}`,
    `style-src 'self' 'unsafe-inline' ${resourceSrc}`,
    `img-src 'self' data: blob: ${resourceSrc}`,
    `font-src 'self' ${resourceSrc}`,
    `media-src 'self' data: ${resourceSrc}`,
    frameDomains.length > 0 ? `frame-src ${frameDomains.join(' ')}` : "frame-src 'none'",
    connectDomains.length > 0 ? `connect-src ${connectDomains.join(' ')}` : "connect-src 'none'",
    baseUriDomains.length > 0 ? `base-uri ${baseUriDomains.join(' ')}` : "base-uri 'self'",
    "object-src 'none'",
  ].filter(Boolean).join('; ')
}

function isFullHtmlDocument(content: string): boolean {
  const trimmed = content.trim().toLowerCase()
  return trimmed.startsWith('<!doctype') || trimmed.startsWith('<html')
}

function injectIntoHead(html: string, injections: string): string {
  const headEnd = html.indexOf('</head>')
  if (headEnd >= 0) {
    return html.slice(0, headEnd) + injections + html.slice(headEnd)
  }
  // fallback: inject after <html> tag
  const htmlMatch = html.match(/<html[^>]*>/i)
  if (htmlMatch) {
    const idx = html.indexOf(htmlMatch[0]) + htmlMatch[0].length
    return html.slice(0, idx) + '\n<head>' + injections + '</head>' + html.slice(idx)
  }
  return injections + html
}

function buildIframeSrcDoc(content: string, csp: McpAppCsp | undefined): string {
  const themeCSS = buildThemeCSS()
  const cspStr = buildCSP(csp)

  if (isFullHtmlDocument(content)) {
    // Content is a full HTML doc — inject our additions into its <head>
    const injections = `
<style id="arkloop-theme-vars">
${themeCSS}
</style>
<script type="importmap">
{
  "imports": {
    "@modelcontextprotocol/ext-apps": "/mcp-ext-apps/app-with-deps.js"
  }
}
</script>
<script>
(function() {
  var resizeTimer;
  var lastReported = 0;
  function notifyHeight() {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function() {
      var h = document.body.scrollHeight || 0;
      var reported = Math.ceil(h);
      if (Math.abs(reported - lastReported) <= 5) return;
      lastReported = reported;
      window.parent.postMessage({ type: 'arkloop:mcpapp:resize', height: reported }, '*');
    }, 200);
  }
  new MutationObserver(notifyHeight).observe(document.body, { childList: true, subtree: true, attributes: true });
  window.addEventListener('load', notifyHeight);
})();
</script>`
    let result = injectIntoHead(content, injections)
    // Add CSP meta if not present
    if (!result.includes('Content-Security-Policy')) {
      result = injectIntoHead(result, `<meta http-equiv="Content-Security-Policy" content="${cspStr}">`)
    }
    return result
  }

  // Content is a fragment — wrap in template
  return IFRAME_HTML_TEMPLATE(themeCSS, content, cspStr)
}

const IFRAME_HTML_TEMPLATE = (themeCSS: string, content: string, csp: string) => `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<meta http-equiv="Content-Security-Policy" content="${csp}">` + `
<style>
  * { box-sizing: border-box; }
  html { background: transparent; overflow-x: hidden; }
  body { margin: 0; padding: 0; background: transparent; overflow-x: hidden; }
</style>
<style id="arkloop-theme-vars">
${themeCSS}
</style>
<script type="importmap">
{
  "imports": {
    "@modelcontextprotocol/ext-apps": "/mcp-ext-apps/app-with-deps.js"
  }
}
</script>
</head>
<body>
${content}
<script>
(function() {
  var resizeTimer;
  var lastReported = 0;
  function notifyHeight() {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function() {
      var h = document.body.scrollHeight || 0;
      var reported = Math.ceil(h);
      if (Math.abs(reported - lastReported) <= 5) return;
      lastReported = reported;
      window.parent.postMessage({ type: 'arkloop:mcpapp:resize', height: reported }, '*');
    }, 200);
  }
  new MutationObserver(notifyHeight).observe(document.body, { childList: true, subtree: true, attributes: true });
  window.addEventListener('load', notifyHeight);
})();
</script>
</body>
</html>`

function buildThemeCSS(): string {
  if (typeof document === 'undefined') return ''
  const root = document.documentElement
  const vars: string[] = []
  for (let i = 0; i < root.style.length; i++) {
    const name = root.style.item(i)
    if (name.startsWith('--c-')) {
      vars.push(`  ${name}: ${root.style.getPropertyValue(name)};`)
    }
  }
  // fallback: read computed styles for known variables
  const computed = getComputedStyle(root)
  const knownVars = [
    '--c-bg-page', '--c-bg-sub', '--c-text-primary', '--c-text-secondary',
    '--c-border', '--c-border-subtle', '--c-status-error', '--c-status-success',
  ]
  for (const name of knownVars) {
    if (!vars.some((v) => v.includes(name))) {
      const value = computed.getPropertyValue(name)
      if (value) vars.push(`  ${name}: ${value};`)
    }
  }
  return `:root {\n` + vars.join('\n') + `\n}`
}

type Props = {
  uri: string
  content: string
  toolOutput?: unknown
  csp?: McpAppCsp
  serverId?: string
  name?: string
  accessToken?: string
  onOpenLink?: (url: string) => void
  onSendMessage?: (text: string) => void
  style?: React.CSSProperties
  className?: string
  displayMode?: 'inline' | 'fullscreen'
  onExpand?: () => void
  hideHeader?: boolean
  noBorder?: boolean
  toolName?: string
  toolInput?: Record<string, unknown>
  toolCancelled?: boolean
  toolCancelReason?: string
}

function toCallToolResult(output: unknown): CallToolResult {
  if (output && typeof output === 'object') {
    const o = output as Record<string, unknown>
    if (Array.isArray(o.content)) {
      return o as CallToolResult
    }
    if (o.result && typeof o.result === 'object') {
      const result = o.result as Record<string, unknown>
      if (Array.isArray(result.content)) {
        return result as CallToolResult
      }
    }
  }
  const text = typeof output === 'string' ? output : JSON.stringify(output)
  return { content: [{ type: 'text', text }] }
}

function getResolvedTheme(theme: 'system' | 'light' | 'dark'): 'light' | 'dark' {
  if (theme === 'dark') return 'dark'
  if (theme === 'light') return 'light'
  if (typeof window !== 'undefined' && window.matchMedia) {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }
  return 'light'
}

export function McpAppIframe({
  uri,
  content,
  toolOutput,
  csp,
  serverId,
  name,
  accessToken,
  onOpenLink,
  onSendMessage,
  style,
  className,
  displayMode = 'inline',
  onExpand,
  hideHeader,
  noBorder,
  toolName,
  toolInput,
  toolCancelled,
  toolCancelReason,
}: Props) {
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const bridgeRef = useRef<AppBridge | null>(null)
  const pendingToolResultRef = useRef<unknown>(undefined)
  const isConnectedRef = useRef(false)
  const lastHeightRef = useRef<number>(0)
  const [iframeHeight, setIframeHeight] = useState<number | undefined>(undefined)
  const { theme: themeMode } = useTheme()
  const { locale } = useLocale()

  // Rebuild iframe HTML when content or theme changes
  const [srcDoc, setSrcDoc] = useState(() => buildIframeSrcDoc(content, csp))

  const sendToolResult = useCallback((bridge: AppBridge, output: unknown) => {
    if (output === undefined) return
    try {
      bridge.sendToolResult(toCallToolResult(output))
    } catch (err) {
      console.error('[McpAppIframe] sendToolResult failed:', err)
    }
  }, [])

  // Handle toolOutput prop changes
  useEffect(() => {
    if (isConnectedRef.current && bridgeRef.current) {
      sendToolResult(bridgeRef.current, toolOutput)
    } else {
      pendingToolResultRef.current = toolOutput
    }
  }, [toolOutput, sendToolResult])

  // Connect AppBridge when iframe loads (guarantees all scripts are ready)
  const handleLoad = useCallback(async () => {
    const iframe = iframeRef.current
    if (!iframe?.contentWindow) return

    // Close previous bridge if any
    const prevBridge = bridgeRef.current
    if (prevBridge) {
      bridgeRef.current = null
      isConnectedRef.current = false
      prevBridge.close().catch(() => {})
    }

    // Reset height tracking on each iframe reload
    lastHeightRef.current = 0
    setIframeHeight(undefined)

    const transport = new PostMessageTransport(
      iframe.contentWindow,
      iframe.contentWindow,
    )

    const resolvedTheme = getResolvedTheme(themeMode)
    const resolvedDisplayMode = displayMode ?? 'inline'

    const bridge = new AppBridge(
      null,
      { name: 'arkloop', version: '1.0.0' },
      {
        openLinks: {},
        downloadFile: {},
        serverTools: { listChanged: true },
        serverResources: { listChanged: true },
        logging: {},
        sandbox: {
          csp: {
            connectDomains: csp?.connectDomains,
            resourceDomains: csp?.resourceDomains,
            frameDomains: csp?.frameDomains,
            baseUriDomains: csp?.baseUriDomains,
          },
        },
        message: { text: {} },
      },
      {
        hostContext: {
          displayMode: resolvedDisplayMode,
          availableDisplayModes: ['inline', 'fullscreen', 'pip'],
          theme: resolvedTheme,
          locale,
          platform: isDesktop() ? 'desktop' : 'web',
          userAgent: typeof navigator !== 'undefined' ? navigator.userAgent : undefined,
          timeZone: typeof Intl !== 'undefined' ? Intl.DateTimeFormat().resolvedOptions().timeZone : undefined,
          containerDimensions: { maxHeight: 2000 },
          toolInfo: toolName ? { tool: { name: toolName, inputSchema: { type: 'object' } } } : undefined,
        },
      },
    )

    bridge.onrequestdisplaymode = async (request) => {
      if (request.mode === resolvedDisplayMode) {
        return { mode: resolvedDisplayMode }
      }
      if (request.mode === 'fullscreen' && onExpand) {
        onExpand()
        return { mode: 'fullscreen' }
      }
      if (request.mode === 'pip') {
        // pip is not supported in this host; keep current mode
        return { mode: resolvedDisplayMode }
      }
      return { mode: resolvedDisplayMode }
    }

    bridge.onsizechange = ({ height }) => {
      if (height != null && height > 0) {
        const newHeight = Math.min(height, 2000)
        if (Math.abs(newHeight - lastHeightRef.current) > 10) {
          lastHeightRef.current = newHeight
          setIframeHeight(newHeight)
        }
      }
    }

    bridge.onopenlink = async (request) => {
      onOpenLink?.(request.url)
      return { success: true }
    }

    bridge.oncalltool = async (request) => {
      if (!serverId) {
        throw new Error('MCP server ID not available for tool calling')
      }
      const headers: Record<string, string> = { 'Content-Type': 'application/json' }
      if (accessToken) {
        headers['Authorization'] = `Bearer ${accessToken}`
      }
      const resp = await fetch(`${apiBaseUrl()}/v1/mcp/tool-call`, {
        method: 'POST',
        headers,
        body: JSON.stringify({
          server_id: serverId,
          tool_name: request.name,
          arguments: request.arguments ?? {},
        }),
      })
      if (!resp.ok) {
        const errorText = await resp.text().catch(() => resp.statusText)
        throw new Error(`MCP tool call failed: ${resp.status} ${errorText}`)
      }
      const result = await resp.json() as { content: Array<{ type: string; text?: string }>; isError?: boolean }
      const content: CallToolResult['content'] = result.content.map((item) => {
        if (item.type === 'text') return { type: 'text' as const, text: item.text ?? '' }
        return item as CallToolResult['content'][number]
      })
      return {
        content,
        isError: result.isError ?? false,
      }
    }

    bridge.onmessage = async (params) => {
      if (!onSendMessage) {
        return { isError: true }
      }
      const parts: string[] = []
      for (const block of params.content) {
        if (block && typeof block === 'object' && 'type' in block) {
          if (block.type === 'text' && 'text' in block && typeof block.text === 'string') {
            parts.push(block.text)
          }
        }
      }
      const text = parts.join('\n').trim()
      if (!text) {
        return { isError: true }
      }
      onSendMessage(text)
      return { isError: false }
    }

    bridge.onrequestteardown = async () => {
      onExpand?.()
      return {}
    }

      bridge.onupdatemodelcontext = async (params) => {
      console.log('[McpAppIframe] update-model-context:', params)
      return {}
    }

    bridge.onloggingmessage = async (params) => {
      console.log('[MCP App]', params)
      return {}
    }

    bridge.ondownloadfile = async ({ contents }) => {
      try {
        for (const item of contents) {
          if (item.type === 'resource') {
            const res = item.resource
            let blob: Blob
            if ('blob' in res && res.blob) {
              blob = new Blob([Uint8Array.from(atob(res.blob), c => c.charCodeAt(0))], { type: res.mimeType })
            } else if ('text' in res) {
              blob = new Blob([res.text ?? ''], { type: res.mimeType })
            } else {
              continue
            }
            const url = URL.createObjectURL(blob)
            const a = document.createElement('a')
            a.href = url
            a.download = res.uri.split('/').pop() ?? 'download'
            document.body.appendChild(a)
            a.click()
            a.remove()
            URL.revokeObjectURL(url)
          } else if (item.type === 'resource_link') {
            window.open(item.uri, '_blank')
          }
        }
        return {}
      } catch {
        return { isError: true }
      }
    }

    bridge.oninitialized = () => {
      isConnectedRef.current = true
      if (toolInput && Object.keys(toolInput).length > 0) {
        try {
          bridge.sendToolInput({ arguments: toolInput })
        } catch (err) {
          console.error('[McpAppIframe] sendToolInput failed:', err)
        }
      }
      if (pendingToolResultRef.current !== undefined) {
        sendToolResult(bridge, pendingToolResultRef.current)
        pendingToolResultRef.current = undefined
      }
      if (toolCancelled) {
        try {
          bridge.sendToolCancelled({ reason: toolCancelReason })
        } catch (err) {
          console.error('[McpAppIframe] sendToolCancelled failed:', err)
        }
      }
    }

    try {
      await bridge.connect(transport)
      bridgeRef.current = bridge
    } catch (err) {
      console.error('[McpAppIframe] AppBridge connect failed:', err)
    }

    // Measure height from parent side after load to avoid feedback loop
    requestAnimationFrame(() => {
      try {
        const doc = iframe.contentDocument
        if (!doc?.body) return
        const h = doc.body.scrollHeight
        if (h > 0) {
          lastHeightRef.current = h
          setIframeHeight(h)
        }
      } catch {
        // cross-origin, fall back to postMessage
      }
    })
  }, [onOpenLink, onSendMessage, sendToolResult, serverId, displayMode, onExpand, themeMode, locale, toolName, toolInput, toolCancelled, toolCancelReason, csp, accessToken])

  // Cleanup on unmount: teardown gracefully before closing
  useEffect(() => {
    return () => {
      isConnectedRef.current = false
      const bridge = bridgeRef.current
      if (bridge) {
        bridge.teardownResource({}).catch(() => {})
        bridge.close().catch(() => {})
      }
      bridgeRef.current = null
    }
  }, [])

  // Listen for resize messages from iframe (fallback for cross-origin)
  // Primary: parent-side ResizeObserver on iframe content body
  useEffect(() => {
    const iframe = iframeRef.current
    if (!iframe) return

    let ro: ResizeObserver | null = null
    let rafId = 0

    const measureAndSet = () => {
      try {
        const doc = iframe.contentDocument
        if (!doc?.body) return
        const h = doc.body.scrollHeight
        if (h > 0 && Math.abs(h - lastHeightRef.current) > 5) {
          lastHeightRef.current = h
          setIframeHeight(h)
        }
      } catch {
        // cross-origin, rely on postMessage fallback
      }
    }

    // Initial measure after a frame
    rafId = requestAnimationFrame(() => {
      measureAndSet()
      // Observe iframe body for content changes
      try {
        const doc = iframe.contentDocument
        if (doc?.body) {
          ro = new ResizeObserver(() => {
            cancelAnimationFrame(rafId)
            rafId = requestAnimationFrame(measureAndSet)
          })
          ro.observe(doc.body)
        }
      } catch {
        // cross-origin
      }
    })

    return () => {
      cancelAnimationFrame(rafId)
      ro?.disconnect()
    }
  }, [srcDoc]) // re-observe when srcDoc changes

  // Fallback: listen for resize messages from iframe (cross-origin support)
  useEffect(() => {
    const handler = (event: MessageEvent) => {
      const iframe = iframeRef.current
      if (!iframe || event.source !== iframe.contentWindow) return
      if (event.data?.type === 'arkloop:mcpapp:resize' && typeof event.data.height === 'number') {
        const newHeight = Math.min(event.data.height, 2000)
        if (Math.abs(newHeight - lastHeightRef.current) > 10) {
          lastHeightRef.current = newHeight
          setIframeHeight(newHeight)
        }
      }
    }
    window.addEventListener('message', handler)
    return () => window.removeEventListener('message', handler)
  }, [])

  const rebuildSrcDoc = useCallback((htmlContent: string) => {
    const next = buildIframeSrcDoc(htmlContent, csp)
    setSrcDoc((prev) => (prev !== next ? next : prev))
  }, [csp])

  useEffect(() => {
    rebuildSrcDoc(content)
  }, [content, rebuildSrcDoc])

  // Send tool-cancelled when the prop flips to true (after bridge is connected)
  useEffect(() => {
    if (!toolCancelled || !isConnectedRef.current || !bridgeRef.current) return
    try {
      bridgeRef.current.sendToolCancelled({ reason: toolCancelReason })
    } catch (err) {
      console.error('[McpAppIframe] sendToolCancelled failed:', err)
    }
  }, [toolCancelled, toolCancelReason])

  // Theme change listener: rebuild srcDoc AND update hostContext via setHostContext
  useEffect(() => {
    if (typeof document === 'undefined') return
    const root = document.documentElement
    const observer = new MutationObserver(() => {
      rebuildSrcDoc(content)
      if (bridgeRef.current) {
        try {
          bridgeRef.current.setHostContext({
            theme: getResolvedTheme(themeMode),
          })
        } catch {
          // bridge may have been closed
        }
      }
    })
    observer.observe(root, { attributes: true, attributeFilter: ['data-theme'] })
    return () => observer.disconnect()
  }, [content, rebuildSrcDoc, themeMode])

  if (displayMode === 'fullscreen' && onExpand) {
    return (
      <div
        style={{
          borderRadius: '10px',
          border: '0.5px solid var(--c-border-subtle)',
          overflow: 'hidden',
          background: 'var(--c-bg-sub)',
          cursor: 'pointer',
          transition: 'border-color 150ms ease',
        }}
        onClick={onExpand}
        onMouseEnter={(e) => { e.currentTarget.style.borderColor = 'var(--c-border-mid)' }}
        onMouseLeave={(e) => { e.currentTarget.style.borderColor = 'var(--c-border-subtle)' }}
      >
        <div style={{
          padding: '12px 16px',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: '12px',
        }}>
          <div style={{
            display: 'flex',
            alignItems: 'center',
            gap: '10px',
            minWidth: 0,
            flex: 1,
          }}>
            <div style={{
              width: '36px',
              height: '36px',
              borderRadius: '8px',
              background: 'var(--c-bg-page)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              flexShrink: 0,
              border: '0.5px solid var(--c-border-subtle)',
            }}>
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="var(--c-text-muted)" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
                <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
                <line x1="8" y1="21" x2="16" y2="21" />
                <line x1="12" y1="17" x2="12" y2="21" />
              </svg>
            </div>
            <span style={{
              fontSize: '14px',
              color: 'var(--c-text-primary)',
              fontWeight: 500,
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
            }}>
              {name || uri}
            </span>
          </div>
          <span style={{
            fontSize: '13px',
            color: 'var(--c-text-secondary)',
            display: 'flex',
            alignItems: 'center',
            gap: '4px',
            flexShrink: 0,
            padding: '4px 10px',
            borderRadius: '6px',
            background: 'var(--c-bg-input)',
            border: '0.5px solid var(--c-border-subtle)',
          }}>
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M15 3h6v6" /><path d="M10 14 21 3" /><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
            </svg>
            预览
          </span>
        </div>
      </div>
    )
  }

  return (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      width: '100%',
      height: '100%',
      border: noBorder ? 'none' : '0.5px solid var(--c-border-subtle)',
      borderRadius: noBorder ? 0 : '10px',
      overflow: 'hidden',
      background: 'var(--c-bg-sub)',
    }}>
      {(name || uri) && !hideHeader && (
        <div style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: '8px',
          padding: '6px 12px',
          borderBottom: '0.5px solid var(--c-border-subtle)',
          background: 'var(--c-bg-sub)',
          minWidth: 0,
        }}>
          {name && (
            <span style={{
              fontSize: '12px',
              color: 'var(--c-text-secondary)',
              fontWeight: 500,
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              minWidth: 0,
              flexShrink: 1,
            }}>
              {name}
            </span>
          )}
          {uri && (
            <span style={{
              fontSize: '11px',
              color: 'var(--c-text-muted)',
              whiteSpace: 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              minWidth: 0,
              flexShrink: 1,
            }}>
              {uri}
            </span>
          )}
        </div>
      )}
      <iframe
        ref={iframeRef}
        srcDoc={srcDoc}
        title={`mcp-app-${uri}`}
        sandbox="allow-scripts allow-same-origin"
        onLoad={handleLoad}
        style={{
          width: '100%',
          minHeight: '200px',
          height: iframeHeight ? `${iframeHeight}px` : 'auto',
          border: 'none',
          borderRadius: 0,
          background: 'transparent',
          display: 'block',
          ...style,
        }}
        className={className}
      />
    </div>
  )
}
