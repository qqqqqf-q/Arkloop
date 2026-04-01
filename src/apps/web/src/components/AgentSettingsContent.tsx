import { useState, useEffect, useCallback } from 'react'
import { RotateCcw } from 'lucide-react'
import {
  type Persona,
  type SpawnProfile,
  type LlmProvider,
  listPersonas,
  patchPersona,
  isApiError,
  listSpawnProfiles,
  setSpawnProfile,
  deleteSpawnProfile,
  listLlmProviders,
} from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { SettingsSelect } from './settings/_SettingsSelect'
import { SettingsModelDropdown } from './settings/SettingsModelDropdown'

const REASONING_MODES = ['default', 'enabled', 'disabled'] as const

type Props = {
  accessToken: string
}

export function AgentSettingsContent({ accessToken }: Props) {
  const { t } = useLocale()
  const a = t.agentSettings
  const [personas, setPersonas] = useState<Persona[]>([])
  const [loading, setLoading] = useState(true)
  const [resetting, setResetting] = useState(false)
  const [resetMsg, setResetMsg] = useState('')
  const [spawnProfiles, setSpawnProfiles] = useState<SpawnProfile[]>([])
  const [providers, setProviders] = useState<LlmProvider[]>([])

  const load = useCallback(async () => {
    try {
      const [p, sp, prov] = await Promise.all([
        listPersonas(accessToken),
        listSpawnProfiles(accessToken).catch(() => [] as SpawnProfile[]),
        listLlmProviders(accessToken).catch(() => [] as LlmProvider[]),
      ])
      setPersonas(p)
      setSpawnProfiles(sp)
      setProviders(prov)
    } catch {
      // load error handled per-row
    } finally {
      setLoading(false)
    }
  }, [accessToken])

  useEffect(() => { load() }, [load])

  const handleResetAll = async () => {
    setResetting(true)
    setResetMsg('')
    let count = 0
    for (const p of personas) {
      try {
        await patchPersona(accessToken, p.id, { model: '', preferred_credential: '' }, p.scope)
        count++
      } catch { /* skip */ }
    }
    setResetMsg(a.resetDone.replace('{count}', String(count)))
    setResetting(false)
    void load()
  }

  const handleSpawnProfileChange = async (name: string, model: string) => {
    if (model === '') {
      await deleteSpawnProfile(accessToken, name)
    } else {
      await setSpawnProfile(accessToken, name, model)
    }
    const sp = await listSpawnProfiles(accessToken).catch(() => [] as SpawnProfile[])
    setSpawnProfiles(sp)
  }

  if (loading) return <div className="text-sm text-[var(--c-text-tertiary)]">{t.loading}</div>

  const modelOptions = providers.flatMap((p) =>
    p.models
      .filter((m) => m.show_in_picker)
      .map((m) => ({ value: `${p.name}^${m.model}`, label: `${p.name} · ${m.model}` })),
  )

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-medium text-[var(--c-text-heading)]">{a.title}</h3>
          <p className="mt-0.5 text-xs text-[var(--c-text-tertiary)]">{a.subtitle}</p>
        </div>
        <button
          onClick={handleResetAll}
          disabled={resetting || personas.length === 0}
          className="inline-flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-xs font-medium transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
          style={{ borderColor: 'var(--c-border-subtle)', color: 'var(--c-text-tertiary)' }}
        >
          <RotateCcw size={12} />
          {resetting ? '...' : a.resetAll}
        </button>
      </div>
      {resetMsg && <p className="text-xs text-green-500">{resetMsg}</p>}

      {personas.length === 0 ? (
        <p className="text-sm text-[var(--c-text-tertiary)]">{a.noPersonas}</p>
      ) : (
        <div className="flex flex-col gap-2">
          {personas.map((p) => (
            <PersonaRow
              key={p.id}
              persona={p}
              accessToken={accessToken}
              onUpdated={load}
            />
          ))}
        </div>
      )}

      <SpawnProfileSection
        profiles={spawnProfiles}
        modelOptions={modelOptions}
        onChange={handleSpawnProfileChange}
      />
    </div>
  )
}

