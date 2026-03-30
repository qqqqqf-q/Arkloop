import { useEffect, useState } from 'react'
import {
  listSpawnProfiles,
  listLlmProviders,
  setSpawnProfile,
  deleteSpawnProfile,
} from '../../api'
import type { SpawnProfile, LlmProvider } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { isLocalMode } from '@arkloop/shared/desktop'
import { SettingsModelDropdown } from './SettingsModelDropdown'

type Props = {
  accessToken: string
}

const PROFILE_NAMES = ['explore', 'task', 'strong'] as const

export function RoutingSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const a = t.agentSettings
  const [profiles, setProfiles] = useState<SpawnProfile[]>([])
  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [saving, setSaving] = useState<string | null>(null)
  const placeholder = isLocalMode()
    ? a.spawnProfileFollowCurrentChat
    : a.spawnProfilePlatformDefault

  useEffect(() => {
    listSpawnProfiles(accessToken).then(setProfiles).catch(() => {})
    listLlmProviders(accessToken).then(setProviders).catch(() => {})
  }, [accessToken])

  const modelOptions = providers
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

  const profileMeta: Record<string, { label: string; desc: string }> = {
    explore: { label: a.spawnProfileExplore, desc: a.spawnProfileExploreDesc },
    task:    { label: a.spawnProfileTask,    desc: a.spawnProfileTaskDesc    },
    strong:  { label: a.spawnProfileStrong,  desc: a.spawnProfileStrongDesc  },
  }

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="text-sm font-medium text-[var(--c-text-heading)]">
          {a.spawnProfileTitle}
        </h3>
        <p className="mt-1 text-xs text-[var(--c-text-muted)]">
          {a.spawnProfileSubtitle}
        </p>
      </div>

      {PROFILE_NAMES.map(name => {
        const profile = profiles.find(p => p.profile === name)
        const currentValue = profile?.has_override ? profile.resolved_model : ''
        const meta = profileMeta[name]
        return (
          <div
            key={name}
            className="flex items-center justify-between gap-4 rounded-xl px-5 py-4"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-menu)' }}
          >
            <div className="min-w-0 shrink-0">
              <span className="text-sm font-medium text-[var(--c-text-primary)]">
                {meta.label}
              </span>
              <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">
                {meta.desc}
              </p>
            </div>
            <div className="min-w-0 flex-1" style={{ maxWidth: 320 }}>
              <SettingsModelDropdown
                value={currentValue}
                options={modelOptions}
                placeholder={placeholder}
                disabled={saving === name}
                onChange={v => handleChange(name, v)}
              />
            </div>
          </div>
        )
      })}
    </div>
  )
}
