import { useState, useEffect, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { Badge } from '../../components/Badge'
import { useToast } from '@arkloop/shared'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { getPlatformSetting, setPlatformSetting } from '../../api/platform-settings'

const KEY_REGEX_ENABLED = 'security.injection_scan.regex_enabled'

type Layer = {
  id: string
  nameKey: 'layerRegex' | 'layerSemantic'
  descKey: 'layerRegexDesc' | 'layerSemanticDesc'
  settingsKey: string | null
}

const LAYERS: Layer[] = [
  { id: 'regex', nameKey: 'layerRegex', descKey: 'layerRegexDesc', settingsKey: KEY_REGEX_ENABLED },
  { id: 'semantic', nameKey: 'layerSemantic', descKey: 'layerSemanticDesc', settingsKey: null },
]

export function PromptInjectionPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tp = t.pages.promptInjection

  const [loading, setLoading] = useState(true)
  const [toggling, setToggling] = useState('')
  const [settings, setSettings] = useState<Record<string, boolean>>({})

  const loadSettings = useCallback(async () => {
    setLoading(true)
    try {
      const result = await getPlatformSetting(KEY_REGEX_ENABLED, accessToken)
        .catch(() => ({ value: 'true' }))
      setSettings({ [KEY_REGEX_ENABLED]: result.value === 'true' })
    } catch (err) {
      addToast(isApiError(err) ? err.message : tp.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tp.toastLoadFailed])

  useEffect(() => { loadSettings() }, [loadSettings])

  const handleToggle = useCallback(async (key: string, current: boolean) => {
    if (toggling) return
    setToggling(key)
    try {
      await setPlatformSetting(key, String(!current), accessToken)
      setSettings(prev => ({ ...prev, [key]: !current }))
      addToast(tp.toastUpdated, 'success')
    } catch (err) {
      addToast(isApiError(err) ? err.message : tp.toastFailed, 'error')
    } finally {
      setToggling('')
    }
  }, [toggling, accessToken, addToast, tp.toastUpdated, tp.toastFailed])

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tp.title} />
      <div className="flex-1 overflow-y-auto p-6">
        <p className="mb-6 text-sm text-[var(--c-text-secondary)]">{tp.description}</p>

        {loading ? (
          <div className="flex items-center justify-center py-16">
            <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            {LAYERS.map(layer => {
              const enabled = layer.settingsKey ? settings[layer.settingsKey] ?? true : false
              const comingSoon = !layer.settingsKey
              const isToggling = toggling === layer.settingsKey

              return (
                <div
                  key={layer.id}
                  className="flex items-center justify-between rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] px-5 py-4"
                >
                  <div className="flex flex-col gap-1">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-[var(--c-text-primary)]">
                        {tp[layer.nameKey]}
                      </span>
                      {comingSoon ? (
                        <Badge variant="neutral">{tp.statusComingSoon}</Badge>
                      ) : (
                        <Badge variant={enabled ? 'success' : 'warning'}>
                          {enabled ? tp.statusEnabled : tp.statusDisabled}
                        </Badge>
                      )}
                    </div>
                    <span className="text-xs text-[var(--c-text-muted)]">
                      {tp[layer.descKey]}
                    </span>
                  </div>

                  {!comingSoon && (
                    <button
                      onClick={() => handleToggle(layer.settingsKey!, enabled)}
                      disabled={isToggling || loading}
                      className={`relative inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full transition-colors duration-200 ${
                        enabled
                          ? 'bg-[var(--c-status-success)]'
                          : 'bg-[var(--c-border-console)]'
                      } ${(isToggling || loading) ? 'opacity-50 cursor-not-allowed' : ''}`}
                    >
                      {isToggling ? (
                        <Loader2 size={12} className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 animate-spin text-white" />
                      ) : (
                        <span
                          className={`inline-block h-4 w-4 rounded-full bg-white shadow transition-transform duration-200 ${
                            enabled ? 'translate-x-[22px]' : 'translate-x-[3px]'
                          }`}
                        />
                      )}
                    </button>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
