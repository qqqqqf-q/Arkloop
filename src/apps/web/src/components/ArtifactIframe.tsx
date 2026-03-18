import { useRef, useEffect, useCallback, useImperativeHandle, forwardRef, useState } from 'react'
import { apiBaseUrl } from '@arkloop/shared/api'
import type { ArtifactRef } from '../storage'
import { ARTIFACT_SVG_STYLES } from './artifactSvgStyles'

const STREAM_UPDATE_DEBOUNCE_MS = 120

export type ArtifactAction =
  | { type: 'prompt'; text: string }
  | { type: 'resize'; height: number }
  | { type: 'error'; message: string }

export type ArtifactIframeHandle = {
  setStreamingContent: (html: string) => void
  finalizeContent: (html: string) => void
}

type Props = {
  mode: 'streaming' | 'static'
  artifact?: ArtifactRef
  accessToken?: string
  content?: string
  contentType?: string
  onAction?: (action: ArtifactAction) => void
  frameTitle?: string
  className?: string
  style?: React.CSSProperties
}

type ThemeSnapshot = {
  cssVars: string
  theme: 'light' | 'dark' | null
}

type RuntimeContent = {
  html: string
  finalize: boolean
  contentType?: string
}

function buildThemeCSS(cssVars: string): string {
  return `:root {
    ${cssVars}
    --color-text-primary: var(--c-text-primary);
    --color-text-secondary: var(--c-text-secondary);
    --color-text-tertiary: var(--c-text-tertiary, var(--c-text-secondary));
    --color-text-info: var(--c-status-info-text, var(--c-text-primary));
    --color-text-danger: var(--c-status-error-text, var(--c-text-primary));
    --color-text-success: var(--c-status-success-text, var(--c-text-primary));
    --color-text-warning: var(--c-status-warning-text, var(--c-text-primary));
    --color-background-primary: var(--c-bg-sub);
    --color-background-secondary: var(--c-bg-page);
    --color-background-tertiary: var(--c-bg-deep);
    --color-background-info: var(--c-status-info-bg, var(--c-bg-sub));
    --color-background-danger: var(--c-status-danger-bg, var(--c-bg-sub));
    --color-background-success: var(--c-status-success-bg, var(--c-bg-sub));
    --color-background-warning: var(--c-status-warning-bg, var(--c-bg-sub));
    --color-border-primary: var(--c-border);
    --color-border-secondary: var(--c-border-mid);
    --color-border-tertiary: var(--c-border-subtle);
    --color-border-info: var(--c-status-info-border, var(--c-border-mid));
    --color-border-danger: var(--c-status-danger-border, var(--c-border-mid));
    --color-border-success: var(--c-status-success-border, var(--c-border-mid));
    --color-border-warning: var(--c-status-warning-border, var(--c-border-mid));
    --font-sans: var(--c-font-body, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif);
    --font-serif: var(--c-font-serif, "Iowan Old Style", "Palatino Linotype", Georgia, serif);
    --font-mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;
    --border-radius-md: 8px;
    --border-radius-lg: 12px;
    --border-radius-xl: 16px;
  }`
}

function collectCSSVariables(): string {
  if (typeof document === 'undefined') return ''
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
      // ignore cross-origin stylesheets
    }
  }
  return vars.join('\n    ')
}

function collectThemeSnapshot(): ThemeSnapshot {
  if (typeof document === 'undefined') {
    return { cssVars: '', theme: null }
  }
  const rawTheme = document.documentElement.getAttribute('data-theme')
  return {
    cssVars: collectCSSVariables(),
    theme: rawTheme === 'light' || rawTheme === 'dark' ? rawTheme : null,
  }
}

