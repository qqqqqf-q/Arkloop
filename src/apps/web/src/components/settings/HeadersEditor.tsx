import type { ReactNode } from 'react'
import { ChevronRight, Minus, Plus } from 'lucide-react'
import { SettingsButton, SettingsIconButton } from './_SettingsButton'
import { SettingsInput } from './_SettingsInput'

export const OPENVIKING_EXTRA_HEADERS_KEY = 'openviking_extra_headers'

export type HeaderEntry = {
  key: string
  value: string
}

export function readHeaderEntriesFromAdvancedJSON(advanced: Record<string, unknown> | null | undefined): HeaderEntry[] {
  const raw = advanced?.[OPENVIKING_EXTRA_HEADERS_KEY]
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return []
  return Object.entries(raw as Record<string, unknown>)
    .filter(([, value]) => typeof value === 'string')
    .map(([key, value]) => ({ key, value: value as string }))
}

export function writeHeaderEntriesToAdvancedJSON(
  advanced: Record<string, unknown> | null | undefined,
  headers: HeaderEntry[],
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...(advanced ?? {}) }
  const cleaned: Record<string, string> = {}
  for (const header of headers) {
    const key = header.key.trim()
    const value = header.value.trim()
    if (!key || !value) continue
    cleaned[key] = value
  }
  if (Object.keys(cleaned).length === 0) {
    delete next[OPENVIKING_EXTRA_HEADERS_KEY]
  } else {
    next[OPENVIKING_EXTRA_HEADERS_KEY] = cleaned
  }
  return next
}

export function stripHeaderEntriesFromAdvancedJSON(
  advanced: Record<string, unknown> | null | undefined,
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...(advanced ?? {}) }
  delete next[OPENVIKING_EXTRA_HEADERS_KEY]
  return next
}

export function advancedJSONSignature(advanced: Record<string, unknown> | null | undefined): string {
  return JSON.stringify(advanced ?? {})
}

type HeadersEditorProps = {
  headers: HeaderEntry[]
  onChange: (next: HeaderEntry[]) => void
  addLabel: string
  removeLabel?: string
  keyPlaceholder: string
  valuePlaceholder: string
}

export function HeadersEditor({
  headers,
  onChange,
  addLabel,
  removeLabel,
  keyPlaceholder,
  valuePlaceholder,
}: HeadersEditorProps) {
  const update = (idx: number, patch: Partial<HeaderEntry>) => {
    onChange(headers.map((header, index) => (index === idx ? { ...header, ...patch } : header)))
  }
  const remove = (idx: number) => {
    onChange(headers.filter((_, index) => index !== idx))
  }

  return (
    <div className="space-y-2">
      {headers.map((header, idx) => (
        <div key={idx} className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-2">
          <SettingsInput
            variant="md"
            value={header.key}
            onChange={(e) => update(idx, { key: e.target.value })}
            placeholder={keyPlaceholder}
          />
          <SettingsInput
            variant="md"
            value={header.value}
            onChange={(e) => update(idx, { value: e.target.value })}
            placeholder={valuePlaceholder}
          />
          <SettingsIconButton label={removeLabel ?? addLabel} variant="plain" danger onClick={() => remove(idx)}>
            <Minus size={13} />
          </SettingsIconButton>
        </div>
      ))}
      <SettingsButton
        type="button"
        onClick={() => onChange([...headers, { key: '', value: '' }])}
        size="modal"
        icon={<Plus size={13} />}
      >
        {addLabel}
      </SettingsButton>
    </div>
  )
}

type AdvancedOptionsDisclosureProps = {
  label: string
  open: boolean
  onToggle: () => void
  children: ReactNode
}

export function AdvancedOptionsDisclosure({
  label,
  open,
  onToggle,
  children,
}: AdvancedOptionsDisclosureProps) {
  return (
    <div className="col-span-2">
      <button
        type="button"
        onClick={onToggle}
        className="group/folder -ml-2 flex h-[34px] w-[calc(100%+16px)] cursor-pointer select-none items-center px-2 py-0 text-left"
      >
        <span
          className="select-none text-[13.5px] leading-[20px] text-[var(--c-text-muted)] transition-colors duration-[80ms] group-hover/folder:text-[var(--c-text-tertiary)]"
          style={{ fontWeight: 500 }}
        >
          {label}
        </span>
        <span className="ml-1 shrink-0 text-[var(--c-text-muted)] opacity-0 transition-opacity duration-[80ms] group-hover/folder:opacity-100">
          <ChevronRight
            size={12}
            className={['transition-transform duration-150', open ? 'rotate-90' : 'rotate-0'].join(' ')}
          />
        </span>
      </button>
      {open && <div className="space-y-3">{children}</div>}
    </div>
  )
}
