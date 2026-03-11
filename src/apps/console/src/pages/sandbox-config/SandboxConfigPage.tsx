import { useState, useEffect, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Loader2, Save } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { getPlatformSetting, setPlatformSetting } from '../../api/platform-settings'

const KEYS = {
  baseUrl:           'sandbox.base_url',
  provider:          'sandbox.provider',
  allowEgress:       'sandbox.allow_egress',
  dockerImage:       'sandbox.docker_image',
  maxSessions:       'sandbox.max_sessions',
  agentPort:         'sandbox.agent_port',
  bootTimeout:       'sandbox.boot_timeout_s',
  warmLite:          'sandbox.warm_lite',
  warmPro:           'sandbox.warm_pro',
  refillInterval:    'sandbox.refill_interval_s',
  refillConcurrency: 'sandbox.refill_concurrency',
  idleLite:          'sandbox.idle_timeout_lite_s',
  idlePro:           'sandbox.idle_timeout_pro_s',
  maxLifetime:       'sandbox.max_lifetime_s',
} as const

type FormState = Record<keyof typeof KEYS, string>

const DEFAULTS: FormState = {
  baseUrl: '',
  provider: 'firecracker',
  allowEgress: 'true',
  dockerImage: 'arkloop/sandbox-agent:latest',
  maxSessions: '50',
  agentPort: '8080',
  bootTimeout: '30',
  warmLite: '3',
  warmPro: '2',
  refillInterval: '5',
  refillConcurrency: '2',
  idleLite: '180',
  idlePro: '300',
  maxLifetime: '1800',
}

export function SandboxConfigPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.sandboxConfig

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
    e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>,
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

            {/* Provider */}
            <div className={sectionCls}>
              <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.sectionProvider}</h3>
              <div className="mt-4 space-y-4">
                <div>
                  <label className={labelCls}>{tc.fieldProvider}</label>
                  <select value={form.provider} onChange={set('provider')} className={inputCls}>
                    <option value="firecracker">Firecracker</option>
                    <option value="docker">Docker</option>
                  </select>
                </div>
                <div>
                  <label className={labelCls}>{tc.fieldBaseUrl}</label>
                  <input
                    type="text"
                    className={inputCls}
                    value={form.baseUrl}
                    onChange={set('baseUrl')}
                    placeholder="http://sandbox:8002"
                  />
                </div>
                <div>
                  <label className={labelCls}>{tc.fieldAllowEgress}</label>
                  <select value={form.allowEgress} onChange={set('allowEgress')} className={inputCls}>
                    <option value="true">true</option>
                    <option value="false">false</option>
                  </select>
                </div>
                {form.provider === 'docker' && (
                  <div>
                    <label className={labelCls}>{tc.fieldDockerImage}</label>
                    <input
                      type="text"
                      className={inputCls}
                      value={form.dockerImage}
                      onChange={set('dockerImage')}
                    />
                  </div>
                )}
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className={labelCls}>{tc.fieldMaxSessions}</label>
                    <input type="number" min={1} className={inputCls} value={form.maxSessions} onChange={set('maxSessions')} />
                  </div>
                  <div>
                    <label className={labelCls}>{tc.fieldBootTimeout}</label>
                    <input type="number" min={1} className={inputCls} value={form.bootTimeout} onChange={set('bootTimeout')} />
                  </div>
                </div>
              </div>
            </div>

            {/* Warm Pool */}
            <div className={sectionCls}>
              <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.sectionPool}</h3>
              <div className="mt-4 space-y-4">
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className={labelCls}>Lite</label>
                    <input type="number" min={0} className={inputCls} value={form.warmLite} onChange={set('warmLite')} />
                  </div>
                  <div>
                    <label className={labelCls}>Pro</label>
                    <input type="number" min={0} className={inputCls} value={form.warmPro} onChange={set('warmPro')} />
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className={labelCls}>{tc.fieldRefillInterval}</label>
                    <input type="number" min={1} className={inputCls} value={form.refillInterval} onChange={set('refillInterval')} />
                  </div>
                  <div>
                    <label className={labelCls}>{tc.fieldRefillConcurrency}</label>
                    <input type="number" min={1} className={inputCls} value={form.refillConcurrency} onChange={set('refillConcurrency')} />
                  </div>
                </div>
              </div>
            </div>

            {/* Session Timeout */}
            <div className={sectionCls}>
              <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.sectionTimeout}</h3>
              <div className="mt-4 space-y-4">
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className={labelCls}>Lite (s)</label>
                    <input type="number" min={1} className={inputCls} value={form.idleLite} onChange={set('idleLite')} />
                  </div>
                  <div>
                    <label className={labelCls}>Pro (s)</label>
                    <input type="number" min={1} className={inputCls} value={form.idlePro} onChange={set('idlePro')} />
                  </div>
                </div>
                <div>
                  <label className={labelCls}>{tc.fieldMaxLifetime}</label>
                  <input type="number" min={1} className={inputCls} value={form.maxLifetime} onChange={set('maxLifetime')} />
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