function buildShellHTML(snapshot: ThemeSnapshot): string {
  const themeAttr = snapshot.theme ? ` data-theme="${snapshot.theme}"` : ''

  return `<!DOCTYPE html>
<html${themeAttr}>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'unsafe-inline' https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://unpkg.com https://esm.sh; style-src 'unsafe-inline'; img-src data: blob: https:; font-src https:; connect-src https:;">
<style id="arkloop-theme-vars">
${buildThemeCSS(snapshot.cssVars)}
</style>
<style>
  * { box-sizing: border-box; }
  html {
    background: transparent;
    overflow-x: hidden;
    color-scheme: light dark;
  }
  body {
    margin: 0;
    padding: 10px 0;
    background: transparent;
    color: var(--color-text-primary, #111111);
    font: 400 14px/1.7 var(--font-sans);
    overflow-x: hidden;
  }
  #root {
    display: block;
    width: 100%;
    background: transparent;
  }
  #root > :first-child { margin-top: 0 !important; }
  #root > :last-child { margin-bottom: 0 !important; }
  .arkloop-embed-body {
    display: block;
    width: 100%;
    color: inherit;
    background: transparent;
  }
  .arkloop-svg-stage {
    display: flex;
    width: 100%;
    justify-content: center;
    align-items: flex-start;
    padding: 10px 0;
  }
  .arkloop-svg-stage > svg {
    display: block;
    max-width: 100%;
    height: auto;
  }
  :where(a) {
    color: inherit;
  }
  :where(button, select, input, textarea) {
    font: inherit;
    color: var(--color-text-primary);
  }
  :where(button) {
    appearance: none;
    border: 0.5px solid var(--color-border-tertiary);
    border-radius: var(--border-radius-md);
    background: var(--color-background-primary);
    padding: 6px 14px;
  }
  :where(button:hover) {
    background: var(--color-background-tertiary);
  }
  :where(select, input[type="text"], input[type="number"], textarea) {
    appearance: none;
    border: 0.5px solid var(--color-border-tertiary);
    border-radius: var(--border-radius-md);
    background: var(--color-background-primary);
    min-height: 36px;
    padding: 0 10px;
  }
  :where(textarea) {
    min-height: 80px;
    padding: 10px;
  }
  :where(input[type="range"]) {
    appearance: none;
    width: 100%;
    min-width: 88px;
    height: 20px;
    background: transparent;
  }
  :where(input[type="range"]::-webkit-slider-runnable-track) {
    height: 2px;
    border-radius: 999px;
    background: var(--color-border-secondary);
  }
  :where(input[type="range"]::-webkit-slider-thumb) {
    appearance: none;
    width: 12px;
    height: 12px;
    margin-top: -5px;
    border-radius: 999px;
    border: none;
    background: var(--color-text-primary);
  }
  :where(input[type="range"]::-moz-range-track) {
    height: 2px;
    border: none;
    border-radius: 999px;
    background: var(--color-border-secondary);
  }
  :where(input[type="range"]::-moz-range-thumb) {
    width: 12px;
    height: 12px;
    border: none;
    border-radius: 999px;
    background: var(--color-text-primary);
  }
  @keyframes _fadeIn {
    from { opacity: 0; transform: translateY(4px); }
    to { opacity: 1; transform: translateY(0); }
  }
  @media (prefers-reduced-motion: reduce) {
    *, *::before, *::after {
      animation-duration: 0.001ms !important;
      animation-iteration-count: 1 !important;
      transition-duration: 0.001ms !important;
      scroll-behavior: auto !important;
    }
  }

${ARTIFACT_SVG_STYLES}
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

  function reportError(message) {
    window.parent.postMessage({ type: 'arkloop:artifact:action', action: 'error', message: String(message || 'render error').slice(0, 4000) }, '*');
  }

  function buildThemeCSS(cssVars) {
    return ':root {\\n'
      + cssVars + '\\n'
      + '    --color-text-primary: var(--c-text-primary);\\n'
      + '    --color-text-secondary: var(--c-text-secondary);\\n'
      + '    --color-text-tertiary: var(--c-text-tertiary, var(--c-text-secondary));\\n'
      + '    --color-text-info: var(--c-status-info-text, var(--c-text-primary));\\n'
      + '    --color-text-danger: var(--c-status-error-text, var(--c-text-primary));\\n'
      + '    --color-text-success: var(--c-status-success-text, var(--c-text-primary));\\n'
      + '    --color-text-warning: var(--c-status-warning-text, var(--c-text-primary));\\n'
      + '    --color-background-primary: var(--c-bg-sub);\\n'
      + '    --color-background-secondary: var(--c-bg-page);\\n'
      + '    --color-background-tertiary: var(--c-bg-deep);\\n'
      + '    --color-background-info: var(--c-status-info-bg, var(--c-bg-sub));\\n'
      + '    --color-background-danger: var(--c-status-danger-bg, var(--c-bg-sub));\\n'
      + '    --color-background-success: var(--c-status-success-bg, var(--c-bg-sub));\\n'
      + '    --color-background-warning: var(--c-status-warning-bg, var(--c-bg-sub));\\n'
      + '    --color-border-primary: var(--c-border);\\n'
      + '    --color-border-secondary: var(--c-border-mid);\\n'
      + '    --color-border-tertiary: var(--c-border-subtle);\\n'
      + '    --color-border-info: var(--c-status-info-border, var(--c-border-mid));\\n'
      + '    --color-border-danger: var(--c-status-danger-border, var(--c-border-mid));\\n'
      + '    --color-border-success: var(--c-status-success-border, var(--c-border-mid));\\n'
      + '    --color-border-warning: var(--c-status-warning-border, var(--c-border-mid));\\n'
      + '    --font-sans: var(--c-font-body, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif);\\n'
      + '    --font-serif: var(--c-font-serif, "Iowan Old Style", "Palatino Linotype", Georgia, serif);\\n'
      + '    --font-mono: ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace;\\n'
      + '    --border-radius-md: 8px;\\n'
      + '    --border-radius-lg: 12px;\\n'
      + '    --border-radius-xl: 16px;\\n'
      + '  }';
  }

  function cloneOuterHTML(nodes) {
    return Array.prototype.map.call(nodes, function(node) {
      return node.outerHTML;
    }).join('');
  }

  function normalizeContent(html, contentType) {
    var source = typeof html === 'string' ? html : '';
    var trimmed = source.trim();
    if (!trimmed) return '';

    var hintedSvg = contentType === 'image/svg+xml';
    var isSvg = hintedSvg || /^<svg[\\s>]/i.test(trimmed) || /^<\\?xml[\\s\\S]*?<svg[\\s>]/i.test(trimmed);
    if (isSvg) {
      var cleanedSvg = trimmed
        .replace(/^<\\?xml[\\s\\S]*?\\?>\\s*/i, '')
        .replace(/^<!doctype[\\s\\S]*?>\\s*/i, '');
      return '<div class="arkloop-svg-stage">' + cleanedSvg + '</div>';
    }

    if (!/(<!doctype|<html[\\s>]|<head[\\s>]|<body[\\s>])/i.test(trimmed)) {
      return source;
    }

    try {
      var parser = new DOMParser();
      var doc = parser.parseFromString(trimmed, 'text/html');
      var styles = cloneOuterHTML(doc.querySelectorAll('style, link[rel="stylesheet"]'));
      var scripts = cloneOuterHTML(doc.querySelectorAll('script'));
      var body = doc.body ? doc.body.cloneNode(true) : null;
      if (body) {
        Array.prototype.slice.call(body.querySelectorAll('style, link[rel="stylesheet"], script')).forEach(function(node) {
          if (node && node.parentNode) node.parentNode.removeChild(node);
        });
      }

      var bodyStyle = doc.body && doc.body.getAttribute('style') ? doc.body.getAttribute('style') : '';
      var bodyClass = doc.body && doc.body.getAttribute('class') ? doc.body.getAttribute('class') : '';
      var wrapper = doc.createElement('div');
      wrapper.className = ('arkloop-embed-body ' + bodyClass).trim();
      if (bodyStyle) wrapper.setAttribute('style', bodyStyle);
      wrapper.innerHTML = body ? body.innerHTML : trimmed;

      return styles + wrapper.outerHTML + scripts;
    } catch {
      return source;
    }
  }

  window.addEventListener('arkloop:send-prompt', function(event) {
    var text = event && typeof event.detail === 'string' ? event.detail : '';
    if (text) window.arkloop.sendPrompt(text);
  });

  window.addEventListener('error', function(event) {
    reportError(event && event.message ? event.message : 'render error');
  });

  window.addEventListener('unhandledrejection', function(event) {
    var reason = event && event.reason;
    reportError(reason && reason.message ? reason.message : String(reason || 'render error'));
  });

  window._applyTheme = function(payload) {
    var root = document.documentElement;
    var theme = payload && typeof payload.theme === 'string' ? payload.theme : '';
    if (theme === 'light' || theme === 'dark') {
      root.setAttribute('data-theme', theme);
    } else {
      root.removeAttribute('data-theme');
    }
    var style = document.getElementById('arkloop-theme-vars');
    if (style && payload && typeof payload.cssVars === 'string') {
      style.textContent = buildThemeCSS(payload.cssVars);
    }
    window._notifyHeight();
  };

  window._setContent = function(html, finalize, contentType) {
    if (!morphReady) {
      pending = {
        html: typeof html === 'string' ? html : '',
        finalize: finalize === true,
        contentType: typeof contentType === 'string' ? contentType : ''
      };
      return;
    }

    var root = document.getElementById('root');
    if (!root) return;

    var target = document.createElement('div');
    target.id = 'root';
    target.innerHTML = normalizeContent(html, contentType);

    morphdom(root, target, {
      onBeforeElUpdated: function(from, to) {
        if (from.isEqualNode(to)) return false;
        return true;
      },
      onNodeAdded: function(node) {
        if (node.nodeType === 1 && node.tagName !== 'STYLE' && node.tagName !== 'SCRIPT') {
          node.style.animation = '_fadeIn 0.3s ease both';
        }
        return node;
      }
    });

    window._notifyHeight();
    if (finalize === true) {
      window._runScripts();
    }
  };

  window._runScripts = async function() {
    var scripts = Array.prototype.slice.call(document.querySelectorAll('#root script'));
    for (var index = 0; index < scripts.length; index++) {
      await new Promise(function(resolve) {
        var old = scripts[index];
        if (!old || !old.parentNode) { resolve(); return; }
        var script = document.createElement('script');
        var isExternal = !!old.src;
        if (isExternal) {
          script.src = old.src;
          script.onload = function() { resolve(); };
          script.onerror = function() {
            reportError('failed to load script: ' + old.src);
            resolve();
          };
        } else {
          script.textContent = old.textContent;
        }
        for (var i = 0; i < old.attributes.length; i++) {
          var attr = old.attributes[i];
          if (attr.name !== 'src') script.setAttribute(attr.name, attr.value);
        }
        old.parentNode.replaceChild(script, old);
        if (!isExternal) resolve();
      });
    }
    window._notifyHeight();
  };

  window._notifyHeight = function() {
    var root = document.getElementById('root');
    if (!root) return;
    var rect = root.getBoundingClientRect();
    var height = Math.max(root.scrollHeight, Math.ceil(rect.height), document.body.scrollHeight) + 20;
    window.parent.postMessage({ type: 'arkloop:artifact:action', action: 'resize', height: height }, '*');
  };

  var morphScript = document.querySelector('script[src*="morphdom"]');
  if (typeof window.morphdom === 'function') {
    morphReady = true;
  } else if (morphScript) {
    morphScript.onload = function() {
      morphReady = true;
      if (pending) {
        window._setContent(pending.html, pending.finalize, pending.contentType);
        pending = null;
      }
    };
    morphScript.onerror = function() {
      morphReady = true;
      if (pending) {
        document.getElementById('root').innerHTML = normalizeContent(pending.html, pending.contentType);
        if (pending.finalize === true) window._runScripts();
        window._notifyHeight();
        pending = null;
      }
    };
  }

  window.addEventListener('message', function(event) {
    var data = event.data;
    if (!data) return;
    if (data.type === 'arkloop:artifact:set-theme') {
      window._applyTheme(data);
      return;
    }
    if (data.type !== 'arkloop:artifact:set-content') return;
    window._setContent(data.html, data.finalize === true, data.contentType);
  });

  new MutationObserver(function() { window._notifyHeight(); })
    .observe(document.getElementById('root'), { childList: true, subtree: true, attributes: true });

  if (typeof ResizeObserver === 'function') {
    var resizeObserver = new ResizeObserver(function() { window._notifyHeight(); });
    resizeObserver.observe(document.body);
    resizeObserver.observe(document.getElementById('root'));
  }

  window.addEventListener('load', function() {
    window._notifyHeight();
  });
})();
</script>
</body>
</html>`
}

