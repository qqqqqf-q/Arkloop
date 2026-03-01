import { useCallback, useEffect, useMemo, useState } from 'react'
import { useOutletContext } from 'react-router-dom'
import { ChevronDown, ChevronRight, Loader2, Save, Search, Trash2 } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { listConfigSchema, type ConfigSchemaEntry } from '../../api/config-schema'
import { listPlatformSettings, setPlatformSetting, deletePlatformSetting } from '../../api/platform-settings'
import { listOrgSettings, setOrgSetting, deleteOrgSetting } from '../../api/org-settings'

const MASKED = '******'

const inputClass =
  'w-full rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-border-focus)]'

function groupByPrefix(entries: ConfigSchemaEntry[]): Map<string, ConfigSchemaEntry[]> {
  const groups = new Map<string, ConfigSchemaEntry[]>()
  for (const entry of entries) {
    const dot = entry.key.indexOf('.')
    const prefix = dot > 0 ? entry.key.slice(0, dot) : entry.key
    const list = groups.get(prefix) ?? []
    list.push(entry)
    groups.set(prefix, list)
  }
  return groups
}

function FieldInput({
  entry,
  value,
  onChange,
}: {
  entry: ConfigSchemaEntry
  value: string
  onChange: (v: string) => void
}) {
  if (entry.type === 'bool') {
    return (
      <select className={inputClass} value={value} onChange={(e) => onChange(e.target.value)}>
        <option value="true">true</option>
        <option value="false">false</option>
      </select>
    )
  }

  if (entry.type === 'int' || entry.type === 'number') {
    return (
      <input
        type="number"
        className={inputClass}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        step={entry.type === 'number' ? 'any' : '1'}
      />
    )
  }

  // string / duration
  return (
    <input
      type={entry.sensitive ? 'password' : 'text'}
      className={inputClass}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      placeholder={entry.sensitive ? MASKED : entry.default || undefined}
      autoComplete={entry.sensitive ? 'new-password' : 'off'}
    />
  )
}

