import { useEffect, useRef, useState } from 'react'
import { ChevronDown, Check } from 'lucide-react'
import {
  listSpawnProfiles,
  listLlmProviders,
  setSpawnProfile,
  deleteSpawnProfile,
} from '../../api'
import type { SpawnProfile, LlmProvider } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { isLocalMode } from '@arkloop/shared/desktop'

type Props = {
  accessToken: string
}

type Option = { value: string; label: string }

function ModelDropdown({
  value,
  options,
  placeholder,
  disabled,
  onChange,
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

  const currentLabel = value === ''
    ? placeholder
    : (options.find(o => o.value === value)?.label ?? value)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node) ||
        btnRef.current?.contains(e.target as Node)
      ) return
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
          {/* platform default option */}
          <button
            type="button"
            onClick={() => { onChange(''); setOpen(false) }}
            className="flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
            style={{
              color: value === '' ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
              fontWeight: value === '' ? 500 : 400,
            }}
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
              style={{
                color: value === o.value ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
                fontWeight: value === o.value ? 500 : 400,
              }}
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

const PROFILE_NAMES = ['explore', 'task', 'strong'] as const

export function RoutingSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const a = t.agentSettings
  const [profiles, setProfiles] = useState<SpawnProfile[]>([])
  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [saving, setSaving] = useState<string | null>(null)
  const spawnProfilePlaceholder = isLocalMode()
    ? a.spawnProfileFollowCurrentChat
    : a.spawnProfilePlatformDefault

  useEffect(() => {
    listSpawnProfiles(accessToken).then(setProfiles).catch(() => {})
    listLlmProviders(accessToken).then(setProviders).catch(() => {})
  }, [accessToken])

  const modelOptions: Option[] = providers
    .flatMap(p => p.models.filter(m => m.show_in_picker).map(m => ({
      value: `${p.name}^${m.model}`,
      label: `${p.name} / ${m.model}`,
    })))

  const handleChange = async (name: string, value: string) => {
    setSaving(name)
    try {
      if (value === '') {
        await deleteSpawnProfile(accessToken, name)
      } else {
        await setSpawnProfile(accessToken, name, value)
      }
      const updated = await listSpawnProfiles(accessToken)
      setProfiles(updated)
    } finally {
      setSaving(null)
    }
  }

  const profileLabels: Record<string, string> = {
    explore: a.spawnProfileExplore,
    task: a.spawnProfileTask,
    strong: a.spawnProfileStrong,
  }

  return (
    <div className="flex flex-col gap-6">
      <div
        className="flex flex-col gap-3 rounded-lg p-3"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        <span className="text-sm font-medium text-[var(--c-text-heading)]">
          {a.spawnProfileTitle}
        </span>
        <div className="flex flex-col gap-2">
          {PROFILE_NAMES.map(name => {
            const profile = profiles.find(p => p.profile === name)
            const currentValue = profile?.has_override ? profile.resolved_model : ''
            return (
              <div key={name} className="flex items-center gap-3">
                <span className="w-20 shrink-0 text-sm text-[var(--c-text-secondary)]">
                  {profileLabels[name]}
                </span>
                <ModelDropdown
                  value={currentValue}
                  options={modelOptions}
                  placeholder={spawnProfilePlaceholder}
                  disabled={saving === name}
                  onChange={v => handleChange(name, v)}
                />
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}
