import { useState, useEffect, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Loader2, Save } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '@arkloop/shared'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { getPlatformSetting, setPlatformSetting } from '../../api/platform-settings'

const KEYS = {
  baseUrl:        'openviking.base_url',
  rootApiKey:     'openviking.root_api_key',
  costPerCommit:  'openviking.cost_per_commit',
} as const

type FormState = Record<keyof typeof KEYS, string>

const DEFAULTS: FormState = {
  baseUrl: '',
  rootApiKey: '',
  costPerCommit: '0',
}

export function MemoryConfigPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.memoryConfig

  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [form, setForm] = useState<FormState>({ ...DEFAULTS })
  const [saved, setSaved] = useState<FormState>({ ...DEFAULTS })

  const loadAll = useCallback(async () => {
    setLoading(true)
    try {
      const entries = Object.entries(KEYS) as [keyof typeof KEYS, string][]
      const results = await Promise.allSettled(
        entries.map(([, key]) => getPlatformSetting(key, accessToken)),
      )
      const next = { ...DEFAULTS }
      entries.forEach(([field], i) => {
        if (results[i].status === 'fulfilled') {
          const v = results[i].value.value
          if (v !== '') next[field] = v
        }
      })
      setForm(next)
      setSaved(next)
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => { void loadAll() }, [loadAll])

  const isDirty = (Object.keys(KEYS) as (keyof typeof KEYS)[]).some(
    (k) => form[k] !== saved[k],
  )

  const handleSave = async () => {
    setSaving(true)
    try {
      const entries = Object.entries(KEYS) as [keyof typeof KEYS, string][]
      const ops = entries
        .filter(([field]) => form[field] !== saved[field])
        .map(([field, key]) => setPlatformSetting(key, form[field].trim(), accessToken))
      await Promise.all(ops)
      setSaved({ ...form })
      addToast(tc.toastSaved, 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastSaveFailed, 'error')
    } finally {
      setSaving(false)
    }
  }

  const set = (field: keyof typeof KEYS) => (
    e: React.ChangeEvent<HTMLInputElement>,
  ) => setForm((prev) => ({ ...prev, [field]: e.target.value }))

  const inputCls =
    'w-full rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-border-focus)]'
  const labelCls = 'mb-1 block text-xs font-medium text-[var(--c-text-secondary)]'
  const sectionCls = 'rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5'

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} />
      <div className="flex-1 overflow-y-auto p-6">
        {loading ? (
          <div className="flex items-center justify-center py-16">
            <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
          </div>
        ) : (
          <div className="mx-auto max-w-xl space-y-6">

            {/* Provider: OpenViking */}
            <div className={sectionCls}>
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-medium text-[var(--c-text-primary)]">OpenViking</h3>
                <span className="rounded bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-text-muted)]">
                  {tc.providerTag}
                </span>
              </div>
              <div className="mt-4 space-y-4">
                <div>
                  <label className={labelCls}>{tc.fieldBaseUrl}</label>
                  <input
                    type="text"
                    className={inputCls}
                    value={form.baseUrl}
                    onChange={set('baseUrl')}
                    placeholder="http://openviking:19000"
                  />
                </div>
                <div>
                  <label className={labelCls}>{tc.fieldApiKey}</label>
                  <input
                    type="password"
                    className={inputCls}
                    value={form.rootApiKey}
                    onChange={set('rootApiKey')}
                    placeholder="••••••••"
                  />
                </div>
              </div>
            </div>

            {/* Billing */}
            <div className={sectionCls}>
              <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.sectionBilling}</h3>
              <div className="mt-4">
                <div>
                  <label className={labelCls}>{tc.fieldCostPerCommit}</label>
                  <input
                    type="number"
                    min={0}
                    step="0.001"
                    className={inputCls}
                    value={form.costPerCommit}
                    onChange={set('costPerCommit')}
                    placeholder="0"
                  />
                  <p className="mt-1 text-xs text-[var(--c-text-muted)]">{tc.fieldCostPerCommitHint}</p>
                </div>
              </div>
            </div>

            {/* Save */}
            <div className="border-t border-[var(--c-border-console)] pt-4">
              <button
                onClick={handleSave}
                disabled={saving || !isDirty}
                className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              >
                {saving ? <Loader2 size={12} className="animate-spin" /> : <Save size={12} />}
                {tc.save}
              </button>
            </div>

          </div>
        )}
      </div>
    </div>
  )
}