function PlatformSection({
  prefix,
  entries,
  values,
  savedValues,
  onValueChange,
  onSave,
  onReset,
  saving,
}: {
  prefix: string
  entries: ConfigSchemaEntry[]
  values: Record<string, string>
  savedValues: Record<string, string>
  onValueChange: (key: string, value: string) => void
  onSave: (keys: string[]) => void
  onReset: (keys: string[]) => void
  saving: boolean
}) {
  const [collapsed, setCollapsed] = useState(false)
  const keys = useMemo(() => entries.map((e) => e.key), [entries])

  const dirty = keys.some((k) => (values[k] ?? '') !== (savedValues[k] ?? ''))

  return (
    <div className="rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)]">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-5 py-3 text-left"
        onClick={() => setCollapsed(!collapsed)}
      >
        {collapsed ? (
          <ChevronRight size={14} className="text-[var(--c-text-muted)]" />
        ) : (
          <ChevronDown size={14} className="text-[var(--c-text-muted)]" />
        )}
        <span className="text-sm font-medium text-[var(--c-text-primary)]">{prefix}</span>
        <span className="text-xs text-[var(--c-text-muted)]">({entries.length})</span>
      </button>

      {!collapsed && (
        <div className="border-t border-[var(--c-border-console)] px-5 py-4">
          <div className="space-y-4">
            {entries.map((entry) => (
              <div key={entry.key}>
                <label className="mb-1 block text-xs font-medium text-[var(--c-text-secondary)]">
                  {entry.key}
                </label>
                <FieldInput
                  entry={entry}
                  value={values[entry.key] ?? ''}
                  onChange={(v) => onValueChange(entry.key, v)}
                />
                {entry.description && (
                  <p className="mt-1 text-xs text-[var(--c-text-muted)]">{entry.description}</p>
                )}
              </div>
            ))}
          </div>
          <div className="mt-4 flex items-center gap-2 border-t border-[var(--c-border-console)] pt-4">
            <button
              onClick={() => onSave(keys)}
              disabled={saving || !dirty}
              className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
            >
              {saving ? <Loader2 size={12} className="animate-spin" /> : <Save size={12} />}
              Save
            </button>
            <button
              onClick={() => onReset(keys)}
              disabled={!dirty}
              className="inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs text-[var(--c-text-muted)] transition-colors hover:text-[var(--c-text-secondary)] disabled:opacity-50"
            >
              Reset
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function OrgOverrideSection({
  entries,
  orgId,
  orgValues,
  orgDrafts,
  onOrgDraftChange,
  onOrgSave,
  onOrgDelete,
  saving,
}: {
  entries: ConfigSchemaEntry[]
  orgId: string
  orgValues: Record<string, string>
  orgDrafts: Record<string, string>
  onOrgDraftChange: (key: string, value: string) => void
  onOrgSave: (key: string) => void
  onOrgDelete: (key: string) => void
  saving: boolean
}) {
  if (entries.length === 0) return null

  return (
    <div className="space-y-3">
      {entries.map((entry) => {
        const hasOverride = orgValues[entry.key] !== undefined
        const draftValue = orgDrafts[entry.key] ?? orgValues[entry.key] ?? ''
        const isDirty = draftValue !== (orgValues[entry.key] ?? '')

        return (
          <div key={entry.key} className="rounded-md border border-[var(--c-border-console)] p-3">
            <div className="mb-2 flex items-center justify-between">
              <label className="text-xs font-medium text-[var(--c-text-secondary)]">
                {entry.key}
              </label>
              {hasOverride && (
                <button
                  onClick={() => onOrgDelete(entry.key)}
                  disabled={saving}
                  className="inline-flex items-center gap-1 text-xs text-red-500 hover:text-red-600 disabled:opacity-50"
                >
                  <Trash2 size={11} />
                </button>
              )}
            </div>
            <div className="flex items-center gap-2">
              <div className="flex-1">
                <FieldInput
                  entry={entry}
                  value={draftValue}
                  onChange={(v) => onOrgDraftChange(entry.key, v)}
                />
              </div>
              <button
                onClick={() => onOrgSave(entry.key)}
                disabled={saving || !isDirty || draftValue.trim() === ''}
                className="inline-flex items-center gap-1 rounded-md border border-[var(--c-border-console)] px-2 py-1.5 text-xs text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              >
                <Save size={11} />
              </button>
            </div>
            {entry.description && (
              <p className="mt-1 text-xs text-[var(--c-text-muted)]">{entry.description}</p>
            )}
          </div>
        )
      })}
    </div>
  )
}

export function PlatformConfigPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.platformConfig

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const [schema, setSchema] = useState<ConfigSchemaEntry[]>([])
  const [values, setValues] = useState<Record<string, string>>({})
  const [savedValues, setSavedValues] = useState<Record<string, string>>({})

  // org override
  const [orgId, setOrgId] = useState('')
  const [orgLoading, setOrgLoading] = useState(false)
  const [orgValues, setOrgValues] = useState<Record<string, string>>({})
  const [orgDrafts, setOrgDrafts] = useState<Record<string, string>>({})
  const [orgLoaded, setOrgLoaded] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [schemaData, settingsData] = await Promise.all([
        listConfigSchema(accessToken),
        listPlatformSettings(accessToken),
      ])
      setSchema(schemaData)

      const valMap: Record<string, string> = {}
      for (const s of settingsData) {
        valMap[s.key] = s.value
      }
      setValues(valMap)
      setSavedValues({ ...valMap })
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => {
    void load()
  }, [load])

  const groups = useMemo(() => groupByPrefix(schema), [schema])
  const sortedPrefixes = useMemo(() => [...groups.keys()].sort(), [groups])

  const handleValueChange = (key: string, value: string) => {
    setValues((prev) => ({ ...prev, [key]: value }))
  }

  const handleSave = async (keys: string[]) => {
    setSaving(true)
    try {
      const ops: Promise<unknown>[] = []
      for (const key of keys) {
        const current = (values[key] ?? '').trim()
        const saved = savedValues[key] ?? ''
        if (current === saved) continue

        if (current === '' || current === MASKED) {
          // 空值时删除该 key
          ops.push(deletePlatformSetting(key, accessToken).catch(() => {}))
        } else {
          ops.push(setPlatformSetting(key, current, accessToken))
        }
      }
      await Promise.all(ops)

      // 更新 saved 基准
      const newSaved = { ...savedValues }
      for (const key of keys) {
        const v = (values[key] ?? '').trim()
        if (v === '' || v === MASKED) {
          delete newSaved[key]
        } else {
          newSaved[key] = v
        }
      }
      setSavedValues(newSaved)
      addToast(tc.toastSaved, 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastSaveFailed, 'error')
    } finally {
      setSaving(false)
    }
  }

  const handleReset = (keys: string[]) => {
    setValues((prev) => {
      const next = { ...prev }
      for (const key of keys) {
        if (savedValues[key] !== undefined) {
          next[key] = savedValues[key]
        } else {
          delete next[key]
        }
      }
      return next
    })
  }

  // org settings
  const orgScopedEntries = useMemo(
    () => schema.filter((e) => e.scope === 'org' || e.scope === 'both'),
    [schema],
  )

  const handleLoadOrg = async () => {
    const trimmedId = orgId.trim()
    if (!trimmedId) return
    setOrgLoading(true)
    try {
      const items = await listOrgSettings(trimmedId, accessToken)
      const valMap: Record<string, string> = {}
      for (const s of items) {
        valMap[s.key] = s.value
      }
      setOrgValues(valMap)
      setOrgDrafts({})
      setOrgLoaded(true)
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastLoadFailed, 'error')
    } finally {
      setOrgLoading(false)
    }
  }

  const handleOrgDraftChange = (key: string, value: string) => {
    setOrgDrafts((prev) => ({ ...prev, [key]: value }))
  }

  const handleOrgSave = async (key: string) => {
    const trimmedId = orgId.trim()
    if (!trimmedId) return
    const val = (orgDrafts[key] ?? orgValues[key] ?? '').trim()
    if (!val) return

    setSaving(true)
    try {
      await setOrgSetting(trimmedId, key, val, accessToken)
      setOrgValues((prev) => ({ ...prev, [key]: val }))
      setOrgDrafts((prev) => {
        const next = { ...prev }
        delete next[key]
        return next
      })
      addToast(tc.toastSaved, 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastSaveFailed, 'error')
    } finally {
      setSaving(false)
    }
  }

  const handleOrgDelete = async (key: string) => {
    const trimmedId = orgId.trim()
    if (!trimmedId) return

    setSaving(true)
    try {
      await deleteOrgSetting(trimmedId, key, accessToken)
      setOrgValues((prev) => {
        const next = { ...prev }
        delete next[key]
        return next
      })
      setOrgDrafts((prev) => {
        const next = { ...prev }
        delete next[key]
        return next
      })
      addToast(tc.toastDeleted, 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastDeleteFailed, 'error')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} />
      <div className="flex-1 overflow-y-auto p-6">
        {loading ? (
          <div className="flex items-center justify-center py-16">
            <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
          </div>
        ) : (
          <div className="mx-auto max-w-2xl space-y-4">
            {/* Platform Settings */}
            {sortedPrefixes.map((prefix) => (
              <PlatformSection
                key={prefix}
                prefix={prefix}
                entries={groups.get(prefix)!}
                values={values}
                savedValues={savedValues}
                onValueChange={handleValueChange}
                onSave={handleSave}
                onReset={handleReset}
                saving={saving}
              />
            ))}

            {/* Org Override */}
            {orgScopedEntries.length > 0 && (
              <div className="mt-8 rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5">
                <h3 className="text-sm font-medium text-[var(--c-text-primary)]">
                  {tc.orgOverrideTitle}
                </h3>
                <div className="mt-3 flex items-center gap-2">
                  <input
                    type="text"
                    className={inputClass}
                    value={orgId}
                    onChange={(e) => {
                      setOrgId(e.target.value)
                      setOrgLoaded(false)
                    }}
                    placeholder="org_id (UUID)"
                  />
                  <button
                    onClick={handleLoadOrg}
                    disabled={orgLoading || !orgId.trim()}
                    className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
                  >
                    {orgLoading ? (
                      <Loader2 size={12} className="animate-spin" />
                    ) : (
                      <Search size={12} />
                    )}
                    {tc.orgLoad}
                  </button>
                </div>

                {orgLoaded && (
                  <div className="mt-4">
                    <OrgOverrideSection
                      entries={orgScopedEntries}
                      orgId={orgId}
                      orgValues={orgValues}
                      orgDrafts={orgDrafts}
                      onOrgDraftChange={handleOrgDraftChange}
                      onOrgSave={handleOrgSave}
                      onOrgDelete={handleOrgDelete}
                      saving={saving}
                    />
                  </div>
                )}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
