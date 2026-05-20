import { memo, useEffect, useMemo, useRef, useState } from 'react'
import type { Extension } from '@codemirror/state'
import { tags } from '@lezer/highlight'
import { codeLanguageFromFilename } from './language'
import './CodeDocumentViewer.css'

type DiffLineKind = 'added' | 'removed'

type DiffLine = {
  line: number
  kind: DiffLineKind
}

type Props = {
  content: string
  filename?: string
  mimeType?: string
  showLineNumbers?: boolean
  maxHeight?: number
  fillHeight?: boolean
  diffLines?: DiffLine[]
}

type CodeMirrorRuntime = {
  EditorState: typeof import('@codemirror/state').EditorState
  EditorView: typeof import('@codemirror/view').EditorView
  Decoration: typeof import('@codemirror/view').Decoration
  keymap: typeof import('@codemirror/view').keymap
  lineNumbers: typeof import('@codemirror/view').lineNumbers
  highlightSpecialChars: typeof import('@codemirror/view').highlightSpecialChars
  drawSelection: typeof import('@codemirror/view').drawSelection
  syntaxHighlighting: typeof import('@codemirror/language').syntaxHighlighting
  HighlightStyle: typeof import('@codemirror/language').HighlightStyle
  javascript: typeof import('@codemirror/lang-javascript').javascript
  python: typeof import('@codemirror/lang-python').python
  markdown: typeof import('@codemirror/lang-markdown').markdown
  json: typeof import('@codemirror/lang-json').json
  html: typeof import('@codemirror/lang-html').html
  css: typeof import('@codemirror/lang-css').css
  yaml: typeof import('@codemirror/lang-yaml').yaml
  go: typeof import('@codemirror/lang-go').go
}

let runtimePromise: Promise<CodeMirrorRuntime> | null = null

function loadCodeMirrorRuntime(): Promise<CodeMirrorRuntime> {
  runtimePromise ??= Promise.all([
    import('@codemirror/state'),
    import('@codemirror/view'),
    import('@codemirror/language'),
    import('@codemirror/lang-javascript'),
    import('@codemirror/lang-python'),
    import('@codemirror/lang-markdown'),
    import('@codemirror/lang-json'),
    import('@codemirror/lang-html'),
    import('@codemirror/lang-css'),
    import('@codemirror/lang-yaml'),
    import('@codemirror/lang-go'),
  ]).then(([state, view, language, javascript, python, markdown, json, html, css, yaml, go]) => ({
    EditorState: state.EditorState,
    EditorView: view.EditorView,
    Decoration: view.Decoration,
    keymap: view.keymap,
    lineNumbers: view.lineNumbers,
    highlightSpecialChars: view.highlightSpecialChars,
    drawSelection: view.drawSelection,
    syntaxHighlighting: language.syntaxHighlighting,
    HighlightStyle: language.HighlightStyle,
    javascript: javascript.javascript,
    python: python.python,
    markdown: markdown.markdown,
    json: json.json,
    html: html.html,
    css: css.css,
    yaml: yaml.yaml,
    go: go.go,
  }))
  return runtimePromise
}

function languageExtension(runtime: CodeMirrorRuntime, language: string): Extension[] {
  switch (language) {
    case 'css':
      return [runtime.css()]
    case 'go':
      return [runtime.go()]
    case 'html':
      return [runtime.html()]
    case 'javascript':
      return [runtime.javascript({ jsx: true })]
    case 'json':
      return [runtime.json()]
    case 'markdown':
      return [runtime.markdown()]
    case 'python':
      return [runtime.python()]
    case 'typescript':
      return [runtime.javascript({ jsx: true, typescript: true })]
    case 'yaml':
      return [runtime.yaml()]
    default:
      return []
  }
}

function diffLineExtension(runtime: CodeMirrorRuntime, diffLines: DiffLine[] | undefined): Extension {
  const byLine = new Map<number, DiffLineKind>()
  for (const entry of diffLines ?? []) {
    if (entry.line > 0) byLine.set(entry.line, entry.kind)
  }

  return runtime.EditorView.decorations.compute(['doc'], (state) => {
    const ranges = []
    for (let lineNumber = 1; lineNumber <= state.doc.lines; lineNumber += 1) {
      const kind = byLine.get(lineNumber)
      if (!kind) continue
      const line = state.doc.line(lineNumber)
      ranges.push(runtime.Decoration.line({ class: kind === 'added' ? 'cm-code-line-added' : 'cm-code-line-removed' }).range(line.from))
    }
    return runtime.Decoration.set(ranges, true)
  })
}

function viewerTheme(runtime: CodeMirrorRuntime, maxHeight?: number, fillHeight?: boolean): Extension {
  const rootStyle: Record<string, string> = {
    minHeight: fillHeight ? '100%' : '0',
    height: fillHeight ? '100%' : 'auto',
  }
  const scrollerStyle: Record<string, string> = {
    overflow: 'auto',
  }
  if (maxHeight) {
    rootStyle.maxHeight = `${maxHeight}px`
    scrollerStyle.maxHeight = `${maxHeight}px`
  }

  return runtime.EditorView.theme({
    '&': rootStyle,
    '.cm-scroller': scrollerStyle,
  })
}