export const ArtifactIframe = forwardRef<ArtifactIframeHandle, Props>(
  function ArtifactIframe({ mode, artifact, accessToken, content, contentType, onAction, frameTitle, className, style }, ref) {
    const iframeRef = useRef<HTMLIFrameElement>(null)
    const [shellUrl, setShellUrl] = useState<string | null>(null)
    const [error, setError] = useState(false)
    const [staticContent, setStaticContent] = useState<{ html: string; contentType?: string } | null>(
      mode === 'static' && typeof content === 'string'
        ? { html: content, contentType }
        : null,
    )
    const shellBlobRef = useRef<string | null>(null)
    const isReadyRef = useRef(false)
    const pendingContentRef = useRef<RuntimeContent | null>(null)
    const flushTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

    useEffect(() => {
      isReadyRef.current = false
      const html = buildShellHTML(collectThemeSnapshot())
      const blob = new Blob([html], { type: 'text/html' })
      const url = URL.createObjectURL(blob)
      shellBlobRef.current = url
      setShellUrl(url)
      return () => {
        if (shellBlobRef.current === url) {
          shellBlobRef.current = null
        }
        URL.revokeObjectURL(url)
      }
    }, [])

    const postThemeSnapshot = useCallback(() => {
      const iframe = iframeRef.current
      if (!iframe?.contentWindow || !isReadyRef.current) return
      const snapshot = collectThemeSnapshot()
      try {
        iframe.contentWindow.postMessage({
          type: 'arkloop:artifact:set-theme',
          theme: snapshot.theme ?? '',
          cssVars: snapshot.cssVars,
        }, '*')
      } catch {
        // iframe not ready
      }
    }, [])

    const flushPendingContent = useCallback(() => {
      if (flushTimerRef.current) {
        clearTimeout(flushTimerRef.current)
        flushTimerRef.current = null
      }
      const iframe = iframeRef.current
      const pending = pendingContentRef.current
      if (!pending || !iframe?.contentWindow || !isReadyRef.current) return
      try {
        iframe.contentWindow.postMessage({
          type: 'arkloop:artifact:set-content',
          html: pending.html,
          finalize: pending.finalize,
          contentType: pending.contentType,
        }, '*')
      } catch {
        // iframe not ready
      }
    }, [])

    const queueContent = useCallback((next: RuntimeContent) => {
      pendingContentRef.current = next
      if (next.finalize) {
        flushPendingContent()
        return
      }
      if (flushTimerRef.current) return
      flushTimerRef.current = setTimeout(() => {
        flushTimerRef.current = null
        flushPendingContent()
      }, STREAM_UPDATE_DEBOUNCE_MS)
    }, [flushPendingContent])

    useImperativeHandle(ref, () => ({
      setStreamingContent(html: string) {
        queueContent({ html, finalize: false, contentType: 'text/html' })
      },
      finalizeContent(html: string) {
        queueContent({ html, finalize: true, contentType: 'text/html' })
      },
    }), [queueContent])

    useEffect(() => {
      if (mode !== 'static') return
      setError(false)
      if (typeof content === 'string') {
        setStaticContent({ html: content, contentType })
        return
      }
      if (!artifact || !accessToken) {
        setStaticContent(null)
        return
      }

      let cancelled = false
      const url = `${apiBaseUrl()}/v1/artifacts/${artifact.key}`
      fetch(url, { headers: { Authorization: `Bearer ${accessToken}` } })
        .then(async (res) => {
          if (!res.ok) throw new Error(`${res.status}`)
          const resolvedContentType = res.headers.get('content-type') ?? artifact.mime_type
          const html = await res.text()
          if (cancelled) return
          setStaticContent({ html, contentType: resolvedContentType })
        })
        .catch(() => {
          if (cancelled) return
          setError(true)
        })

      return () => {
        cancelled = true
      }
    }, [mode, artifact, accessToken, content, contentType])

    useEffect(() => {
      if (mode !== 'static' || !staticContent) return
      queueContent({
        html: staticContent.html,
        finalize: true,
        contentType: staticContent.contentType,
      })
    }, [mode, queueContent, staticContent])

    useEffect(() => {
      if (typeof document === 'undefined') return

      const root = document.documentElement
      const handleThemeChange = () => postThemeSnapshot()
      const observer = new MutationObserver(handleThemeChange)
      observer.observe(root, { attributes: true, attributeFilter: ['data-theme'] })

      const media = typeof window.matchMedia === 'function'
        ? window.matchMedia('(prefers-color-scheme: dark)')
        : null
      const addMediaListener = media?.addEventListener?.bind(media)
      const removeMediaListener = media?.removeEventListener?.bind(media)
      const addLegacyMediaListener = media && 'addListener' in media
        ? (media.addListener as ((listener: (event: MediaQueryListEvent) => void) => void)).bind(media)
        : null
      const removeLegacyMediaListener = media && 'removeListener' in media
        ? (media.removeListener as ((listener: (event: MediaQueryListEvent) => void) => void)).bind(media)
        : null

      if (addMediaListener) {
        addMediaListener('change', handleThemeChange)
      } else if (addLegacyMediaListener) {
        addLegacyMediaListener(handleThemeChange)
      }

      return () => {
        observer.disconnect()
        if (removeMediaListener) {
          removeMediaListener('change', handleThemeChange)
        } else if (removeLegacyMediaListener) {
          removeLegacyMediaListener(handleThemeChange)
        }
      }
    }, [postThemeSnapshot])

    useEffect(() => {
      const handler = (event: MessageEvent) => {
        const iframe = iframeRef.current
        if (!iframe || event.source !== iframe.contentWindow) return
        if (event.data?.type !== 'arkloop:artifact:action') return
        const action = event.data.action
        if (action === 'resize' && typeof event.data.height === 'number') {
          iframe.style.height = `${Math.min(event.data.height, 2000)}px`
          onAction?.({ type: 'resize', height: event.data.height })
          return
        }
        if (action === 'prompt' && typeof event.data.text === 'string') {
          onAction?.({ type: 'prompt', text: event.data.text.slice(0, 4000) })
          return
        }
        if (action === 'error' && typeof event.data.message === 'string') {
          onAction?.({ type: 'error', message: event.data.message.slice(0, 4000) })
        }
      }
      window.addEventListener('message', handler)
      return () => window.removeEventListener('message', handler)
    }, [onAction])

    useEffect(() => () => {
      if (flushTimerRef.current) clearTimeout(flushTimerRef.current)
    }, [])

    if (error) return null

    const isLoading = !shellUrl || (mode === 'static' && !staticContent)
    if (isLoading) {
      return (
        <div
          className={className}
          style={{
            width: '100%',
            minHeight: '200px',
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
        src={shellUrl}
        title={frameTitle ?? 'artifact'}
        sandbox="allow-scripts"
        onLoad={() => {
          isReadyRef.current = true
          postThemeSnapshot()
          flushPendingContent()
        }}
        style={{
          width: '100%',
          minHeight: '200px',
          border: '0.5px solid var(--c-border-subtle)',
          borderRadius: '10px',
          background: 'transparent',
          display: 'block',
          ...style,
        }}
        className={className}
      />
    )
  },
)
