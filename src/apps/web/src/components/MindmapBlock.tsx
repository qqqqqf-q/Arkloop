import { useRef, useEffect, useCallback, useState } from 'react'
import { Transformer } from 'markmap-lib'
import { Markmap } from 'markmap-view'
import { Maximize2, Minimize2 } from 'lucide-react'

const transformer = new Transformer()

const DEBOUNCE_MS = 150
const DEFAULT_HEIGHT = 400
const EXPANDED_HEIGHT = 600

// 覆盖 markmap 默认颜色，跟随项目 CSS 变量
const themeStyle = () => `
.markmap {
  --markmap-text-color: var(--c-text-primary);
  --markmap-code-bg: var(--c-md-inline-code-bg, var(--c-bg-deep));
  --markmap-code-color: var(--c-text-secondary);
  --markmap-a-color: var(--c-text-secondary);
  --markmap-circle-open-bg: var(--c-bg-page);
}
`

type Props = {
  content: string
}

export function MindmapBlock({ content }: Props) {
  const svgRef = useRef<SVGSVGElement>(null)
  const markmapRef = useRef<Markmap | null>(null)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [expanded, setExpanded] = useState(false)

  const updateData = useCallback((markdown: string) => {
    if (!markmapRef.current) return
    const { root } = transformer.transform(markdown)
    void markmapRef.current.setData(root)
    void markmapRef.current.fit()
  }, [])

  useEffect(() => {
    if (!svgRef.current) return

    const { root } = transformer.transform(content)
    const mm = Markmap.create(svgRef.current, {
      autoFit: true,
      duration: 300,
      zoom: true,
      pan: true,
      initialExpandLevel: -1,
      embedGlobalCSS: true,
      style: themeStyle,
    }, root)
    markmapRef.current = mm

    return () => {
      mm.destroy()
      markmapRef.current = null
      if (timerRef.current) clearTimeout(timerRef.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (!markmapRef.current) return
    if (timerRef.current) clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => updateData(content), DEBOUNCE_MS)
  }, [content, updateData])

  useEffect(() => {
    if (!markmapRef.current) return
    requestAnimationFrame(() => {
      void markmapRef.current?.fit()
    })
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
          mindmap
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
      <svg
        ref={svgRef}
        style={{
          width: '100%',
          height: `${height}px`,
          display: 'block',
          transition: 'height 0.2s ease',
        }}
      />
    </div>
  )
}