function highlightTheme(runtime: CodeMirrorRuntime): Extension {
  return runtime.syntaxHighlighting(runtime.HighlightStyle.define([
    { tag: [tags.comment, tags.quote], color: 'var(--c-text-muted)', fontStyle: 'italic' },
    { tag: [tags.keyword, tags.operatorKeyword, tags.modifier], color: 'var(--c-accent)' },
    { tag: [tags.string, tags.special(tags.string)], color: 'var(--c-status-success-text)' },
    { tag: [tags.number, tags.bool, tags.null], color: 'var(--c-status-warning-text)' },
    { tag: [tags.function(tags.variableName), tags.definition(tags.function(tags.variableName))], color: 'var(--c-text-primary)', fontWeight: '500' },
    { tag: [tags.typeName, tags.className], color: 'var(--c-accent)' },
    { tag: [tags.propertyName, tags.attributeName], color: 'var(--c-text-secondary)' },
    { tag: [tags.tagName], color: 'var(--c-accent)' },
    { tag: [tags.meta], color: 'var(--c-text-tertiary)' },
  ]))
}

export function diffLinesFromUnifiedText(text: string): DiffLine[] {
  const result: DiffLine[] = []
  let visibleLine = 0
  for (const rawLine of text.replace(/\r\n/g, '\n').split('\n')) {
    const line = rawLine.trim()
    if (!line) continue
    if (line.startsWith('--- ') || line.startsWith('+++ ') || line === '---' || line === '+++') continue
    if (line.startsWith('@@') && line.includes('@@')) continue
    if (line.startsWith('diff --git') || line.startsWith('index ')) continue
    visibleLine += 1
    if (rawLine.startsWith('+')) result.push({ line: visibleLine, kind: 'added' })
    else if (rawLine.startsWith('-')) result.push({ line: visibleLine, kind: 'removed' })
  }
  return result
}

export function compactUnifiedDiffForDisplay(text: string): string {
  return text
    .replace(/\r\n/g, '\n')
    .split('\n')
    .filter((rawLine) => {
      const line = rawLine.trim()
      if (!line) return false
      if (line.startsWith('--- ') || line.startsWith('+++ ') || line === '---' || line === '+++') return false
      if (line.startsWith('@@') && line.includes('@@')) return false
      if (line.startsWith('diff --git') || line.startsWith('index ')) return false
      return true
    })
    .join('\n')
}

export const CodeDocumentViewer = memo(function CodeDocumentViewer({
  content,
  filename,
  mimeType,
  showLineNumbers = true,
  maxHeight,
  fillHeight = false,
  diffLines,
}: Props) {
  const mountRef = useRef<HTMLDivElement | null>(null)
  const viewRef = useRef<import('@codemirror/view').EditorView | null>(null)
  const [runtime, setRuntime] = useState<CodeMirrorRuntime | null>(null)
  const language = useMemo(() => codeLanguageFromFilename(filename, mimeType), [filename, mimeType])

  useEffect(() => {
    let cancelled = false
    loadCodeMirrorRuntime().then((loaded) => {
      if (!cancelled) setRuntime(loaded)
    })
    return () => { cancelled = true }
  }, [])

  useEffect(() => {
    const mount = mountRef.current
    if (!mount || !runtime) return

    const extensions: Extension[] = [
      runtime.highlightSpecialChars(),
      runtime.drawSelection(),
      runtime.EditorView.lineWrapping,
      runtime.EditorState.readOnly.of(true),
      runtime.EditorView.editable.of(false),
      runtime.EditorView.contentAttributes.of({ spellcheck: 'false' }),
      runtime.EditorView.theme({}, { dark: false }),
      viewerTheme(runtime, maxHeight, fillHeight),
      highlightTheme(runtime),
      diffLineExtension(runtime, diffLines),
      ...languageExtension(runtime, language),
    ]
    if (showLineNumbers) extensions.unshift(runtime.lineNumbers())

    const view = new runtime.EditorView({
      parent: mount,
      state: runtime.EditorState.create({ doc: content, extensions }),
    })
    view.dom.classList.add('ark-code-viewer')
    viewRef.current = view

    return () => {
      view.destroy()
      if (viewRef.current === view) viewRef.current = null
    }
  }, [diffLines, fillHeight, language, maxHeight, mimeType, runtime, showLineNumbers])

  useEffect(() => {
    const view = viewRef.current
    if (!view) return
    const current = view.state.doc.toString()
    if (current === content) return
    view.dispatch({
      changes: { from: 0, to: view.state.doc.length, insert: content },
    })
  }, [content])

  if (!runtime) {
    return (
      <div className={`code-document-viewer${fillHeight ? ' code-document-viewer--fill' : ''}`} style={{ maxHeight }}>
        <pre className="code-document-viewer__fallback">{content}</pre>
      </div>
    )
  }

  return (
    <div className={`code-document-viewer${fillHeight ? ' code-document-viewer--fill' : ''} code-document-viewer--readonly`} style={{ maxHeight }}>
      <div ref={mountRef} className="code-document-viewer__mount" />
    </div>
  )
})
