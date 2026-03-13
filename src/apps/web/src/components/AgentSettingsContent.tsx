import { useState, useEffect, useCallback } from 'react'
import { RotateCcw } from 'lucide-react'
import {
  type Persona,
  type LlmProvider,
  listPersonas,
  listLlmProviders,
  patchPersona,
  isApiError,
} from '../api'
import { useLocale } from '../contexts/LocaleContext'

const REASONING_MODES = ['default', 'enabled', 'disabled'] as const

type Props = {
  accessToken: string
}

export function AgentSettingsContent({ accessToken }: Props) {
  const { t } = useLocale()
  const a = t.agentSettings
  const [personas, setPersonas] = useState<Persona[]>([])
  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [loading, setLoading] = useState(true)
  const [resetting, setResetting] = useState(false)
  const [resetMsg, setResetMsg] = useState('')

  const load = useCallback(async () => {
    try {
      const [p, prov] = await Promise.all([
        listPersonas(accessToken),
        listLlmProviders(accessToken),
      ])
      setPersonas(p)
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

  if (loading) return <div className="text-sm text-[var(--c-text-tertiary)]">{t.loading}</div>

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
              providers={providers}
              accessToken={accessToken}
              onUpdated={load}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function PersonaRow({
  persona,
  providers,
  accessToken,
  onUpdated,
}: {
  persona: Persona
  providers: LlmProvider[]
  accessToken: string
  onUpdated: () => void
}) {
  const { t } = useLocale()
  const a = t.agentSettings
  const [saving, setSaving] = useState(false)
  const [err, setErr] = useState('')

  const budgets = (persona.budgets ?? {}) as Record<string, unknown>
  const temperature = typeof budgets.temperature === 'number' ? budgets.temperature : 1
  const maxOutputTokens = typeof budgets.max_output_tokens === 'number' ? budgets.max_output_tokens : 4096

  // build combined model options: "credentialName^modelName"
  const modelOptions: { value: string; label: string }[] = [
    { value: '', label: a.credentialDefault },
  ]
  for (const p of providers) {
    for (const m of p.models) {
      modelOptions.push({ value: `${p.name}^${m.model}`, label: `${p.name} / ${m.model}` })
    }
  }

  // current combined value: model field may already be "credName^model" or just "model"
  const currentCombo = (() => {
    const model = persona.model ?? ''
    if (!model) return ''
    if (model.includes('^')) return model
    // legacy: model is plain model name + separate preferred_credential
    const cred = persona.preferred_credential ?? ''
    return cred ? `${cred}^${model}` : model
  })()

  const handleModelComboChange = async (combo: string) => {
    setSaving(true)
    setErr('')
    try {
      if (!combo) {
        // reset to platform default
        await patchPersona(accessToken, persona.id, { model: '', preferred_credential: '' }, persona.scope)
      } else {
        // combo is "credName^model"
        await patchPersona(accessToken, persona.id, { model: combo, preferred_credential: '' }, persona.scope)
      }
      onUpdated()
    } catch (e) {
      setErr(isApiError(e) ? e.message : a.saveFailed)
    } finally {
      setSaving(false)
    }
  }

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

      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
        {/* model + credential combined */}
        <SelectField
          label={a.model}
          value={currentCombo}
          onChange={handleModelComboChange}
          options={modelOptions}
        />

        {/* reasoning mode */}
        <SelectField
          label={a.reasoningMode}
          value={persona.reasoning_mode || 'default'}
          onChange={(v) => handleChange('reasoning_mode', v)}
          options={REASONING_MODES.map((mode) => ({ value: mode, label: a.reasoningModes[mode] }))}
        />

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
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="h-7 rounded-md px-2 text-xs outline-none"
        style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)', color: 'var(--c-text-heading)' }}
      >
        {options.map((o) => (
          <option key={o.value} value={o.value}>{o.label}</option>
        ))}
      </select>
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
