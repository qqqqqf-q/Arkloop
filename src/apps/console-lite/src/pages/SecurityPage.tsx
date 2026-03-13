import { useState, useEffect, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import { ShieldAlert, Loader2 } from 'lucide-react'
import type { LiteOutletContext } from '../layouts/LiteLayout'
import { PageHeader } from '../components/PageHeader'
import { useToast } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import {
  listPlatformSettings,
  updatePlatformSetting,
  isApiError,
} from '../api'

const KEY_REGEX_ENABLED = 'security.injection_scan.regex_enabled'

type Layer = {
  id: string
  nameKey: 'layerRegex' | 'layerSemantic'
  descKey: 'layerRegexDesc' | 'layerSemanticDesc'
  settingKey: string | null
}

const LAYERS: Layer[] = [
  {
    id: 'regex',
    nameKey: 'layerRegex',
    descKey: 'layerRegexDesc',
    settingKey: KEY_REGEX_ENABLED,
  },
  {
    id: 'semantic',
    nameKey: 'layerSemantic',
    descKey: 'layerSemanticDesc',
    settingKey: null,
  },
]

export function SecurityPage() {
  const { accessToken } = useOutletContext<LiteOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const ts = t.security

  const [values, setValues] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [toggling, setToggling] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const list = await listPlatformSettings(accessToken)
      const map: Record<string, string> = {}
      for (const s of list) map[s.key] = s.value
      setValues(map)
    } catch (err) {
      if (isApiError(err)) addToast(ts.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, ts.toastLoadFailed])

  useEffect(() => { void load() }, [load])

  const toggle = useCallback(async (key: string, current: boolean) => {
    setToggling(key)
    try {
      await updatePlatformSetting(key, String(!current), accessToken)
      setValues((prev) => ({ ...prev, [key]: String(!current) }))
      addToast(ts.toastUpdated, 'success')
    } catch (err) {
      if (isApiError(err)) addToast(ts.toastFailed, 'error')
    } finally {
      setToggling(null)
    }
  }, [accessToken, addToast, ts.toastUpdated, ts.toastFailed])

  const isEnabled = (key: string) => values[key] === 'true'

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader
        title={
          <span className="flex items-center gap-2">
            <ShieldAlert size={15} />
            {ts.title}
          </span>
        }
      />

      <div className="flex-1 overflow-y-auto p-6">
        <p className="mb-5 text-xs text-[var(--c-text-muted)]">{ts.description}</p>

        <div className="flex flex-col gap-3">
          {LAYERS.map((layer) => {
            const configurable = layer.settingKey !== null
            const enabled = configurable && isEnabled(layer.settingKey!)
            const busy = toggling === layer.settingKey

            return (
              <div
                key={layer.id}
                className="flex items-center justify-between rounded-lg border border-[var(--c-border-console)] px-4 py-3"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-[var(--c-text-primary)]">
                      {ts[layer.nameKey]}
                    </span>
                    {!configurable && (
                      <span className="rounded bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-text-muted)]">
                        {ts.statusComingSoon}
                      </span>
                    )}
                    {configurable && (
                      <span
                        className={[
                          'rounded px-1.5 py-0.5 text-[10px] font-medium',
                          enabled
                            ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                            : 'bg-[var(--c-bg-tag)] text-[var(--c-text-muted)]',
                        ].join(' ')}
                      >
                        {enabled ? ts.statusEnabled : ts.statusDisabled}
                      </span>
                    )}
                  </div>
                  <p className="mt-1 text-xs text-[var(--c-text-muted)]">
                    {ts[layer.descKey]}
                  </p>
                </div>

                {configurable && (
                  <button
                    disabled={busy}
                    onClick={() => void toggle(layer.settingKey!, enabled)}
                    className={[
                      'relative ml-4 h-5 w-9 shrink-0 rounded-full transition-colors',
                      enabled ? 'bg-emerald-500' : 'bg-[var(--c-bg-tag)]',
                      busy ? 'opacity-50' : 'cursor-pointer',
                    ].join(' ')}
                  >
                    <span
                      className={[
                        'absolute top-0.5 h-4 w-4 rounded-full bg-white shadow transition-transform',
                        enabled ? 'translate-x-[18px]' : 'translate-x-0.5',
                      ].join(' ')}
                    />
                  </button>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}
