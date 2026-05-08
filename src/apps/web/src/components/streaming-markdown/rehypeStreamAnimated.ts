type StreamAnimatedOptions = {
  births?: number[]
  fadeDuration?: number
  nowMs?: number
  revealed?: boolean
}

type HastText = {
  type: 'text'
  value: string
}

type HastElement = {
  type: 'element'
  tagName: string
  properties?: Record<string, unknown>
  children: HastNode[]
}

type HastRoot = {
  type: 'root'
  children: HastNode[]
}

type HastNode = HastRoot | HastElement | HastText | { type: string; [key: string]: unknown }

const BLOCK_TAGS = new Set(['p', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'li'])
const SKIP_TAGS = new Set(['pre', 'code', 'table', 'svg'])

function isElement(node: HastNode): node is HastElement {
  return node.type === 'element' && typeof (node as HastElement).tagName === 'string' && Array.isArray((node as HastElement).children)
}

function isParent(node: HastNode): node is HastRoot | HastElement {
  return Array.isArray((node as { children?: unknown }).children)
}

function hasClass(node: HastElement, className: string): boolean {
  const current = node.properties?.className
  if (Array.isArray(current)) return current.some((item) => String(item).includes(className))
  return typeof current === 'string' && current.includes(className)
}

// Adapted from @lobehub/ui Streamdown (MIT).
export function rehypeStreamAnimated(options: StreamAnimatedOptions = {}) {
  const { births, fadeDuration = 150, nowMs, revealed = false } = options
  const hasBirths = !revealed && Array.isArray(births) && typeof nowMs === 'number'

  return (tree: HastRoot) => {
    let globalCharIndex = 0

    const shouldSkip = (node: HastElement): boolean => SKIP_TAGS.has(node.tagName) || hasClass(node, 'katex')

    const wrapText = (node: HastElement) => {
      const nextChildren: HastNode[] = []

      for (const child of node.children) {
        if (child.type === 'text') {
          const text = child as HastText
          for (const char of text.value) {
            let className = 'stream-char'
            let delay: number | undefined

            if (revealed) {
              className = 'stream-char stream-char-revealed'
            } else if (hasBirths) {
              const birthTs = births[globalCharIndex]
              if (birthTs === undefined) {
                className = 'stream-char stream-char-revealed'
              } else {
                const elapsed = nowMs - birthTs
                if (elapsed >= fadeDuration) {
                  className = 'stream-char stream-char-revealed'
                } else {
                  delay = -elapsed
                }
              }
            }

            const properties: Record<string, unknown> = { className }
            if (delay !== undefined && delay !== 0) {
              properties.style = `animation-delay:${delay}ms`
            }

            nextChildren.push({
              children: [{ type: 'text', value: char }],
              properties,
              tagName: 'span',
              type: 'element',
            })
            globalCharIndex++
          }
          continue
        }

        if (isElement(child) && !shouldSkip(child)) {
          wrapText(child)
        }
        nextChildren.push(child)
      }

      node.children = nextChildren
    }

    const visitElement = (node: HastNode): void => {
      if (!isElement(node)) {
        if (isParent(node)) node.children.forEach(visitElement)
        return
      }

      if (shouldSkip(node)) return
      if (BLOCK_TAGS.has(node.tagName)) {
        wrapText(node)
        return
      }

      node.children.forEach(visitElement)
    }

    visitElement(tree)
  }
}
