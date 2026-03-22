import { useState, type ReactNode } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { jsonStringifyForDebugDisplay, redactDataUrlsInString } from '../debugPayloadRedact'

type CollapseBlockProps = {
  label: string
  preview?: string
  defaultOpen?: boolean
  children: ReactNode
  dim?: boolean
}

export function CollapseBlock({ label, preview, defaultOpen = false, children, dim }: CollapseBlockProps) {
  const [open, setOpen] = useState(defaultOpen)

  return (
    <div className="overflow-hidden rounded border border-[var(--c-border)]">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={[
          'flex w-full items-start gap-1.5 px-2.5 py-1.5 text-left transition-colors hover:bg-[var(--c-bg-sub)]',
          dim ? 'opacity-60' : '',
        ].join(' ')}
      >
        <span className="mt-0.5 shrink-0 text-[var(--c-text-muted)]">
          {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        </span>
        <span className="text-[11px] font-medium text-[var(--c-text-secondary)]">{label}</span>
        {!open && preview && (
          <span className="ml-1 truncate text-[11px] text-[var(--c-text-muted)]">{preview}</span>
        )}
      </button>
      {open && (
        <div className="border-t border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-2">
          {children}
        </div>
      )}
    </div>
  )
}

export function PreText({ text }: { text: string }) {
  const safe = redactDataUrlsInString(text)
  return (
    <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-[var(--c-text-secondary)]">
      {safe}
    </pre>
  )
}

export function JsonBlock({ value }: { value: unknown }) {
  return (
    <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-[var(--c-text-secondary)]">
      {jsonStringifyForDebugDisplay(value, 2)}
    </pre>
  )
}
