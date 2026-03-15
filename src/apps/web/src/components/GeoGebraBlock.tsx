import { useRef, useEffect, useState, useCallback } from 'react'
import { Maximize2, Minimize2 } from 'lucide-react'

const DEPLOYGGB_URL = 'https://www.geogebra.org/apps/deployggb.js'
const DEFAULT_HEIGHT = 500
const EXPANDED_HEIGHT = 700

let ggbScriptLoaded = false
let ggbScriptLoading: Promise<void> | null = null

function loadGeoGebraScript(): Promise<void> {
  if (ggbScriptLoaded) return Promise.resolve()
  if (ggbScriptLoading) return ggbScriptLoading

  ggbScriptLoading = new Promise((resolve, reject) => {
    const script = document.createElement('script')
    script.src = DEPLOYGGB_URL
    script.async = true
    script.onload = () => {
      ggbScriptLoaded = true
      resolve()
    }
    script.onerror = () => reject(new Error('Failed to load GeoGebra'))
    document.head.appendChild(script)
  })
  return ggbScriptLoading
}

// GeoGebra Applet API (subset)
interface GGBAppletAPI {
  evalCommand(cmd: string): boolean
  reset(): void
  recalculateEnvironments(): void
}

declare class GGBApplet {
  constructor(params: Record<string, unknown>, version?: string)
  inject(id: string): void
}

let instanceCounter = 0

type Props = {
  content: string
}

export function GeoGebraBlock({ content }: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const apiRef = useRef<GGBAppletAPI | null>(null)
  const [expanded, setExpanded] = useState(false)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const idRef = useRef(`ggb-${++instanceCounter}`)
  const lastContentRef = useRef('')

  const contentRef = useRef(content)
  contentRef.current = content

  const initApplet = useCallback(async () => {
    if (!containerRef.current) return
    try {
      await loadGeoGebraScript()

      const applet = new GGBApplet({
        appName: 'classic',
        width: containerRef.current.clientWidth,
        height: DEFAULT_HEIGHT,
        showToolBar: false,
        showAlgebraInput: false,
        showMenuBar: false,
        showResetIcon: true,
        enableLabelDrags: false,
        enableShiftDragZoom: true,
        enableRightClick: false,
        showZoomButtons: true,
        borderColor: 'transparent',
        appletOnLoad: (api: GGBAppletAPI) => {
          apiRef.current = api
          setLoading(false)
          const code = contentRef.current
          if (code.trim()) {
            const lines = code.split('\n').filter(l => l.trim() && !l.trim().startsWith('#'))
            for (const line of lines) {
              api.evalCommand(line.trim())
            }
            lastContentRef.current = code
          }
        },
      }, '5.0')

      applet.inject(idRef.current)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void initApplet()
  }, [initApplet])

  // 增量执行：内容变化时只执行新增部分
  useEffect(() => {
    if (!apiRef.current || content === lastContentRef.current) return
    const api = apiRef.current

    // 简单策略：reset + 全量重执行
    api.reset()
    const lines = content.split('\n').filter(l => l.trim() && !l.trim().startsWith('#'))
    for (const line of lines) {
      api.evalCommand(line.trim())
    }
    lastContentRef.current = content
  }, [content])

  // resize
  useEffect(() => {
    apiRef.current?.recalculateEnvironments()
  }, [expanded])

  const height = expanded ? EXPANDED_HEIGHT : DEFAULT_HEIGHT

  return (
    <div
      style={{
        position: 'relative',
        margin: '1em 0',
        border: '0.5px solid var(--c-border-subtle)',
        borderRadius: '10px',
        background: 'var(--c-bg-page)',
        overflow: 'hidden',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '0 10px',
          height: '28px',
          borderBottom: '0.5px solid var(--c-border-subtle)',
          background: 'var(--c-md-code-label-bg, var(--c-bg-sub))',
        }}
      >
        <span
          style={{
            fontSize: '11px',
            letterSpacing: '0.18px',
            color: 'var(--c-text-secondary)',
            userSelect: 'none',
          }}
        >
          geogebra
        </span>
        <button
          onClick={() => setExpanded(prev => !prev)}
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: '22px',
            height: '22px',
            borderRadius: '5px',
            border: 'none',
            background: 'transparent',
            color: 'var(--c-text-icon)',
            cursor: 'pointer',
            transition: 'opacity 0.15s',
          }}
          className="opacity-60 hover:opacity-100"
        >
          {expanded ? <Minimize2 size={12} /> : <Maximize2 size={12} />}
        </button>
      </div>
      {error ? (
        <div
          style={{
            padding: '16px',
            color: 'var(--c-status-error)',
            fontSize: '13px',
            fontFamily: "'JetBrains Mono', monospace",
            whiteSpace: 'pre-wrap',
          }}
        >
          {error}
        </div>
      ) : (
        <div style={{ position: 'relative', height: `${height}px`, transition: 'height 0.2s ease' }}>
          {loading && (
            <div
              style={{
                position: 'absolute',
                inset: 0,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                color: 'var(--c-text-muted)',
                fontSize: '13px',
              }}
            >
              Loading GeoGebra...
            </div>
          )}
          <div
            id={idRef.current}
            ref={containerRef}
            style={{ width: '100%', height: '100%' }}
          />
        </div>
      )}
    </div>
  )
}