function PersonaRow({
  persona,
  accessToken,
  onUpdated,
}: {
  persona: Persona
  accessToken: string
  onUpdated: () => void
}) {
  const { t } = useLocale()
  const a = t.agentSettings
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')

  const budgets = (persona.budgets ?? {}) as Record<string, unknown>
  const temperature = typeof budgets.temperature === 'number' ? budgets.temperature : 1
  const maxOutputTokens = typeof budgets.max_output_tokens === 'number' ? budgets.max_output_tokens : 32768

  const handleChange = async (field: 'reasoning_mode', value: string) => {
    setSaving(true)
    setErr('')
    try {
      await patchPersona(accessToken, persona.id, { [field]: value }, persona.scope)
      onUpdated()
    } catch (e) {
      setErr(isApiError(e) ? e.message : a.saveFailed)
    } finally {
      setSaving(false)
    }
  }

  const handleStreamThinking = async (value: boolean) => {
    setSaving(true)
    setErr('')
    try {
      await patchPersona(accessToken, persona.id, { stream_thinking: value }, persona.scope)
      onUpdated()
    } catch (e) {
      setErr(isApiError(e) ? e.message : a.saveFailed)
    } finally {
      setSaving(false)
    }
  }

  const handleBudgetChange = async (key: string, value: number) => {
    setSaving(true)
    setErr('')
    try {
      await patchPersona(accessToken, persona.id, {
        budgets: { ...budgets, [key]: value },
      }, persona.scope)
      onUpdated()
    } catch (e) {
      setErr(isApiError(e) ? e.message : a.saveFailed)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div
      className="flex flex-col gap-3 rounded-lg p-3 transition-colors"
      style={{ border: '0.5px solid var(--c-border-subtle)' }}
    >
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">
          {persona.display_name || persona.persona_key}
        </span>
        {saving && <span className="text-xs text-[var(--c-text-tertiary)]">...</span>}
      </div>

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        {/* reasoning mode */}
        <SelectField
          label={a.reasoningMode}
          value={persona.reasoning_mode || 'default'}
          onChange={(v) => handleChange('reasoning_mode', v)}
          options={REASONING_MODES.map((mode) => ({ value: mode, label: a.reasoningModes[mode] }))}
        />

        <label className="flex cursor-pointer select-none flex-col gap-1">
          <span className="text-xs text-[var(--c-text-muted)]">{a.streamThinking}</span>
          <input
            type="checkbox"
            className="h-4 w-4 rounded border-[var(--c-border-subtle)]"
            checked={persona.stream_thinking !== false}
            onChange={(e) => void handleStreamThinking(e.target.checked)}
          />
        </label>

        {/* temperature */}
        <NumberField
          label={a.temperature}
          value={temperature}
          min={0}
          max={2}
          step={0.1}
          onCommit={(v) => handleBudgetChange('temperature', v)}
        />

        {/* max output tokens */}
        <NumberField
          label={a.maxOutputTokens}
          value={maxOutputTokens}
          min={256}
          max={65536}
          step={256}
          onCommit={(v) => handleBudgetChange('max_output_tokens', v)}
        />
      </div>

      {err && <p className="text-xs text-red-400">{err}</p>}
    </div>
  )
}

const PROFILE_NAMES = ['explore', 'task', 'strong'] as const

function SpawnProfileSection({
  profiles,
  modelOptions,
  onChange,
}: {
  profiles: SpawnProfile[]
  modelOptions: { value: string; label: string }[]
  onChange: (name: string, model: string) => void
}) {
  const { t } = useLocale()
  const a = t.agentSettings
  const profileLabels: Record<string, string> = {
    explore: a.spawnProfileExplore,
    task: a.spawnProfileTask,
    strong: a.spawnProfileStrong,
  }
  const [saving, setSaving] = useState<string | null>(null)

  const handleChange = async (name: string, model: string) => {
    setSaving(name)
    try {
      await onChange(name, model)
    } finally {
      setSaving(null)
    }
  }

  return (
    <div
      className="flex flex-col gap-3 rounded-lg p-3"
      style={{ border: '0.5px solid var(--c-border-subtle)' }}
    >
      <span className="text-sm font-medium text-[var(--c-text-heading)]">{a.spawnProfileTitle}</span>
      <div className="flex flex-col gap-2">
        {PROFILE_NAMES.map((name) => {
          const profile = profiles.find((p) => p.profile === name)
          const currentValue = profile?.has_override ? profile.resolved_model : ''
          return (
            <div key={name} className="flex items-center gap-3">
              <span className="w-16 shrink-0 text-xs text-[var(--c-text-tertiary)]">
                {profileLabels[name]}
              </span>
              <div className="flex-1">
                <SettingsModelDropdown
                  value={currentValue}
                  options={modelOptions}
                  placeholder={a.spawnProfilePlatformDefault}
                  disabled={saving === name}
                  onChange={(value) => void handleChange(name, value)}
                />
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function SelectField({
  label,
  value,
  onChange,
  options,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  options: { value: string; label: string }[]
}) {
  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-[var(--c-text-tertiary)]">{label}</label>
      <SettingsSelect
        value={value}
        onChange={onChange}
        options={options}
      />
    </div>
  )
}

function NumberField({
  label,
  value,
  min,
  max,
  step,
  onCommit,
}: {
  label: string
  value: number
  min: number
  max: number
  step: number
  onCommit: (v: number) => void
}) {
  const [local, setLocal] = useState(String(value))

  useEffect(() => { setLocal(String(value)) }, [value])

  const commit = () => {
    const n = parseFloat(local)
    if (!isNaN(n) && n >= min && n <= max && n !== value) {
      onCommit(Math.round(n / step) * step)
    } else {
      setLocal(String(value))
    }
  }

  return (
    <div className="flex flex-col gap-1">
      <label className="text-xs text-[var(--c-text-tertiary)]">{label}</label>
      <input
        type="number"
        value={local}
        min={min}
        max={max}
        step={step}
        onChange={(e) => setLocal(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => { if (e.key === 'Enter') commit() }}
        className="h-7 rounded-md px-2 text-xs outline-none"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)', color: 'var(--c-text-heading)' }}
      />
    </div>
  )
}
