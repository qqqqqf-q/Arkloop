import { useState } from 'react'
import type { ReactNode } from 'react'
import { Check, Eye, EyeOff, Link2, Loader2, Plus, X } from 'lucide-react'
import { ConfirmDialog } from '@arkloop/shared'
import type { ChannelBindingResponse, ChannelResponse, LlmProvider, Persona } from '../../api'
import { DEFAULT_PERSONA_KEY } from '../../storage'
import { useLocale } from '../../contexts/LocaleContext'
import { secondaryButtonBorderStyle, secondaryButtonSmCls } from '../buttonStyles'
import { settingsInputCls } from './_SettingsInput'
import { SettingsSelect } from './_SettingsSelect'

export type ModelOption = { value: string; label: string }

export const inputCls =
  settingsInputCls('md')

export const secondaryButtonCls =
  'button-secondary inline-flex h-[32px] items-center justify-center gap-1.5 rounded-[6.5px] bg-[var(--c-bg-input)] px-3.5 text-sm font-[450] text-[color-mix(in_srgb,var(--c-text-secondary)_72%,var(--c-text-primary)_28%)] [background-clip:padding-box] transition-colors duration-[180ms] hover:border-transparent hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)] disabled:cursor-not-allowed disabled:opacity-40'

export const primaryButtonCls =
  'inline-flex h-[32px] items-center justify-center gap-1.5 rounded-[6.5px] bg-[var(--c-btn-bg)] px-3.5 text-sm font-[450] text-[var(--c-btn-text)] transition-[box-shadow] duration-150 hover:[box-shadow:inset_0_0_0_999px_rgba(255,255,255,0.07),0_0_0_0.2px_var(--c-btn-bg)] active:[box-shadow:inset_0_0_0_999px_rgba(0,0,0,0.04)] disabled:cursor-not-allowed disabled:opacity-40'

export const channelRowsCls =
  "flex flex-col [&>*]:relative [&>*]:grid [&>*]:items-center [&>*]:gap-3 [&>*]:px-5 [&>*]:py-4 [&>*]:sm:grid-cols-[minmax(0,1fr)_minmax(260px,390px)] [&>*]:sm:gap-6 [&>*+*]:before:absolute [&>*+*]:before:left-5 [&>*+*]:before:right-5 [&>*+*]:before:top-0 [&>*+*]:before:h-px [&>*+*]:before:bg-[var(--c-border-subtle)] [&>*+*]:before:content-[''] [&_label]:mb-0 [&_label]:text-[13px] [&_label]:font-medium [&_label]:text-[var(--c-text-primary)]"

export function ChannelDetailRow({
  label,
  children,
}: {
  label: string
  children: ReactNode
}) {
  return (
    <div>
      <div className="min-w-0 text-[13px] font-medium text-[var(--c-text-primary)]">{label}</div>
      <div className="min-w-0 sm:justify-self-end sm:w-full">{children}</div>
    </div>
  )
}

export function ModelDropdown({
  value,
  options,
  placeholder,
  disabled,
  onChange,
  showEmpty = true,
}: {
  value: string
  options: ModelOption[]
  placeholder?: string
  disabled?: boolean
  onChange: (v: string) => void
  showEmpty?: boolean
}) {
  const selectOptions = showEmpty && placeholder
    ? [{ value: '', label: placeholder }, ...options]
    : options

  return (
    <SettingsSelect
      value={value}
      options={selectOptions}
      placeholder={placeholder}
      disabled={disabled}
      onChange={onChange}
      triggerClassName="h-9"
    />
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
      className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium"
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
      <label className="mb-1.5 block text-[13px] font-medium text-[var(--c-text-primary)]">
        {label}
      </label>
      {values.length > 0 && (
        <div className="mb-2 flex flex-wrap gap-2">
          {values.map((item) => (
            <span
              key={item}
              className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-[var(--c-text-primary)]"
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
      className="rounded-md px-1.5 py-0.5 text-[10px] font-medium"
      style={{
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
  ownerLabel,
  adminLabel,
  setOwnerLabel,
  unbindLabel,
  unbindConfirmLabel,
  cancelLabel,
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
  unbindConfirmLabel: string
  cancelLabel: string
  onSaveHeartbeat: (binding: ChannelBindingResponse, next: { enabled: boolean; interval: number; model: string }) => Promise<void>
  onMakeOwner: (binding: ChannelBindingResponse) => Promise<void>
  onUnbind: (binding: ChannelBindingResponse) => Promise<void>
  onOwnerUnbindAttempt: () => void
}) {
  const [promotingOwner, setPromotingOwner] = useState(false)
  const [confirmUnbind, setConfirmUnbind] = useState(false)
  const [unbinding, setUnbinding] = useState(false)

  const handleConfirmUnbind = async () => {
    setUnbinding(true)
    try {
      await onUnbind(binding)
      setConfirmUnbind(false)
    } finally {
      setUnbinding(false)
    }
  }

  return (
    <>
      <div
        data-binding-id={binding.binding_id}
        className="relative px-5 py-4 [&+&]:before:absolute [&+&]:before:left-5 [&+&]:before:right-5 [&+&]:before:top-0 [&+&]:before:h-px [&+&]:before:bg-[var(--c-border-subtle)] [&+&]:before:content-['']"
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
                setConfirmUnbind(true)
              }}
              className="rounded-md px-2.5 py-1 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)]"
            >
              {unbindLabel}
            </button>
          </div>
        </div>
      </div>
      <ConfirmDialog
        open={confirmUnbind}
        title={unbindLabel}
        message={unbindConfirmLabel}
        confirmLabel={unbindLabel}
        cancelLabel={cancelLabel}
        loading={unbinding}
        onClose={() => setConfirmUnbind(false)}
        onConfirm={() => void handleConfirmUnbind()}
      />
    </>
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
      className="overflow-hidden rounded-xl"
      style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
    >
      <div className="flex flex-col">
        <div className="flex items-center justify-between gap-3 px-5 py-4">
          <div>
            <div className="text-[13px] font-medium text-[var(--c-text-primary)]">{title}</div>
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
          <p className="px-5 pb-4 text-sm text-[var(--c-text-muted)]">{emptyLabel}</p>
        ) : (
          <div>
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
                unbindConfirmLabel={t.channels.unbindConfirm}
                cancelLabel={t.channels.cancel}
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
    <div className="flex items-center gap-3 px-1 pt-1">
      <button
        type="button"
        onClick={onSave}
        disabled={saving || !canSave}
        className={primaryButtonCls}
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
          style={secondaryButtonBorderStyle}
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
  label?: string
  value: string
  placeholder: string
  onChange: (value: string) => void
}) {
  const [showToken, setShowToken] = useState(false)

  return (
    <div className="md:col-span-2">
      {label && (
        <label className="mb-1.5 block text-[13px] font-medium text-[var(--c-text-primary)]">
          {label}
        </label>
      )}
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
