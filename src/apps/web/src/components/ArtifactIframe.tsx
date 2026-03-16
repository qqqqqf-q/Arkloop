import { useRef, useEffect, useCallback, useImperativeHandle, forwardRef, useState } from 'react'
import { apiBaseUrl } from '@arkloop/shared/api'
import type { ArtifactRef } from '../storage'

export type ArtifactAction =
  | { type: 'prompt'; text: string }
  | { type: 'resize'; height: number }

export type ArtifactIframeHandle = {
  setStreamingContent: (html: string) => void
  finalizeContent: (html: string) => void
}

type Props = {
  mode: 'streaming' | 'static'
  artifact?: ArtifactRef
  accessToken?: string
  onAction?: (action: ArtifactAction) => void
  className?: string
  style?: React.CSSProperties
}

function collectCSSVariables(): string {
  const root = document.documentElement
  const computed = getComputedStyle(root)
  const vars: string[] = []
  for (const sheet of document.styleSheets) {
    try {
      for (const rule of sheet.cssRules) {
        if (rule instanceof CSSStyleRule && rule.selectorText === ':root') {
          for (let i = 0; i < rule.style.length; i++) {
            const prop = rule.style[i]
            if (prop.startsWith('--c-')) {
              vars.push(`${prop}: ${computed.getPropertyValue(prop).trim()};`)
            }
          }
        }
      }
    } catch {
      // cross-origin stylesheets
    }
  }
  return vars.join('\n    ')
}

function buildShellHTML(): string {
  const cssVars = collectCSSVariables()
  return `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval' https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://unpkg.com https://esm.sh; style-src 'unsafe-inline'; img-src data: blob: https:; font-src https:; connect-src https:;">
<style>
  :root {
    ${cssVars}
  }
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    font-size: 14px;
    line-height: 1.5;
    color: var(--c-text-primary, #faf9f5);
    background: transparent;
    padding: 16px;
    overflow-x: hidden;
  }
  @keyframes _fadeIn {
    from { opacity: 0; transform: translateY(4px); }
    to { opacity: 1; transform: translateY(0); }
  }
</style>
</head>
<body>
<div id="root"></div>
<script src="https://cdn.jsdelivr.net/npm/morphdom@2/dist/morphdom-umd.min.js"></script>
<script>
(function() {
  var morphReady = false;
  var pending = null;

  window.arkloop = {
    sendPrompt: function(text) {
      window.parent.postMessage({ type: 'arkloop:artifact:action', action: 'prompt', text: String(text).slice(0, 4000) }, '*');
    }
  };

  window._setContent = function(html) {
    if (!morphReady) { pending = html; return; }
    var root = document.getElementById('root');
    if (!root) return;
    var target = document.createElement('div');
    target.id = 'root';
    target.innerHTML = html;
    morphdom(root, target, {
      onBeforeElUpdated: function(from, to) {
        if (from.isEqualNode(to)) return false;
        return true;
      },
      onNodeAdded: function(node) {
        if (node.nodeType === 1 && node.tagName !== 'SCRIPT') {
          node.style.animation = '_fadeIn 0.3s ease both';
        }
        return node;
      }
    });
    _notifyHeight();
  };

  window._runScripts = function() {
    var scripts = document.querySelectorAll('#root script');
    scripts.forEach(function(old) {
      var s = document.createElement('script');
      if (old.src) { s.src = old.src; }
      else { s.textContent = old.textContent; }
      for (var i = 0; i < old.attributes.length; i++) {
        var attr = old.attributes[i];
        if (attr.name !== 'src') s.setAttribute(attr.name, attr.value);
      }
      old.parentNode.replaceChild(s, old);
    });
  };

  window._notifyHeight = function() {
    var root = document.getElementById('root');
    if (!root) return;
    var h = root.scrollHeight + 32;
    window.parent.postMessage({ type: 'arkloop:artifact:action', action: 'resize', height: h }, '*');
  };

  var morphScript = document.querySelector('script[src*="morphdom"]');
  if (morphScript) {
    morphScript.onload = function() {
      morphReady = true;
      if (pending) { window._setContent(pending); pending = null; }
    };
    morphScript.onerror = function() {
      morphReady = true;
      if (pending) {
        document.getElementById('root').innerHTML = pending;
        pending = null;
      }
    };
  }

  new MutationObserver(function() { _notifyHeight(); })
    .observe(document.getElementById('root'), { childList: true, subtree: true, attributes: true });
})();
</script>
</body>
</html>`
}

