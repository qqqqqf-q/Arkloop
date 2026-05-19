import { useRef, useEffect, useCallback, useState } from 'react'
import type { McpAppCsp } from '../storage'

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
</head>
<body>
${content}
<script>
(function() {
  var resizeTimer;
  function notifyHeight() {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function() {
      var h = Math.max(
        document.body.scrollHeight,
        document.documentElement.scrollHeight,
        document.body.offsetHeight,
        document.documentElement.getBoundingClientRect().height,
        document.body.getBoundingClientRect().height
      );
      window.parent.postMessage({ type: 'arkloop:mcpapp:resize', height: Math.ceil(h) + 20 }, '*');
    }, 150);
  }
  new MutationObserver(notifyHeight).observe(document.body, { childList: true, subtree: true, attributes: true });
  if (typeof ResizeObserver === 'function') {
    var ro = new ResizeObserver(notifyHeight);
    ro.observe(document.body);
  }
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

function collectThemeSnapshot(): { css: string; theme: 'light' | 'dark' | null } {
  if (typeof document === 'undefined') {
    return { css: '', theme: null }
  }
  const rawTheme = document.documentElement.getAttribute('data-theme')
  return {
    css: buildThemeCSS(),
    theme: rawTheme === 'light' || rawTheme === 'dark' ? rawTheme : null,
  }
}

type Props = {
  uri: string
  content: string
  toolOutput?: unknown
  csp?: McpAppCsp
  onOpenLink?: (url: string) => void
  style?: React.CSSProperties
  className?: string
}

export function McpAppIframe({ uri, content, toolOutput, csp, onOpenLink: _onOpenLink, style, className }: Props) {
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const lastHeightRef = useRef<number>(0)
  const [iframeHeight, setIframeHeight] = useState<number | undefined>(undefined)

  const [srcDoc, setSrcDoc] = useState(() => {
    const snapshot = collectThemeSnapshot()
    return IFRAME_HTML_TEMPLATE(snapshot.css, content, buildCSP(csp))
  })

  const rebuildSrcDoc = useCallback((htmlContent: string) => {
    const snapshot = collectThemeSnapshot()
    const next = IFRAME_HTML_TEMPLATE(snapshot.css, htmlContent, buildCSP(csp))
    setSrcDoc((prev) => (prev !== next ? next : prev))
  }, [csp])

  useEffect(() => {
    rebuildSrcDoc(content)
  }, [content, rebuildSrcDoc])

  // Theme change listener
  useEffect(() => {
    if (typeof document === 'undefined') return
    const root = document.documentElement
    const observer = new MutationObserver(() => {
      rebuildSrcDoc(content)
    })
    observer.observe(root, { attributes: true, attributeFilter: ['data-theme'] })
    return () => observer.disconnect()
  }, [content, rebuildSrcDoc])

  // Listen for resize messages from iframe
  useEffect(() => {
    const handler = (event: MessageEvent) => {
      const iframe = iframeRef.current
      if (!iframe || event.source !== iframe.contentWindow) return
      if (event.data?.type === 'arkloop:mcpapp:resize' && typeof event.data.height === 'number') {
        const newHeight = Math.min(event.data.height, 2000)
        if (Math.abs(newHeight - lastHeightRef.current) > 5) {
          lastHeightRef.current = newHeight
          setIframeHeight(newHeight)
        }
      }
    }
    window.addEventListener('message', handler)
    return () => window.removeEventListener('message', handler)
  }, [])

  // Handle toolOutput prop — pass initial data via script injection on srcDoc change
  useEffect(() => {
    if (toolOutput === undefined || !iframeRef.current?.contentWindow) return
    try {
      iframeRef.current.contentWindow.postMessage({
        type: 'arkloop:mcpapp:tool-output',
        data: toolOutput,
      }, '*')
    } catch { /* ignore */ }
  }, [toolOutput])

  return (
    <iframe
      ref={iframeRef}
      srcDoc={srcDoc}
      title={`mcp-app-${uri}`}
      sandbox="allow-scripts allow-same-origin"
      onLoad={() => {
        if (toolOutput !== undefined && iframeRef.current?.contentWindow) {
          try {
            iframeRef.current.contentWindow.postMessage({
              type: 'arkloop:mcpapp:tool-output',
              data: toolOutput,
            }, '*')
          } catch { /* ignore */ }
        }
      }}
      style={{
        width: '100%',
        minHeight: '200px',
        height: iframeHeight ? `${iframeHeight}px` : 'auto',
        border: '0.5px solid var(--c-border-subtle)',
        borderRadius: '10px',
        background: 'transparent',
        display: 'block',
        ...style,
      }}
      className={className}
    />
  )
}
