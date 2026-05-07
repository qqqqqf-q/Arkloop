import { useEffect, useState, type ReactNode } from 'react'
import {
  listSpawnProfiles,
  listLlmProviders,
  setSpawnProfile,
  deleteSpawnProfile,
} from '../../api'
import type { SpawnProfile, LlmProvider } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { isLocalMode } from '@arkloop/shared/desktop'
import { getAvailableCatalogFromAdvancedJson } from '@arkloop/shared/llm/available-catalog-advanced-json'
import { SettingsModelDropdown } from './SettingsModelDropdown'
import { ToolModelSettingControl } from './ToolModelSettingControl'

type Props = {
  accessToken: string
}

const PROFILE_NAMES = ['explore', 'task', 'strong'] as const

function RoutingSection({
  title,
  description,
  children,
}: {
  title: string
  description?: string
  children: ReactNode
}) {
  return (
    <section className="flex flex-col gap-2.5">
      <div>
        <h3 className="text-sm font-semibold text-[var(--c-text-heading)]">{title}</h3>
        {description && (
          <p className="mt-1 text-xs leading-5 text-[var(--c-text-muted)]">{description}</p>
        )}
      </div>
      {children}
    </section>
  )
}

function RoutingCard({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-hidden rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]">
      {children}
    </div>
  )
}

function RoutingRow({
  title,
  description,
  control,
}: {
  title: string
  description?: ReactNode
  control: ReactNode
}) {
  return (
    <div className="grid gap-3 px-5 py-4 first:border-t-0 sm:grid-cols-[minmax(0,1fr)_minmax(220px,320px)] sm:items-center sm:gap-6 [&+&]:border-t [&+&]:border-[var(--c-border-subtle)]">
      <div className="min-w-0">
        <div className="text-sm font-medium text-[var(--c-text-primary)]">{title}</div>
        {description && (
          <div className="mt-1 text-xs leading-5 text-[var(--c-text-tertiary)]">{description}</div>
        )}
      </div>
      <div className="min-w-0 sm:justify-self-end">{control}</div>
    </div>
  )
}

export function RoutingSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const a = t.agentSettings
  const ds = t.desktopSettings
  const [profiles, setProfiles] = useState<SpawnProfile[]>([])
  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [saving, setSaving] = useState<string | null>(null)
  const subAgentPlaceholder = isLocalMode()
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
  const imageModelOptions = providers
    .flatMap(p => p.models
      .filter((m) => {
        const catalog = getAvailableCatalogFromAdvancedJson(m.advanced_json)
        const outputModalities = Array.isArray(catalog?.output_modalities) ? catalog.output_modalities : []
        return outputModalities.includes('image')
      })
      .map(m => ({
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
  const imageProfile = profiles.find((p) => p.profile === 'image')
  const imageModelValue = imageProfile?.resolved_model ?? ''
  return (
    <div className="mx-auto flex w-full max-w-[1040px] flex-col gap-7 pb-8">
      <RoutingSection title={a.spawnProfileTitle} description={a.spawnProfileSubtitle}>
        <RoutingCard>
          {PROFILE_NAMES.map(name => {
            const profile = profiles.find(p => p.profile === name)
            const currentValue = profile?.has_override ? profile.resolved_model : ''
            const meta = profileMeta[name]
            return (
              <RoutingRow
                key={name}
                title={meta.label}
                description={meta.desc}
                control={(
                  <SettingsModelDropdown
                    value={currentValue}
                    options={modelOptions}
                    placeholder={subAgentPlaceholder}
                    disabled={saving === name}
                    onChange={v => handleChange(name, v)}
                  />
                )}
              />
            )
          })}
        </RoutingCard>
      </RoutingSection>

      <RoutingSection title={ds.toolModel} description={ds.toolModelDesc}>
        <RoutingCard>
          <RoutingRow
            title={ds.toolModel}
            control={(
              <ToolModelSettingControl accessToken={accessToken} />
            )}
          />
        </RoutingCard>
      </RoutingSection>

      <RoutingSection title={a.imageGenerativeTitle} description={a.imageGenerativeDesc}>
        <RoutingCard>
          <RoutingRow
            title={a.imageGenerativeTitle}
            description={a.imageGenerativeDesc}
            control={(
              <SettingsModelDropdown
                value={imageModelValue}
                options={imageModelOptions}
                placeholder={a.imageGenerativeUnset}
                disabled={saving === 'image'}
                onChange={v => handleChange('image', v)}
              />
            )}
          />
        </RoutingCard>
      </RoutingSection>
    </div>
  )
}