function escapeJSString(s: string): string {
  return s
    .replace(/\\/g, '\\\\')
    .replace(/'/g, "\\'")
    .replace(/\n/g, '\\n')
    .replace(/\r/g, '\\r')
    .replace(/<\/script>/gi, '<\\/script>')
}

export const ArtifactIframe = forwardRef<ArtifactIframeHandle, Props>(
  function ArtifactIframe({ mode, artifact, accessToken, onAction, className, style }, ref) {
    const iframeRef = useRef<HTMLIFrameElement>(null)
    const [blobUrl, setBlobUrl] = useState<string | null>(null)
    const [loading, setLoading] = useState(mode === 'static')
    const [error, setError] = useState(false)
    const shellBlobRef = useRef<string | null>(null)

    // streaming mode: create shell HTML blob URL
    useEffect(() => {
      if (mode !== 'streaming') return
      const html = buildShellHTML()
      const blob = new Blob([html], { type: 'text/html' })
      const url = URL.createObjectURL(blob)
      shellBlobRef.current = url
      setBlobUrl(url)
      setLoading(false)
      return () => URL.revokeObjectURL(url)
    }, [mode])

    // static mode: load artifact from API
    useEffect(() => {
      if (mode !== 'static' || !artifact || !accessToken) return
      let cancelled = false
      const url = `${apiBaseUrl()}/v1/artifacts/${artifact.key}`
      fetch(url, { headers: { Authorization: `Bearer ${accessToken}` } })
        .then((res) => {
          if (!res.ok) throw new Error(`${res.status}`)
          return res.blob()
        })
        .then((blob) => {
          if (cancelled) return
          setBlobUrl(URL.createObjectURL(blob))
          setLoading(false)
        })
        .catch(() => {
          if (!cancelled) {
            setError(true)
            setLoading(false)
          }
        })
      return () => { cancelled = true }
    }, [mode, artifact?.key, accessToken])

    // cleanup blob URL
    useEffect(() => {
      return () => {
        if (blobUrl && blobUrl !== shellBlobRef.current) URL.revokeObjectURL(blobUrl)
      }
    }, [blobUrl])

    const evalInIframe = useCallback((js: string) => {
      const iframe = iframeRef.current
      if (!iframe?.contentWindow) return
      try {
        iframe.contentWindow.postMessage({ type: 'arkloop:eval', js }, '*')
      } catch {
        // iframe not ready
      }
    }, [])

    useImperativeHandle(ref, () => ({
      setStreamingContent(html: string) {
        const iframe = iframeRef.current
        if (!iframe?.contentWindow) return
        try {
          const escaped = escapeJSString(html)
          ;(iframe.contentWindow as Window & { eval: (code: string) => void }).eval(`window._setContent('${escaped}')`)
        } catch {
          // iframe not ready yet
        }
      },
      finalizeContent(html: string) {
        const iframe = iframeRef.current
        if (!iframe?.contentWindow) return
        try {
          const escaped = escapeJSString(html)
          ;(iframe.contentWindow as Window & { eval: (code: string) => void }).eval(`window._setContent('${escaped}'); window._runScripts();`)
        } catch {
          // iframe not ready yet
        }
      },
    }), [evalInIframe])

    // postMessage listener for actions from iframe
    useEffect(() => {
      const handler = (e: MessageEvent) => {
        const iframe = iframeRef.current
        if (!iframe) return
        if (e.source !== iframe.contentWindow) return
        if (e.data?.type !== 'arkloop:artifact:action') return
        const action = e.data.action
        if (action === 'resize' && typeof e.data.height === 'number') {
          iframe.style.height = `${Math.min(e.data.height, 2000)}px`
          onAction?.({ type: 'resize', height: e.data.height })
        } else if (action === 'prompt' && typeof e.data.text === 'string') {
          onAction?.({ type: 'prompt', text: e.data.text.slice(0, 4000) })
        }
      }
      window.addEventListener('message', handler)
      return () => window.removeEventListener('message', handler)
    }, [onAction])

    if (error) return null

    if (loading) {
      return (
        <div
          className={className}
          style={{
            width: '100%',
            height: '200px',
            borderRadius: '10px',
            background: 'var(--c-bg-sub)',
            ...style,
          }}
        />
      )
    }

    return (
      <iframe
        ref={iframeRef}
        src={blobUrl!}
        sandbox="allow-scripts allow-same-origin"
        style={{
          width: '100%',
          minHeight: '200px',
          border: '0.5px solid var(--c-border-subtle)',
          borderRadius: '10px',
          background: 'var(--c-bg-page)',
          display: 'block',
          ...style,
        }}
        className={className}
      />
    )
  },
)
