import { useEffect, useRef, useState } from 'react'
import { Check, ChevronDown, Eye, EyeOff, Link2, Loader2, Plus, X } from 'lucide-react'
import type { ChannelBindingResponse, ChannelResponse, LlmProvider, Persona } from '../../api'
import { DEFAULT_PERSONA_KEY } from '../../storage'
import { useLocale } from '../../contexts/LocaleContext'
import { secondaryButtonBorderStyle, secondaryButtonSmCls } from '../buttonStyles'

export type ModelOption = { value: string; label: string }

export const inputCls =
  'w-full rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-2 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)] transition-colors'

export const secondaryButtonCls =
  'button-secondary inline-flex items-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium text-[var(--c-text-secondary)] transition-colors disabled:opacity-50'

export const primaryButtonCls =
  'inline-flex items-center gap-1.5 rounded-lg px-4 py-2 text-sm font-medium transition-colors hover:opacity-90 disabled:opacity-50'

export function ModelDropdown({
  value,
  options,
  placeholder,
  disabled,
  onChange,
}: {
  value: string
  options: ModelOption[]
  placeholder: string
  disabled: boolean
  onChange: (v: string) => void
}) {
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const btnRef = useRef<HTMLButtonElement>(null)

  const currentLabel = options.find((option) => option.value === value)?.label ?? (value || placeholder)

  useEffect(() => {
    if (!open) return
    const handleMouseDown = (event: MouseEvent) => {
      if (menuRef.current?.contains(event.target as Node) || btnRef.current?.contains(event.target as Node)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', handleMouseDown)
    return () => document.removeEventListener('mousedown', handleMouseDown)
  }, [open])

  return (
    <div className="relative">
      <button
        ref={btnRef}
        type="button"
        disabled={disabled}
        onClick={() => setOpen((current) => !current)}
        className="flex h-9 w-full items-center justify-between rounded-lg px-3 text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)', color: 'var(--c-text-secondary)' }}
      >
        <span className="truncate">{currentLabel}</span>
        <ChevronDown size={13} className="ml-2 shrink-0" />
      </button>

      {open && (
        <div
          ref={menuRef}
          className="dropdown-menu absolute left-0 top-[calc(100%+4px)] z-50"
          style={{
            border: '0.5px solid var(--c-border-subtle)',
            borderRadius: '10px',
            padding: '4px',
            background: 'var(--c-bg-menu)',
            width: '100%',
            boxShadow: 'var(--c-dropdown-shadow)',
            maxHeight: '220px',
            overflowY: 'auto',
          }}
        >
          <button
            type="button"
            onClick={() => {
              onChange('')
              setOpen(false)
            }}
            className="flex w-full items-center px-3 py-2 text-sm transition-colors bg-[var(--c-bg-menu)] hover:bg-[var(--c-bg-deep)]"
            style={{ borderRadius: '8px', fontWeight: !value ? 600 : 400, color: !value ? 'var(--c-text-heading)' : 'var(--c-text-secondary)' }}
          >
            {placeholder}
          </button>
          {options.map(({ value: optionValue, label }) => (
            <button
              key={optionValue}
              type="button"
              onClick={() => {
                onChange(optionValue)
                setOpen(false)
              }}
              className="flex w-full items-center px-3 py-2 text-sm transition-colors bg-[var(--c-bg-menu)] hover:bg-[var(--c-bg-deep)]"
              style={{ borderRadius: '8px', fontWeight: value === optionValue ? 600 : 400, color: value === optionValue ? 'var(--c-text-heading)' : 'var(--c-text-secondary)' }}
            >
              {label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

export function StatusBadge({
  active,
  label,
}: {
  active: boolean
  label: string
}) {
  return (
    <span
      className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium"
      style={{
        background: active ? 'var(--c-status-success-bg, rgba(34,197,94,0.1))' : 'var(--c-bg-deep)',
        color: active ? 'var(--c-status-success, #22c55e)' : 'var(--c-text-muted)',
      }}
    >
      <span
        className="inline-block h-1.5 w-1.5 rounded-full"
        style={{ background: active ? 'currentColor' : 'var(--c-text-muted)' }}
      />
      {label}
    </span>
  )
}

export function readStringArrayConfig(channel: ChannelResponse | null, key: string): string[] {
  const raw = channel?.config_json?.[key]
  if (!Array.isArray(raw)) return []

  const seen = new Set<string>()
  const values: string[] = []
  for (const item of raw) {
    if (typeof item !== 'string') continue
    const cleaned = item.trim()
    if (!cleaned || seen.has(cleaned)) continue
    seen.add(cleaned)
    values.push(cleaned)
  }

  return values
}

export function parseListValues(input: string): string[] {
  return input
    .split(/[\n,\s]+/)
    .map((item) => item.trim())
    .filter(Boolean)
}

export function mergeListValues(existing: string[], pendingInput: string): string[] {
  const seen = new Set<string>()
  const merged: string[] = []

  for (const item of [...existing, ...parseListValues(pendingInput)]) {
    if (!item || seen.has(item)) continue
    seen.add(item)
    merged.push(item)
  }

  return merged
}

export function sameItems(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((item, index) => item === b[index])
}

export function defaultPersonaID(personas: Persona[]): string {
  const preferred = personas.find((persona) => persona.persona_key === DEFAULT_PERSONA_KEY)
  return preferred?.id ?? personas[0]?.id ?? ''
}

export function resolvePersonaID(personas: Persona[], storedPersonaID?: string | null): string {
  const cleaned = storedPersonaID?.trim()
  if (cleaned) return cleaned
  return defaultPersonaID(personas)
}

export function buildModelOptions(providers: LlmProvider[]): ModelOption[] {
  return providers.flatMap((provider) =>
    provider.models
      .filter((model) => model.show_in_picker)
      .map((model) => ({
        value: `${provider.name}^${model.model}`,
        label: `${provider.name} / ${model.model}`,
      })),
  )
}

export function ListField({
  label,
  values,
  inputValue,
  placeholder,
  addLabel,
  onInputChange,
  onAdd,
  onRemove,
}: {
  label: string
  values: string[]
  inputValue: string
  placeholder: string
  addLabel: string
  onInputChange: (value: string) => void
  onAdd: () => void
  onRemove: (value: string) => void
}) {
  return (
    <div className="md:col-span-2">
      <label className="mb-1.5 block text-xs font-medium text-[var(--c-text-secondary)]">
        {label}
      </label>
      {values.length > 0 && (
        <div className="mb-2 flex flex-wrap gap-2">
          {values.map((item) => (
            <span
              key={item}
              className="inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs text-[var(--c-text-primary)]"
              style={{ background: 'var(--c-bg-deep)' }}
            >
              {item}
              <button
                type="button"
                onClick={() => onRemove(item)}
                className="text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-primary)]"
                aria-label={label}
              >
                <X size={12} />
              </button>
            </span>
          ))}
        </div>
      )}
      <div className="flex gap-2">
        <input
          type="text"
          value={inputValue}
          onChange={(event) => onInputChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              onAdd()
            }
          }}
          placeholder={placeholder}
          className={inputCls}
        />
        <button
          type="button"
          onClick={onAdd}
          className={`${secondaryButtonSmCls} shrink-0`}
          style={secondaryButtonBorderStyle}
        >
          <Plus size={14} />
          {addLabel}
        </button>
      </div>
    </div>
  )
}

function BindingRoleBadge({
  active,
  label,
}: {
  active: boolean
  label: string
}) {
  return (
    <span
      className="rounded-md px-2 py-0.5 text-[11px] font-medium"
      style={{
        border: '0.5px solid var(--c-border-subtle)',
        background: active ? 'var(--c-status-success-bg, rgba(34,197,94,0.1))' : 'var(--c-bg-deep)',
        color: active ? 'var(--c-status-success, #22c55e)' : 'var(--c-text-secondary)',
      }}
    >
      {label}
    </span>
  )
}

function BindingHeartbeatEditor({
  binding,
  modelOptions,
  enabledLabel,
  intervalLabel,
  modelLabel,
  saveLabel,
  savingLabel,
  ownerLabel,
  adminLabel,
  setOwnerLabel,
  unbindLabel,
  onSaveHeartbeat,
  onMakeOwner,
  onUnbind,
  onOwnerUnbindAttempt,
}: {
  binding: ChannelBindingResponse
  modelOptions: ModelOption[]
  enabledLabel: string
  intervalLabel: string
  modelLabel: string
  saveLabel: string
  savingLabel: string
  ownerLabel: string
  adminLabel: string
  setOwnerLabel: string
  unbindLabel: string
  onSaveHeartbeat: (binding: ChannelBindingResponse, next: { enabled: boolean; interval: number; model: string }) => Promise<void>
  onMakeOwner: (binding: ChannelBindingResponse) => Promise<void>
  onUnbind: (binding: ChannelBindingResponse) => Promise<void>
  onOwnerUnbindAttempt: () => void
}) {
  const [promotingOwner, setPromotingOwner] = useState(false)
  const [hbEnabled, setHbEnabled] = useState(binding.heartbeat_enabled)
  const [hbInterval, setHbInterval] = useState(String(binding.heartbeat_interval_minutes || 30))
  const [hbModel, setHbModel] = useState(binding.heartbeat_model ?? '')
  const [hbSaving, setHbSaving] = useState(false)

  const hbDirty =
    hbEnabled !== binding.heartbeat_enabled ||
    Number(hbInterval) !== (binding.heartbeat_interval_minutes || 30) ||
    hbModel !== (binding.heartbeat_model ?? '')

  useEffect(() => {
    setHbEnabled(binding.heartbeat_enabled)
    setHbInterval(String(binding.heartbeat_interval_minutes || 30))
    setHbModel(binding.heartbeat_model ?? '')
  }, [binding.heartbeat_enabled, binding.heartbeat_interval_minutes, binding.heartbeat_model])

  const handleSave = async () => {
    setHbSaving(true)
    try {
      await onSaveHeartbeat(binding, {
        enabled: hbEnabled,
        interval: Math.max(1, Number(hbInterval) || 30),
        model: hbModel,
      })
    } finally {
      setHbSaving(false)
    }
  }

  return (
    <div
      data-binding-id={binding.binding_id}
      className="rounded-xl px-4 py-4"
      style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
    >
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <div className="truncate text-sm font-medium text-[var(--c-text-heading)]">
              {binding.display_name || binding.platform_subject_id}
            </div>
            <BindingRoleBadge active={binding.is_owner} label={binding.is_owner ? ownerLabel : adminLabel} />
          </div>
          <div className="mt-1 truncate text-xs text-[var(--c-text-muted)]">
            {binding.platform_subject_id}
          </div>
        </div>

        <div className="flex shrink-0 flex-wrap items-center gap-2">
          {!binding.is_owner && (
            <button
              type="button"
              disabled={promotingOwner}
              aria-label={`${setOwnerLabel} ${binding.display_name || binding.platform_subject_id}`}
              onClick={async () => {
                setPromotingOwner(true)
                try {
                  await onMakeOwner(binding)
                } finally {
                  setPromotingOwner(false)
                }
              }}
              className="rounded-md px-2.5 py-1 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
            >
              {setOwnerLabel}
            </button>
          )}
          <button
            type="button"
            aria-label={`${unbindLabel} ${binding.display_name || binding.platform_subject_id}`}
            onClick={() => {
              if (binding.is_owner) {
                onOwnerUnbindAttempt()
                return
              }
              void onUnbind(binding)
            }}
            className="rounded-md px-2.5 py-1 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
          >
            {unbindLabel}
          </button>
        </div>
      </div>

      <div
        className="mt-3 rounded-lg px-3 py-3"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-sub, var(--c-bg-deep))' }}
      >
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:gap-3">
          <label className="flex items-center gap-2 text-xs font-medium text-[var(--c-text-secondary)]">
            <input
              type="checkbox"
              checked={hbEnabled}
              onChange={(e) => setHbEnabled(e.target.checked)}
              className="rounded"
            />
            {enabledLabel}
          </label>

          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <span className="text-[11px] text-[var(--c-text-muted)]">{intervalLabel}</span>
            <input
              type="number"
              min={1}
              value={hbInterval}
              onChange={(e) => setHbInterval(e.target.value)}
              className="w-full rounded-md border-0 bg-[var(--c-bg-input)] px-2.5 py-1.5 text-xs text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:ring-1 focus:ring-[var(--c-border-mid)]"
            />
          </div>

          <div className="flex min-w-0 flex-[2] flex-col gap-1">
            <span className="text-[11px] text-[var(--c-text-muted)]">{modelLabel}</span>
            <ModelDropdown
              value={hbModel}
              options={modelOptions}
              placeholder="--"
              disabled={hbSaving}
              onChange={setHbModel}
            />
          </div>

          <button
            type="button"
            disabled={hbSaving || !hbDirty}
            onClick={() => void handleSave()}
            className="shrink-0 rounded-md px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-50"
            style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
          >
            {hbSaving ? savingLabel : saveLabel}
          </button>
        </div>
      </div>
    </div>
  )
}

export function BindingsCard({
  title,
  bindings,
  bindCode,
  generating,
  generateLabel,
  regenerateLabel,
  emptyLabel,
  ownerLabel,
  adminLabel,
  setOwnerLabel,
  unbindLabel,
  heartbeatEnabledLabel,
  heartbeatIntervalLabel,
  heartbeatModelLabel,
  heartbeatSaveLabel,
  heartbeatSavingLabel,
  modelOptions,
  onGenerate,
  onUnbind,
  onMakeOwner,
  onSaveHeartbeat,
  onOwnerUnbindAttempt,
}: {
  title: string
  bindings: ChannelBindingResponse[]
  bindCode: string | null
  generating: boolean
  generateLabel: string
  regenerateLabel: string
  emptyLabel: string
  ownerLabel: string
  adminLabel: string
  setOwnerLabel: string
  unbindLabel: string
  heartbeatEnabledLabel: string
  heartbeatIntervalLabel: string
  heartbeatModelLabel: string
  heartbeatSaveLabel: string
  heartbeatSavingLabel: string
  modelOptions: ModelOption[]
  onGenerate: () => void
  onUnbind: (binding: ChannelBindingResponse) => Promise<void>
  onMakeOwner: (binding: ChannelBindingResponse) => Promise<void>
  onSaveHeartbeat: (binding: ChannelBindingResponse, next: { enabled: boolean; interval: number; model: string }) => Promise<void>
  onOwnerUnbindAttempt: () => void
}) {
  const { t } = useLocale()
  return (
    <div
      className="rounded-2xl p-5"
      style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
    >
      <div className="flex flex-col gap-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <div className="text-sm font-medium text-[var(--c-text-heading)]">{title}</div>
            {bindCode && (
              <div className="mt-2">
                <code className="rounded-md bg-[var(--c-bg-deep)] px-2 py-1 font-mono text-sm text-[var(--c-text-heading)] select-all">
                  /bind {bindCode}
                </code>
                <p className="mt-1.5 text-xs text-[var(--c-text-muted)]">{t.channels.bindCodeInGroupHint}</p>
              </div>
            )}
          </div>

          <button
            type="button"
            onClick={onGenerate}
            disabled={generating}
            className={`${secondaryButtonSmCls} shrink-0`}
            style={secondaryButtonBorderStyle}
          >
            {generating ? <Loader2 size={14} className="animate-spin" /> : <Link2 size={14} />}
            {generating ? generateLabel : bindCode ? regenerateLabel : generateLabel}
          </button>
        </div>

        {bindings.length === 0 ? (
          <p className="text-sm text-[var(--c-text-muted)]">{emptyLabel}</p>
        ) : (
          <div className="flex flex-col gap-3">
            {bindings.map((binding) => (
              <BindingHeartbeatEditor
                key={binding.binding_id}
                binding={binding}
                modelOptions={modelOptions}
                enabledLabel={heartbeatEnabledLabel}
                intervalLabel={heartbeatIntervalLabel}
                modelLabel={heartbeatModelLabel}
                saveLabel={heartbeatSaveLabel}
                savingLabel={heartbeatSavingLabel}
                ownerLabel={ownerLabel}
                adminLabel={adminLabel}
                setOwnerLabel={setOwnerLabel}
                unbindLabel={unbindLabel}
                onSaveHeartbeat={onSaveHeartbeat}
                onMakeOwner={onMakeOwner}
                onUnbind={onUnbind}
                onOwnerUnbindAttempt={onOwnerUnbindAttempt}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

export function SaveActions({
  saving,
  saved,
  dirty,
  canSave,
  canVerify,
  verifying,
  saveLabel,
  savingLabel,
  verifyLabel,
  verifyingLabel,
  savedLabel,
  onSave,
  onVerify,
}: {
  saving: boolean
  saved: boolean
  dirty: boolean
  canSave: boolean
  canVerify: boolean
  verifying: boolean
  saveLabel: string
  savingLabel: string
  verifyLabel: string
  verifyingLabel: string
  savedLabel: string
  onSave: () => void
  onVerify: () => void
}) {
  return (
    <div className="flex items-center gap-3 border-t border-[var(--c-border-subtle)] pt-4">
      <button
        type="button"
        onClick={onSave}
        disabled={saving || !canSave}
        className={primaryButtonCls}
        style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
      >
        {saving && <Loader2 size={13} className="animate-spin" />}
        {!saving && saved && <Check size={13} />}
        {saving ? savingLabel : saveLabel}
      </button>
      {canVerify && (
        <button
          type="button"
          onClick={onVerify}
          disabled={verifying || saving}
          className={secondaryButtonCls}
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          {verifying && <Loader2 size={13} className="animate-spin" />}
          {verifying ? verifyingLabel : verifyLabel}
        </button>
      )}
      {saved && !dirty && (
        <span
          className="inline-flex items-center gap-1 text-xs"
          style={{ color: 'var(--c-status-success, #22c55e)' }}
        >
          <Check size={11} />
          {savedLabel}
        </span>
      )}
    </div>
  )
}

export function TokenField({
  label,
  value,
  placeholder,
  onChange,
}: {
  label: string
  value: string
  placeholder: string
  onChange: (value: string) => void
}) {
  const [showToken, setShowToken] = useState(false)

  return (
    <div className="md:col-span-2">
      <label className="mb-1.5 block text-xs font-medium text-[var(--c-text-secondary)]">
        {label}
      </label>
      <div className="relative">
        <input
          type={showToken ? 'text' : 'password'}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          placeholder={placeholder}
          className={inputCls}
        />
        <button
          type="button"
          onClick={() => setShowToken((current) => !current)}
          className="absolute right-3 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)]"
        >
          {showToken ? <EyeOff size={14} /> : <Eye size={14} />}
        </button>
      </div>
    </div>
  )
}
