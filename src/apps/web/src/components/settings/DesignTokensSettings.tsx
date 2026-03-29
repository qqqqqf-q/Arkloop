import { useState, useEffect, useRef } from 'react'
import {
  Loader2, Check, Trash2, Eye, EyeOff, ChevronDown,
  Plus, Settings, Info, Globe, Search, Key,
  CheckCircle, XCircle,
} from 'lucide-react'
import { SettingsPillToggle } from './_SettingsPillToggle'
import { SettingsSectionHeader } from './_SettingsSectionHeader'

// ---------------------------------------------------------------------------
// Real dropdown: ModelDropdown — verbatim from RoutingSettings.tsx
// ---------------------------------------------------------------------------

type Option = { value: string; label: string }

function ModelDropdown({
  value, options, placeholder, disabled, onChange,
}: {
  value: string
  options: Option[]
  placeholder: string
  disabled?: boolean
  onChange: (v: string) => void
}) {
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const btnRef = useRef<HTMLButtonElement>(null)
  const currentLabel = value === '' ? placeholder : (options.find(o => o.value === value)?.label ?? value)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (menuRef.current?.contains(e.target as Node) || btnRef.current?.contains(e.target as Node)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  return (
    <div className="relative flex-1">
      <button
        ref={btnRef}
        type="button"
        disabled={disabled}
        onClick={() => setOpen(v => !v)}
        className="flex h-9 w-full items-center justify-between rounded-lg px-3 text-sm transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-50"
        style={{
          border: '0.5px solid var(--c-border-subtle)',
          background: 'var(--c-bg-page)',
          color: value === '' ? 'var(--c-text-tertiary)' : 'var(--c-text-heading)',
        }}
      >
        <span className="truncate">{currentLabel}</span>
        <ChevronDown size={12} className="ml-1 shrink-0 text-[var(--c-text-muted)]" />
      </button>
      {open && (
        <div
          ref={menuRef}
          className="dropdown-menu absolute left-0 top-[calc(100%+4px)] z-50 max-h-60 overflow-y-auto"
          style={{
            border: '0.5px solid var(--c-border-subtle)',
            borderRadius: '10px',
            padding: '4px',
            background: 'var(--c-bg-menu)',
            minWidth: '100%',
            boxShadow: 'var(--c-dropdown-shadow)',
          }}
        >
          <button
            type="button"
            onClick={() => { onChange(''); setOpen(false) }}
            className="flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
            style={{ color: value === '' ? 'var(--c-text-heading)' : 'var(--c-text-secondary)', fontWeight: value === '' ? 500 : 400 }}
          >
            <span>{placeholder}</span>
            {value === '' && <Check size={12} className="shrink-0" />}
          </button>
          {options.map(o => (
            <button
              key={o.value}
              type="button"
              onClick={() => { onChange(o.value); setOpen(false) }}
              className="flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
              style={{ color: value === o.value ? 'var(--c-text-heading)' : 'var(--c-text-secondary)', fontWeight: value === o.value ? 500 : 400 }}
            >
              <span className="truncate">{o.label}</span>
              {value === o.value && <Check size={12} className="shrink-0" />}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Real dropdown: VendorDropdown — verbatim from ProvidersSettings.tsx
// ---------------------------------------------------------------------------

function VendorDropdown({
  value, options, onChange,
}: {
  value: string
  options: { key: string; label: string }[]
  onChange: (v: string) => void
}) {
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const btnRef = useRef<HTMLButtonElement>(null)
  const currentLabel = options.find(o => o.key === value)?.label ?? value

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (menuRef.current?.contains(e.target as Node) || btnRef.current?.contains(e.target as Node)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  return (
    <div className="relative">
      <button
        ref={btnRef}
        type="button"
        onClick={() => setOpen(v => !v)}
        className="flex w-full items-center justify-between rounded-md bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-deep)]"
        style={{ border: '1px solid var(--c-border-subtle)' }}
      >
        <span className="truncate">{currentLabel}</span>
        <ChevronDown size={13} className="ml-2 shrink-0 text-[var(--c-text-muted)]" />
      </button>
      {open && (
        <div
          ref={menuRef}
          className="dropdown-menu absolute left-0 top-[calc(100%+4px)] z-50 min-w-full"
          style={{
            border: '0.5px solid var(--c-border-subtle)',
            borderRadius: '10px',
            padding: '4px',
            background: 'var(--c-bg-menu)',
            boxShadow: 'var(--c-dropdown-shadow)',
          }}
        >
          {options.map((v) => (
            <button
              key={v.key}
              type="button"
              onClick={() => { onChange(v.key); setOpen(false) }}
              className="flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
              style={{ color: value === v.key ? 'var(--c-text-heading)' : 'var(--c-text-secondary)', fontWeight: value === v.key ? 500 : 400 }}
            >
              <span>{v.label}</span>
              {value === v.key && <Check size={13} className="shrink-0" />}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Real expand panel — verbatim from SearchFetchSettings.tsx
// ---------------------------------------------------------------------------

function ExpandPanel({ open, children }: { open: boolean; children: React.ReactNode }) {
  return (
    <div
      className="overflow-hidden transition-[grid-template-rows] duration-200 ease-in-out"
      style={{ display: 'grid', gridTemplateRows: open ? '1fr' : '0fr' }}
    >
      <div className="overflow-hidden">
        <div className="border-t border-[var(--c-border-subtle)] px-4 pb-4 pt-3">
          {children}
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Real status badge — verbatim from SearchFetchSettings.tsx
// ---------------------------------------------------------------------------

type BadgeVariant = 'free' | 'configured' | 'always' | 'missing'

const BADGE: Record<BadgeVariant, { cls: string; dot: string; label: string }> = {
  free:       { cls: 'bg-blue-500/15 text-blue-400',                                dot: 'bg-blue-400',               label: 'Free tier' },
  configured: { cls: 'bg-green-500/15 text-green-400',                              dot: 'bg-green-400',              label: 'Configured' },
  always:     { cls: 'bg-green-500/15 text-green-400',                              dot: 'bg-green-400',              label: 'Always on' },
  missing:    { cls: 'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]',            dot: 'bg-[var(--c-text-muted)]', label: 'Not configured' },
}

function StatusBadge({ variant }: { variant: BadgeVariant }) {
  const s = BADGE[variant]
  return (
    <span className={`inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${s.cls}`}>
      <span className={`inline-block h-1.5 w-1.5 rounded-full ${s.dot}`} />
      {s.label}
    </span>
  )
}

// ---------------------------------------------------------------------------
// Real provider card — verbatim from SearchFetchSettings.tsx
// ---------------------------------------------------------------------------

function ProviderCard({
  icon, title, description, badge, selected, onSelect, children,
}: {
  icon: React.ReactNode
  title: string
  description: string
  badge: BadgeVariant
  selected: boolean
  onSelect: () => void
  children?: React.ReactNode
}) {
  return (
    <div
      className="rounded-xl transition-[border-color] duration-150"
      style={{
        border: selected ? '1.5px solid var(--c-accent)' : '1px solid var(--c-border-subtle)',
        background: 'var(--c-bg-menu)',
      }}
    >
      <button
        type="button"
        onClick={onSelect}
        className="flex w-full items-start gap-3 rounded-xl p-4 text-left transition-colors duration-100 hover:bg-[var(--c-bg-deep)]/40"
      >
        <div
          className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center transition-colors duration-150"
          style={{ color: selected ? 'var(--c-accent)' : 'var(--c-text-muted)' }}
        >
          {icon}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-medium text-[var(--c-text-heading)]">{title}</span>
            <StatusBadge variant={badge} />
          </div>
          <p className="mt-0.5 text-xs leading-relaxed text-[var(--c-text-muted)]">{description}</p>
        </div>
        {/* Radio knob */}
        <div
          className="mt-0.5 h-4 w-4 shrink-0 rounded-full transition-[border-width,border-color] duration-150"
          style={{
            border: selected
              ? '4px solid var(--c-accent)'
              : '1.5px solid var(--c-border-mid)',
          }}
        />
      </button>
      {children && <ExpandPanel open={selected}>{children}</ExpandPanel>}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Shared style constants — copied verbatim from each settings file
// ---------------------------------------------------------------------------

import { settingsInputCls } from './_SettingsInput'
import { settingsLabelCls } from './_SettingsLabel'
import { settingsSectionCls } from './_SettingsSection'

// ProvidersSettings / ConnectorsSettings
const INPUT_CLS = settingsInputCls('sm')

// SearchFetchSettings
const INPUT_CLS_LG = settingsInputCls('md') + ' transition-colors duration-150'

// ProvidersSettings AddProviderModal
const FIELD_INPUT_CLS = 'w-full rounded-[10px] bg-[var(--c-bg-input)] text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-placeholder)]'
const FIELD_INPUT_STYLE = {
  border: '0.5px solid var(--c-border-auth)',
  height: '36px',
  padding: '0 14px',
  fontSize: '13px',
  fontWeight: 500,
  fontFamily: 'inherit',
} as const
const FIELD_LABEL_CLS = 'block text-[11px] font-medium text-[var(--c-placeholder)] mb-1 pl-[2px]'

// ConnectorsSettings
const LABEL_CLS = settingsLabelCls('sm')
const SECTION_CLS = settingsSectionCls
const BTN_ICON = 'rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)] disabled:opacity-40'

// SearchFetchSettings
const LABEL_CLS_LG = settingsLabelCls('md')

// ChatSettings
const CARD_SHELL = 'overflow-hidden rounded-xl border-[0.5px] border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]'
const RANGE_CLASS =
  'h-2 w-full min-w-0 cursor-pointer appearance-none rounded-full bg-[var(--c-bg-deep)] ' +
  '[&::-webkit-slider-thumb]:h-3.5 [&::-webkit-slider-thumb]:w-3.5 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full ' +
  '[&::-webkit-slider-thumb]:border-0 [&::-webkit-slider-thumb]:bg-[var(--c-accent)] [&::-webkit-slider-thumb]:shadow-sm ' +
  '[&::-moz-range-thumb]:h-3.5 [&::-moz-range-thumb]:w-3.5 [&::-moz-range-thumb]:rounded-full [&::-moz-range-thumb]:border-0 ' +
  '[&::-moz-range-thumb]:bg-[var(--c-accent)] ' +
  '[&::-moz-range-track]:h-2 [&::-moz-range-track]:rounded-full [&::-moz-range-track]:bg-[var(--c-bg-deep)]'

// ---------------------------------------------------------------------------
// Sub-section wrapper
// ---------------------------------------------------------------------------

function PreviewSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <div className="text-[11px] font-semibold uppercase tracking-wider text-[var(--c-text-muted)]">
        {title}
      </div>
      {children}
    </section>
  )
}

// ---------------------------------------------------------------------------
// 1. Typography
// ---------------------------------------------------------------------------

function TypographyPreview() {
  return (
    <PreviewSection title="Typography">
      <div className={SECTION_CLS + ' flex flex-col gap-1.5'}>
        <SettingsSectionHeader title="Section header" description="Optional description text below the title." />
        <div className="mt-2 border-t border-[var(--c-border-subtle)]" />
        <div className="mt-2 flex flex-col gap-1">
          <span className="text-xl font-semibold text-[var(--c-text-heading)]">Heading · --c-text-heading</span>
          <span className="text-sm text-[var(--c-text-primary)]">Primary · --c-text-primary</span>
          <span className="text-sm text-[var(--c-text-secondary)]">Secondary · --c-text-secondary</span>
          <span className="text-sm text-[var(--c-text-tertiary)]">Tertiary · --c-text-tertiary</span>
          <span className="text-sm text-[var(--c-text-muted)]">Muted · --c-text-muted</span>
          <span className="text-sm text-[var(--c-text-icon)]">Icon · --c-text-icon</span>
          {/* FIELD_LABEL_CLS — ProvidersSettings AddProviderModal */}
          <span className={FIELD_LABEL_CLS}>Field label (modal) · FIELD_LABEL_CLS · 11px --c-placeholder</span>
          {/* LABEL_CLS — ConnectorsSettings */}
          <span className={LABEL_CLS}>Label (connectors) · LABEL_CLS · xs --c-text-secondary</span>
          {/* LABEL_CLS_LG — SearchFetchSettings */}
          <span className={LABEL_CLS_LG}>Label (search) · LABEL_CLS_LG · xs --c-text-secondary mb-1.5</span>
          <span className="text-xs font-semibold uppercase tracking-wider text-[var(--c-text-muted)]">
            Section label · 11px uppercase tracking-wider
          </span>
          <code
            className="mt-1 self-start rounded-md px-2 py-0.5 text-sm font-mono"
            style={{ background: 'var(--c-md-inline-code-bg)', color: 'var(--c-md-inline-code-color)', border: '0.5px solid var(--c-border-subtle)' }}
          >
            inline code
          </code>
        </div>
      </div>
    </PreviewSection>
  )
}

// ---------------------------------------------------------------------------
// 2. Buttons
// ---------------------------------------------------------------------------

function ButtonsPreview() {
  const [loading, setLoading] = useState(false)
  const [execResult, setExecResult] = useState<'ok' | 'error' | null>(null)

  function simulateLoad() {
    setLoading(true)
    setTimeout(() => setLoading(false), 1500)
  }

  function simulateExecResult(r: 'ok' | 'error') {
    setExecResult(r)
    setTimeout(() => setExecResult(null), 2000)
  }

  return (
    <PreviewSection title="Buttons">
      <div className={SECTION_CLS + ' flex flex-col gap-5'}>

        {/* Primary — bg-[--c-btn-bg] */}
        <div className="flex flex-col gap-1.5">
          <span className={LABEL_CLS}>Primary · rounded-[9px] bg-[--c-btn-bg] px-4 py-1.5 text-sm (ProvidersSettings)</span>
          <div className="flex flex-wrap gap-2">
            <button
              className="flex items-center gap-2 rounded-[9px] px-4 py-1.5 text-sm font-medium"
              style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
            >
              Save
            </button>
            <button
              onClick={simulateLoad}
              className="flex items-center gap-2 rounded-[9px] px-4 py-1.5 text-sm font-medium"
              style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
            >
              {loading ? <Loader2 size={14} className="animate-spin" /> : null}
              {loading ? 'Saving…' : 'Save with state'}
            </button>
            <button
              className="flex items-center gap-2 rounded-[9px] px-4 py-1.5 text-sm font-medium opacity-50 cursor-not-allowed"
              style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
              disabled
            >
              Disabled
            </button>
          </div>
        </div>

        {/* Secondary */}
        <div className="flex flex-col gap-1.5">
          <span className={LABEL_CLS}>Secondary · rounded-[9px] border-[--c-border-subtle] px-4 py-1.5 text-sm</span>
          <div className="flex flex-wrap gap-2">
            <button
              className="rounded-[9px] px-4 py-1.5 text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              Cancel
            </button>
            <button
              className="flex items-center gap-1.5 rounded-[9px] px-4 py-1.5 text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              <Plus size={14} />
              Add provider
            </button>
          </div>
        </div>

        {/* ProvidersSettings left list add button */}
        <div className="flex flex-col gap-1.5">
          <span className={LABEL_CLS}>Add item · flex h-8 w-full rounded-md text-[13px] --c-text-muted (ProvidersSettings sidebar)</span>
          <div className="w-[200px] border-t border-[var(--c-border-subtle)] px-3 py-3" style={{ background: 'var(--c-bg-sidebar)' }}>
            <button className="flex h-8 w-full items-center justify-center gap-1.5 rounded-md text-[13px] text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]">
              <Plus size={14} />
              Add provider
            </button>
          </div>
        </div>

        {/* Destructive variants */}
        <div className="flex flex-col gap-1.5">
          <span className={LABEL_CLS}>Destructive — 3 variants in use (inconsistent)</span>
          <div className="flex flex-wrap gap-2">
            {/* A: bg-red-600 */}
            <button className="flex items-center gap-1.5 rounded-[9px] bg-red-600 px-4 py-1.5 text-sm font-medium text-white hover:bg-red-700">
              <Trash2 size={14} />
              bg-red-600 (ProvidersSettings)
            </button>
            {/* B: text-red-400 ghost */}
            <button className="flex items-center gap-1.5 rounded-[9px] px-4 py-1.5 text-sm font-medium text-red-400 transition-colors hover:bg-[var(--c-bg-deep)]">
              <Trash2 size={14} />
              text-red-400 ghost (ConnectorsSettings)
            </button>
            {/* C: status-danger-text */}
            <button
              className="rounded-[9px] px-4 py-1.5 text-sm font-medium transition-colors hover:bg-[var(--c-bg-deep)]"
              style={{ color: 'var(--c-status-danger-text)' }}
            >
              --c-status-danger-text
            </button>
          </div>
        </div>

        {/* Icon buttons */}
        <div className="flex flex-col gap-1.5">
          <span className={LABEL_CLS}>Icon buttons · BTN_ICON · rounded p-1 (ConnectorsSettings)</span>
          <div className="flex items-center gap-1">
            <button className={BTN_ICON}><Settings size={14} /></button>
            <button className={BTN_ICON}><Trash2 size={14} /></button>
            <button className={BTN_ICON}><Plus size={14} /></button>
            <button className={BTN_ICON} disabled><Settings size={14} /></button>
          </div>
        </div>

        {/* Small action — DeveloperSettings "查看" style */}
        <div className="flex flex-col gap-1.5">
          <span className={LABEL_CLS}>Small action · rounded-md px-2 py-1.5 text-xs (DeveloperSettings)</span>
          <div className="flex flex-wrap gap-2">
            <button
              className="rounded-md px-2 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            >
              查看
            </button>
            <button className="rounded-md bg-[var(--c-btn-bg)] px-2 py-1.5 text-xs font-medium text-[var(--c-btn-text)]">
              Reset
            </button>
          </div>
        </div>

        {/* Inline save result — ChatSettings execSaveResult pattern */}
        <div className="flex flex-col gap-1.5">
          <span className={LABEL_CLS}>Inline save result · CheckCircle / XCircle text-xs (ChatSettings)</span>
          <div className="flex gap-2">
            <button onClick={() => simulateExecResult('ok')} className={BTN_ICON + ' text-xs'}>Simulate ok</button>
            <button onClick={() => simulateExecResult('error')} className={BTN_ICON + ' text-xs'}>Simulate error</button>
          </div>
          {execResult === 'ok' && (
            <span className="flex items-center gap-1 text-xs" style={{ color: 'var(--c-status-ok-text)' }}>
              <CheckCircle size={12} /> Saved
            </span>
          )}
          {execResult === 'error' && (
            <span className="flex items-center gap-1 text-xs" style={{ color: 'var(--c-status-danger-text)' }}>
              <XCircle size={12} /> Failed
            </span>
          )}
        </div>
      </div>
    </PreviewSection>
  )
}

// ---------------------------------------------------------------------------
// 3. Inputs
// ---------------------------------------------------------------------------

function InputsPreview() {
  const [showPw, setShowPw] = useState(false)
  const [modelVal, setModelVal] = useState('')
  const [vendorVal, setVendorVal] = useState('openai_responses')
  const [rangeVal, setRangeVal] = useState(80)
  const [numVal, setNumVal] = useState(4)
  const [textSm, setTextSm] = useState('')
  const [textLg, setTextLg] = useState('')
  const [textModal, setTextModal] = useState('')

  const MODEL_OPTIONS: Option[] = [
    { value: 'p1^claude-opus-4-6', label: 'provider1 / claude-opus-4-6' },
    { value: 'p1^claude-sonnet-4-6', label: 'provider1 / claude-sonnet-4-6' },
    { value: 'p1^claude-haiku-4-5', label: 'provider1 / claude-haiku-4-5' },
  ]

  const VENDOR_OPTIONS = [
    { key: 'openai_responses', label: 'OpenAI (Responses API)' },
    { key: 'openai_chat_completions', label: 'OpenAI (Chat Completions)' },
    { key: 'anthropic_message', label: 'Anthropic' },
    { key: 'gemini', label: 'Gemini' },
  ]

  return (
    <PreviewSection title="Inputs">
      <div className={SECTION_CLS + ' flex flex-col gap-5'}>

        {/* INPUT_CLS — ProvidersSettings / ConnectorsSettings · rounded-md py-1.5 */}
        <div>
          <label className={LABEL_CLS}>TEXT · INPUT_CLS · rounded-md border py-1.5 (ProvidersSettings, ConnectorsSettings)</label>
          <input className={INPUT_CLS} placeholder="e.g. https://api.openai.com/v1" value={textSm} onChange={e => setTextSm(e.target.value)} />
        </div>

        {/* INPUT_CLS_LG — SearchFetchSettings · rounded-lg py-2 */}
        <div>
          <label className={LABEL_CLS}>TEXT LARGER · INPUT_CLS_LG · rounded-lg py-2 (SearchFetchSettings)</label>
          <input className={INPUT_CLS_LG} placeholder="API key…" value={textLg} onChange={e => setTextLg(e.target.value)} />
        </div>

        {/* FIELD_INPUT_CLS — ProvidersSettings AddProviderModal · rounded-[10px] h-36px border-auth */}
        <div>
          <label className={FIELD_LABEL_CLS}>TEXT MODAL · FIELD_INPUT_CLS · rounded-[10px] h-[36px] border-auth (ProvidersSettings modal)</label>
          <input className={FIELD_INPUT_CLS} style={FIELD_INPUT_STYLE} placeholder="My Provider" value={textModal} onChange={e => setTextModal(e.target.value)} />
        </div>

        {/* Password input — ConnectorsSettings Eye toggle */}
        <div>
          <label className={LABEL_CLS}>PASSWORD · Eye/EyeOff toggle · INPUT_CLS + pr-9 (ConnectorsSettings)</label>
          <div className="relative">
            <input
              className={INPUT_CLS + ' pr-9'}
              type={showPw ? 'text' : 'password'}
              placeholder="sk-proj-xxxxxxxxxxxxxxxx"
            />
            <button
              type="button"
              onClick={() => setShowPw(s => !s)}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)] hover:text-[var(--c-text-secondary)]"
            >
              {showPw ? <EyeOff size={14} /> : <Eye size={14} />}
            </button>
          </div>
        </div>

        {/* Number input — ChatSettings · h-9 w-14 */}
        <div>
          <label className={LABEL_CLS}>NUMBER · h-9 w-14 rounded-md border (ChatSettings)</label>
          <input
            type="number"
            min={2}
            max={50}
            value={numVal}
            onChange={e => setNumVal(Number(e.target.value))}
            className="h-9 w-14 shrink-0 rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-1 text-center text-sm tabular-nums text-[var(--c-text-primary)] outline-none focus:border-[var(--c-border)]"
          />
        </div>

        {/* Range — ChatSettings · RANGE_CLASS */}
        <div>
          <label className={LABEL_CLS}>RANGE · RANGE_CLASS · h-2 rounded-full bg-[--c-bg-deep] thumb bg-[--c-accent] (ChatSettings)</label>
          <div className="flex items-center gap-3">
            <span className="w-9 shrink-0 text-center text-[10px] font-medium uppercase tracking-wide text-[var(--c-text-muted)]">Early</span>
            <input
              type="range"
              min={5}
              max={100}
              step={1}
              value={rangeVal}
              onChange={e => setRangeVal(Number(e.target.value))}
              className={RANGE_CLASS}
            />
            <span className="w-9 shrink-0 text-center text-[10px] font-medium uppercase tracking-wide text-[var(--c-text-muted)]">Late</span>
            <span className="shrink-0 rounded-md bg-[var(--c-bg-deep)] px-2.5 py-0.5 text-xs font-medium tabular-nums text-[var(--c-text-secondary)]">
              {rangeVal}%
            </span>
          </div>
        </div>

        {/* ModelDropdown — RoutingSettings · flex h-9 rounded-lg */}
        <div>
          <label className={LABEL_CLS}>DROPDOWN (ModelDropdown) · flex h-9 rounded-lg bg-page border-subtle (RoutingSettings)</label>
          <div className="flex items-center gap-3">
            <span className="w-20 shrink-0 text-sm text-[var(--c-text-secondary)]">explore</span>
            <ModelDropdown
              value={modelVal}
              options={MODEL_OPTIONS}
              placeholder="Platform default"
              onChange={setModelVal}
            />
          </div>
        </div>

        {/* VendorDropdown — ProvidersSettings · rounded-md bg-input border py-1.5 */}
        <div>
          <label className={LABEL_CLS}>DROPDOWN (VendorDropdown) · rounded-md bg-input border-1px py-1.5 (ProvidersSettings modal)</label>
          <VendorDropdown
            value={vendorVal}
            options={VENDOR_OPTIONS}
            onChange={setVendorVal}
          />
        </div>
      </div>
    </PreviewSection>
  )
}

// ---------------------------------------------------------------------------
// 4. Toggle rows (ChatSettings cardShell pattern)
// ---------------------------------------------------------------------------

function TogglesPreview() {
  const [autoOn, setAutoOn] = useState(true)
  const [vmMode, setVmMode] = useState(false)
  const [showDetail, setShowDetail] = useState(true)

  return (
    <PreviewSection title="Toggle Rows (ChatSettings · CARD_SHELL)">
      <div>
        <span className={LABEL_CLS}>CARD_SHELL · overflow-hidden rounded-xl border-[0.5px] bg-[--c-bg-menu]</span>
        <div className={CARD_SHELL}>
          {/* Clickable row */}
          <div
            role="button"
            tabIndex={0}
            className="flex cursor-pointer items-center justify-between gap-4 px-4 py-4 outline-none transition-colors hover:bg-[var(--c-bg-deep)]/25"
            onClick={() => setAutoOn(v => !v)}
            onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setAutoOn(v => !v) } }}
          >
            <div className="min-w-0 flex-1 pr-2">
              <p className="text-sm font-medium text-[var(--c-text-heading)]">Auto-compact context</p>
              <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">Automatically compress when context window fills up.</p>
            </div>
            <div className="shrink-0" onClick={e => e.stopPropagation()}>
              <SettingsPillToggle checked={autoOn} onChange={setAutoOn} />
            </div>
          </div>

          {/* Dependent section — disabled when toggle is off */}
          <div className={`flex flex-col gap-3 border-t border-[var(--c-border-subtle)] px-4 py-4 transition-opacity ${autoOn ? '' : 'pointer-events-none opacity-40'}`}>
            <div className="flex items-center justify-between gap-3">
              <span className="text-sm font-medium text-[var(--c-text-heading)]">Trigger threshold</span>
              <span className="shrink-0 rounded-md bg-[var(--c-bg-deep)] px-2.5 py-0.5 text-xs font-medium tabular-nums text-[var(--c-text-secondary)]">
                80%
              </span>
            </div>
            <div className="flex items-center gap-3">
              <span className="w-9 shrink-0 text-center text-[10px] font-medium uppercase tracking-wide text-[var(--c-text-muted)]">Early</span>
              <input type="range" min={5} max={100} defaultValue={80} className={RANGE_CLASS} />
              <span className="w-9 shrink-0 text-center text-[10px] font-medium uppercase tracking-wide text-[var(--c-text-muted)]">Late</span>
            </div>
          </div>

          {/* Number input row */}
          <div className={`flex items-center justify-between gap-4 border-t border-[var(--c-border-subtle)] px-4 py-4 transition-opacity ${autoOn ? '' : 'pointer-events-none opacity-40'}`}>
            <div className="min-w-0 flex-1 pr-2">
              <p className="text-sm font-medium text-[var(--c-text-heading)]">Keep last N messages</p>
              <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">Messages preserved after compaction.</p>
            </div>
            <input
              type="number"
              min={2}
              max={50}
              defaultValue={4}
              className="h-9 w-14 shrink-0 rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-1 text-center text-sm tabular-nums text-[var(--c-text-primary)] outline-none"
            />
          </div>

          {/* Execution mode row with loading pulse skeleton */}
          <div className="flex items-center justify-between gap-4 border-t border-[var(--c-border-subtle)] px-4 py-4">
            <div className="min-w-0 flex-1 pr-2">
              <p className="text-sm font-medium text-[var(--c-text-heading)]">Sandbox mode</p>
              <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">
                {vmMode ? 'Firecracker VM sandbox' : 'Local terminal execution'}
              </p>
            </div>
            <SettingsPillToggle checked={vmMode} onChange={setVmMode} />
          </div>

          {/* Show run detail row — inline toggle (DeveloperSettings pattern) */}
          <div className="flex items-center justify-between gap-4 border-t border-[var(--c-border-subtle)] px-4 py-4">
            <div className="min-w-0 flex-1 pr-2">
              <p className="text-sm font-medium text-[var(--c-text-heading)]">Show run detail button</p>
              <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">Add a debug button on AI messages.</p>
            </div>
            <SettingsPillToggle checked={showDetail} onChange={setShowDetail} />
          </div>
        </div>
      </div>
    </PreviewSection>
  )
}

// ---------------------------------------------------------------------------
// 5. Provider card (SearchFetchSettings pattern)
// ---------------------------------------------------------------------------

function ProviderCardPreview() {
  const [selected, setSelected] = useState('brave')
  const [braveKey, setBraveKey] = useState('')
  const [tavilyKey, setTavilyKey] = useState('')

  return (
    <PreviewSection title="Provider Card (SearchFetchSettings · ProviderCard + ExpandPanel + StatusBadge)">
      <div className="flex flex-col gap-2">
        <ProviderCard
          icon={<Search size={16} />}
          title="Brave Search"
          description="Privacy-first web search. Free tier available."
          badge="configured"
          selected={selected === 'brave'}
          onSelect={() => setSelected('brave')}
        >
          <div className="flex flex-col gap-3">
            <div>
              <label className={LABEL_CLS_LG}>API Key</label>
              <input className={INPUT_CLS_LG} placeholder="BSA…" value={braveKey} onChange={e => setBraveKey(e.target.value)} />
            </div>
          </div>
        </ProviderCard>

        <ProviderCard
          icon={<Globe size={16} />}
          title="Tavily"
          description="AI-optimised search with answer synthesis."
          badge="missing"
          selected={selected === 'tavily'}
          onSelect={() => setSelected('tavily')}
        >
          <div className="flex flex-col gap-3">
            <div>
              <label className={LABEL_CLS_LG}>API Key</label>
              <input className={INPUT_CLS_LG} placeholder="tvly-…" value={tavilyKey} onChange={e => setTavilyKey(e.target.value)} />
            </div>
          </div>
        </ProviderCard>

        <ProviderCard
          icon={<Key size={16} />}
          title="Jina"
          description="Neural search with free tier."
          badge="free"
          selected={selected === 'jina'}
          onSelect={() => setSelected('jina')}
        />
      </div>
    </PreviewSection>
  )
}

// ---------------------------------------------------------------------------
// 6. List items / sidebar nav (ProvidersSettings left list)
// ---------------------------------------------------------------------------

function ListNavPreview() {
  const [active, setActive] = useState('openai')
  const items = ['openai', 'anthropic', 'gemini']

  return (
    <PreviewSection title="Left-list Nav (ProvidersSettings · h-[34px] rounded-[5px] px-3 text-[13px])">
      <div className="flex overflow-hidden rounded-xl" style={{ border: '0.5px solid var(--c-border-subtle)', height: 160 }}>
        {/* Left */}
        <div className="flex w-[200px] shrink-0 flex-col overflow-hidden" style={{ borderRight: '0.5px solid var(--c-border-subtle)' }}>
          <div className="flex-1 overflow-y-auto px-2 py-1">
            <div className="flex flex-col gap-[3px]">
              {items.map(name => (
                <button
                  key={name}
                  onClick={() => setActive(name)}
                  className={[
                    'flex h-[34px] items-center truncate rounded-[5px] px-3 text-left text-[13px] font-medium transition-colors',
                    active === name
                      ? 'bg-[var(--c-bg-sub)] text-[var(--c-text-primary)]'
                      : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]',
                  ].join(' ')}
                >
                  {name}
                </button>
              ))}
            </div>
          </div>
          <div className="border-t border-[var(--c-border-subtle)] px-3 py-3">
            <button className="flex h-8 w-full items-center justify-center gap-1.5 rounded-md text-[13px] text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]">
              <Plus size={14} />
              Add provider
            </button>
          </div>
        </div>
        {/* Right */}
        <div className="flex flex-1 items-center justify-center bg-[var(--c-bg-page)]">
          <span className="text-sm text-[var(--c-text-muted)]">{active} detail panel</span>
        </div>
      </div>
    </PreviewSection>
  )
}

// ---------------------------------------------------------------------------
// 7. ProvidersSettings modal field layout
// ---------------------------------------------------------------------------

function ModalFieldsPreview() {
  const [modalName, setModalName] = useState('')
  const [modalVendor, setModalVendor] = useState('openai_responses')
  const [modalKey, setModalKey] = useState('')
  const [modalBase, setModalBase] = useState('')
  return (
    <PreviewSection title="Modal Form Fields (ProvidersSettings AddProviderModal · grid-cols-2)">
      <div className={SECTION_CLS}>
        <div className="grid grid-cols-2 gap-x-4 gap-y-3">
          <div>
            <label className={FIELD_LABEL_CLS}>Provider name</label>
            <input className={FIELD_INPUT_CLS} style={FIELD_INPUT_STYLE} placeholder="My Provider" value={modalName} onChange={e => setModalName(e.target.value)} />
          </div>
          <div>
            <label className={FIELD_LABEL_CLS}>Vendor</label>
            <VendorDropdown
              value={modalVendor}
              options={[
                { key: 'openai_responses', label: 'OpenAI (Responses API)' },
                { key: 'anthropic_message', label: 'Anthropic' },
                { key: 'gemini', label: 'Gemini' },
              ]}
              onChange={setModalVendor}
            />
          </div>
          <div className="col-span-2">
            <label className={FIELD_LABEL_CLS}>API Key</label>
            <input type="password" className={FIELD_INPUT_CLS} style={FIELD_INPUT_STYLE} placeholder="sk-proj-…" value={modalKey} onChange={e => setModalKey(e.target.value)} />
          </div>
          <div className="col-span-2">
            <label className={FIELD_LABEL_CLS}>Base URL</label>
            <input className={FIELD_INPUT_CLS} style={FIELD_INPUT_STYLE} placeholder="https://api.example.com/v1" value={modalBase} onChange={e => setModalBase(e.target.value)} />
          </div>
        </div>
      </div>
    </PreviewSection>
  )
}

// ---------------------------------------------------------------------------
// 8. ConnectorsSettings section card
// ---------------------------------------------------------------------------

function SectionCardPreview() {
  return (
    <PreviewSection title="Section Card (ConnectorsSettings · SECTION_CLS rounded-xl border bg-menu p-5)">
      <div className={SECTION_CLS}>
        <div className="mb-3 text-sm font-semibold text-[var(--c-text-heading)]">Providers</div>
        <div className="flex flex-col gap-2">
          {[
            { name: 'sandbox / browser', badge: 'configured' as BadgeVariant },
            { name: 'web search', badge: 'missing' as BadgeVariant },
            { name: 'image understanding', badge: 'free' as BadgeVariant },
          ].map(({ name, badge }) => (
            <div
              key={name}
              className="flex items-center justify-between rounded-lg px-3 py-2"
              style={{ border: '0.5px solid var(--c-border-subtle)' }}
            >
              <span className="text-sm text-[var(--c-text-primary)]">{name}</span>
              <div className="flex items-center gap-2">
                <StatusBadge variant={badge} />
                <button className={BTN_ICON}><Settings size={14} /></button>
              </div>
            </div>
          ))}
        </div>
      </div>
    </PreviewSection>
  )
}

// ---------------------------------------------------------------------------
// 9. Status badges
// ---------------------------------------------------------------------------

function BadgesPreview() {
  return (
    <PreviewSection title="Status Badges">
      <div className={SECTION_CLS + ' flex flex-col gap-4'}>

        {/* SearchFetchSettings StatusBadge — mixed CSS vars + hardcoded */}
        <div>
          <span className={LABEL_CLS}>StatusBadge (SearchFetchSettings) — uses hardcoded bg-green-500/15 etc.</span>
          <div className="flex flex-wrap gap-2 mt-1">
            {(['free', 'configured', 'always', 'missing'] as BadgeVariant[]).map(v => (
              <StatusBadge key={v} variant={v} />
            ))}
          </div>
        </div>

        {/* ConnectorsSettings — same hardcoded colours */}
        <div>
          <span className={LABEL_CLS}>ConnectorsSettings status — same hardcoded pattern</span>
          <div className="flex flex-wrap gap-2 mt-1">
            <span className="inline-flex items-center gap-1 rounded-full bg-green-500/10 px-1.5 py-0.5 text-[10px] font-medium text-green-400">ready</span>
            <span className="inline-flex items-center gap-1 rounded-full bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-medium text-amber-400">missing</span>
            <span className="inline-flex items-center gap-1 rounded-full bg-rose-500/10 px-1.5 py-0.5 text-[10px] font-medium text-rose-400">error</span>
          </div>
        </div>

        {/* CSS-variable based */}
        <div>
          <span className={LABEL_CLS}>CSS-variable status — --c-status-ok/danger/warn (consistent)</span>
          <div className="flex flex-wrap gap-2 mt-1">
            <span className="inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium" style={{ background: 'var(--c-status-ok-bg)', color: 'var(--c-status-ok-text)' }}>ok</span>
            <span className="inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium" style={{ background: 'var(--c-status-danger-bg)', color: 'var(--c-status-danger-text)' }}>error</span>
            <span className="inline-flex items-center rounded-full px-1.5 py-0.5 text-[10px] font-medium" style={{ background: 'var(--c-status-warn-bg)', color: 'var(--c-status-warn-text)' }}>warn</span>
          </div>
        </div>

        {/* Inline badge — ProvidersSettings model list */}
        <div>
          <span className={LABEL_CLS}>Inline badge · rounded bg-deep px-1.5 py-0.5 text-[10px] (ProvidersSettings model list)</span>
          <div className="flex flex-wrap gap-2 mt-1">
            <span className="rounded bg-[var(--c-bg-deep)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-text-muted)]">embed</span>
            <span className="rounded bg-[var(--c-bg-deep)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-text-muted)]">Free</span>
            <span className="rounded bg-[var(--c-bg-deep)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-text-muted)]">Always</span>
          </div>
        </div>
      </div>
    </PreviewSection>
  )
}

// ---------------------------------------------------------------------------
// 10. Empty / loading / error states
// ---------------------------------------------------------------------------

function StatesPreview() {
  return (
    <PreviewSection title="Empty / Loading / Error States">
      <div className="flex flex-col gap-3">
        <div>
          <span className={LABEL_CLS}>Empty · flex flex-col items-center py-14/16 (used across multiple pages)</span>
          <div
            className="flex flex-col items-center justify-center rounded-xl py-10"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
          >
            <p className="text-sm text-[var(--c-text-muted)]">No items found.</p>
          </div>
        </div>

        <div>
          <span className={LABEL_CLS}>Loading · Loader2 animate-spin center (ProvidersSettings, ConnectorsSettings)</span>
          <div
            className="flex items-center justify-center rounded-xl py-10"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
          >
            <Loader2 size={18} className="animate-spin text-[var(--c-text-muted)]" />
          </div>
        </div>

        <div>
          <span className={LABEL_CLS}>Loading skeleton · h-6 w-12 animate-pulse rounded-full bg-deep (ChatSettings toggle)</span>
          <div className="h-6 w-12 animate-pulse rounded-full bg-[var(--c-bg-deep)]" />
        </div>

        <div>
          <span className={LABEL_CLS}>Error callout · --c-error-bg / border / text (used in multiple pages)</span>
          <div
            className="flex items-start gap-2 rounded-xl px-4 py-3 text-sm"
            style={{ background: 'var(--c-error-bg)', border: '0.5px solid var(--c-error-border)', color: 'var(--c-error-text)' }}
          >
            <Info size={14} className="mt-0.5 shrink-0" />
            Failed to load providers. Check your connection.
          </div>
        </div>

        <div>
          <span className={LABEL_CLS}>Inline error · text-xs text-red-400 / text-[--c-status-error-text] (inconsistent)</span>
          <p className="text-xs text-red-400">red-400 variant (ProvidersSettings sidebar)</p>
          <p className="text-xs" style={{ color: 'var(--c-status-error-text)' }}>--c-status-error-text variant (ChatSettings)</p>
        </div>
      </div>
    </PreviewSection>
  )
}

// ---------------------------------------------------------------------------
// 11. Color token swatches
// ---------------------------------------------------------------------------

type Swatch = { label: string; cssVar: string }

const TOKEN_GROUPS: { title: string; swatches: Swatch[] }[] = [
  {
    title: 'Backgrounds',
    swatches: [
      { label: 'page', cssVar: '--c-bg-page' }, { label: 'sidebar', cssVar: '--c-bg-sidebar' },
      { label: 'deep', cssVar: '--c-bg-deep' }, { label: 'deep2', cssVar: '--c-bg-deep2' },
      { label: 'sub', cssVar: '--c-bg-sub' }, { label: 'card', cssVar: '--c-bg-card' },
      { label: 'card-hover', cssVar: '--c-bg-card-hover' }, { label: 'input', cssVar: '--c-bg-input' },
      { label: 'menu', cssVar: '--c-bg-menu' }, { label: 'plus', cssVar: '--c-bg-plus' },
      { label: 'tag', cssVar: '--c-bg-tag' },
    ],
  },
  {
    title: 'Text',
    swatches: [
      { label: 'heading', cssVar: '--c-text-heading' }, { label: 'primary', cssVar: '--c-text-primary' },
      { label: 'secondary', cssVar: '--c-text-secondary' }, { label: 'tertiary', cssVar: '--c-text-tertiary' },
      { label: 'muted', cssVar: '--c-text-muted' }, { label: 'icon', cssVar: '--c-text-icon' },
      { label: 'icon2', cssVar: '--c-text-icon2' }, { label: 'placeholder', cssVar: '--c-placeholder' },
    ],
  },
  {
    title: 'Borders',
    swatches: [
      { label: 'border', cssVar: '--c-border' }, { label: 'subtle', cssVar: '--c-border-subtle' },
      { label: 'mid', cssVar: '--c-border-mid' }, { label: 'auth', cssVar: '--c-border-auth' },
      { label: 'console', cssVar: '--c-border-console' },
    ],
  },
  {
    title: 'Accent',
    swatches: [
      { label: 'accent', cssVar: '--c-accent' }, { label: 'accent-fg', cssVar: '--c-accent-fg' },
      { label: 'btn-bg', cssVar: '--c-btn-bg' }, { label: 'btn-text', cssVar: '--c-btn-text' },
      { label: 'send', cssVar: '--c-accent-send' }, { label: 'send-hover', cssVar: '--c-accent-send-hover' },
      { label: 'send-text', cssVar: '--c-accent-send-text' },
    ],
  },
  {
    title: 'Status',
    swatches: [
      { label: 'ok-bg', cssVar: '--c-status-ok-bg' }, { label: 'ok-text', cssVar: '--c-status-ok-text' },
      { label: 'error-bg', cssVar: '--c-error-bg' }, { label: 'error-border', cssVar: '--c-error-border' },
      { label: 'error-text', cssVar: '--c-error-text' },
      { label: 'danger-bg', cssVar: '--c-status-danger-bg' }, { label: 'danger-text', cssVar: '--c-status-danger-text' },
      { label: 'warn-bg', cssVar: '--c-status-warn-bg' }, { label: 'warn-text', cssVar: '--c-status-warn-text' },
    ],
  },
  {
    title: 'Code & Markdown',
    swatches: [
      { label: 'code-block-bg', cssVar: '--c-md-code-block-bg' }, { label: 'inline-code-bg', cssVar: '--c-md-inline-code-bg' },
      { label: 'inline-code-color', cssVar: '--c-md-inline-code-color' }, { label: 'table-head-bg', cssVar: '--c-md-table-head-bg' },
      { label: 'code-panel-bg', cssVar: '--c-code-panel-bg' }, { label: 'code-output-bg', cssVar: '--c-code-panel-output-bg' },
    ],
  },
]

function SwatchCell({ label, cssVar }: Swatch) {
  return (
    <div className="flex flex-col gap-1" title={cssVar}>
      <div className="h-9 w-full rounded-lg" style={{ background: `var(${cssVar})`, border: '0.5px solid var(--c-border-subtle)' }} />
      <div className="truncate text-[10px] leading-tight text-[var(--c-text-muted)]">{label}</div>
      <div className="truncate font-mono text-[9px] leading-tight text-[var(--c-text-muted)] opacity-60">{cssVar}</div>
    </div>
  )
}

function TokensPreview() {
  return (
    <>
      {TOKEN_GROUPS.map(g => (
        <PreviewSection key={g.title} title={`Tokens · ${g.title}`}>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(72px,1fr))] gap-3">
            {g.swatches.map(s => <SwatchCell key={s.cssVar} {...s} />)}
          </div>
        </PreviewSection>
      ))}
    </>
  )
}

// ---------------------------------------------------------------------------
// Root
// ---------------------------------------------------------------------------

export function DesignTokensSettings() {
  return (
    <div className="flex flex-col gap-8">
      <SettingsSectionHeader
        title="Design Tokens"
        description="Verbatim UI components from settings pages. Labels show source file and class constants. Items marked 'inconsistent' diverge across files."
      />

      <TypographyPreview />
      <ButtonsPreview />
      <InputsPreview />
      <TogglesPreview />
      <ProviderCardPreview />
      <ListNavPreview />
      <ModalFieldsPreview />
      <SectionCardPreview />
      <BadgesPreview />
      <StatesPreview />
      <TokensPreview />
    </div>
  )
}
