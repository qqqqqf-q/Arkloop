import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Eye, EyeOff, Loader2 } from 'lucide-react'
import { useToast } from '@arkloop/shared'
import type { ConnectorsConfig, FetchProvider, SearchProvider, XSearchProvider } from '@arkloop/shared/desktop'
import { useLocale } from '../../contexts/LocaleContext'
import { getDesktopConnectorsApi } from '../../desktopConnectorsApi'
import { getToolProviderOAuthStatus, startToolProviderOAuth } from '../../api-admin'
import { SettingsInput } from './_SettingsInput'
import { SettingsSelect } from './_SettingsSelect'

type Props = {
  accessToken: string
}

function normalizeConnectorsConfig(config: ConnectorsConfig): ConnectorsConfig {
  return {
    ...config,
    xSearch: config.xSearch ?? { provider: 'none' },
  }
}

function SettingsGroup({
  title,
  children,
}: {
  title: string
  children: ReactNode
}) {
  return (
    <section className="flex flex-col gap-2.5">
      <h3 className="pl-2.5 text-[13px] font-normal text-[var(--c-text-secondary)]">{title}</h3>
      {children}
    </section>
  )
}

function SettingsCard({ children }: { children: ReactNode }) {
  return (
    <div className="overflow-hidden rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)]">
      {children}
    </div>
  )
}

function SettingsRow({
  title,
  description,
  control,
  disabled,
}: {
  title: string
  description?: ReactNode
  control: ReactNode
  disabled?: boolean
}) {
  return (
    <div
      className={[
        'relative grid items-center gap-3 px-5 py-4 sm:grid-cols-[minmax(0,1fr)_minmax(220px,320px)] sm:gap-6 [&+&]:before:absolute [&+&]:before:left-5 [&+&]:before:right-5 [&+&]:before:top-0 [&+&]:before:h-px [&+&]:before:bg-[var(--c-border-subtle)] [&+&]:before:content-[\'\']',
        disabled ? 'opacity-50' : '',
      ].filter(Boolean).join(' ')}
    >
      <div className="min-w-0">
        <div className="text-[13px] font-medium text-[var(--c-text-primary)]">{title}</div>
        {description && (
          <div className="mt-1 text-xs leading-5 text-[var(--c-text-tertiary)]">{description}</div>
        )}
      </div>
      <div className="min-w-0 sm:justify-self-end sm:[&>*]:w-[320px]">{control}</div>
    </div>
  )
}

function PasswordInput({
  value,
  onChange,
  placeholder,
  disabled,
  showLabel,
  hideLabel,
}: {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  disabled?: boolean
  showLabel: string
  hideLabel: string
}) {
  const [show, setShow] = useState(false)

  return (
    <div className="relative">
      <SettingsInput
        type={show ? 'text' : 'password'}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        disabled={disabled}
        variant="md"
        className="pr-9"
      />
      <button
        type="button"
        onClick={() => setShow((value) => !value)}
        disabled={disabled}
        aria-label={show ? hideLabel : showLabel}
        className="absolute right-2.5 top-1/2 flex h-6 w-6 -translate-y-1/2 items-center justify-center rounded-md text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)] disabled:pointer-events-none"
      >
        {show ? <EyeOff size={13} /> : <Eye size={13} />}
      </button>
    </div>
  )
}

export function SearchFetchSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const { addToast } = useToast()
  const connectorsApi = getDesktopConnectorsApi(accessToken)

  const [config, setConfig] = useState<ConnectorsConfig | null>(null)
  const [loading, setLoading] = useState(!!connectorsApi)
  const [oauthLoading, setOauthLoading] = useState(false)
  const initializedRef = useRef(false)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    if (!connectorsApi) return
    let active = true
    void connectorsApi.get().then((nextConfig) => {
      if (!active) return
      setConfig(normalizeConnectorsConfig(nextConfig))
      setLoading(false)
      initializedRef.current = true
    }).catch(() => {
      if (active) setLoading(false)
    })
    return () => {
      active = false
    }
  }, [connectorsApi])

  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [])

  const handleSave = useCallback(async (nextConfig: ConnectorsConfig) => {
    if (!connectorsApi) return
    try {
      await connectorsApi.set(nextConfig)
      addToast(ds.connectorSaved, 'success')
    } catch {
      addToast(ds.connectorSaveFailed, 'error')
    }
  }, [addToast, connectorsApi, ds.connectorSaved, ds.connectorSaveFailed])

  const scheduleAutoSave = useCallback((nextConfig: ConnectorsConfig) => {
    if (!initializedRef.current) return
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      void handleSave(nextConfig)
    }, 500)
  }, [handleSave])

  const patchFetch = useCallback((patch: Partial<ConnectorsConfig['fetch']>) => {
    setConfig((prev) => {
      if (!prev) return prev
      const next = { ...prev, fetch: { ...prev.fetch, ...patch } }
      scheduleAutoSave(next)
      return next
    })
  }, [scheduleAutoSave])

  const patchSearch = useCallback((patch: Partial<ConnectorsConfig['search']>) => {
    setConfig((prev) => {
      if (!prev) return prev
      const next = { ...prev, search: { ...prev.search, ...patch } }
      scheduleAutoSave(next)
      return next
    })
  }, [scheduleAutoSave])

  const patchXSearch = useCallback((patch: Partial<ConnectorsConfig['xSearch']>) => {
    setConfig((prev) => {
      if (!prev) return prev
      const next = { ...prev, xSearch: { ...prev.xSearch, ...patch } }
      scheduleAutoSave(next)
      return next
    })
  }, [scheduleAutoSave])

  const refreshConfig = useCallback(async () => {
    if (!connectorsApi) return
    const nextConfig = await connectorsApi.get()
    setConfig(normalizeConnectorsConfig(nextConfig))
  }, [connectorsApi])

  const handleXaiOAuth = useCallback(async () => {
    if (!connectorsApi || !config) return
    setOauthLoading(true)
    try {
      await connectorsApi.set(config)
      const started = await startToolProviderOAuth(accessToken, 'x_search', 'x_search.xai')
      window.open(started.authorization_url, '_blank', 'noopener,noreferrer,width=720,height=820')
      const deadline = Date.now() + 10 * 60 * 1000
      while (Date.now() < deadline) {
        await new Promise((resolve) => window.setTimeout(resolve, 1500))
        const status = await getToolProviderOAuthStatus(accessToken, 'x_search', 'x_search.xai', started.state)
        if (status.completed) {
          await refreshConfig()
          addToast(ds.connectorSaved, 'success')
          return
        }
        if (status.expired) break
      }
      addToast(ds.connectorSaveFailed, 'error')
    } catch {
      addToast(ds.connectorSaveFailed, 'error')
    } finally {
      setOauthLoading(false)
    }
  }, [accessToken, addToast, config, connectorsApi, ds.connectorSaved, ds.connectorSaveFailed, refreshConfig])

  const fetchProviderOptions = [
    { value: 'none', label: ds.providerNone },
    { value: 'jina', label: ds.fetchProviderJina },
    { value: 'basic', label: ds.fetchProviderBasic },
    { value: 'firecrawl', label: ds.fetchProviderFirecrawl },
  ]
  const searchProviderOptions = [
    { value: 'none', label: ds.providerNone },
    { value: 'basic', label: ds.searchProviderBasic },
    { value: 'tavily', label: ds.searchProviderTavily },
    { value: 'exa', label: ds.searchProviderExa },
    { value: 'searxng', label: ds.searchProviderSearxng },
  ]
  const xSearchProviderOptions = [
    { value: 'none', label: ds.providerNone },
    { value: 'xai_oauth', label: ds.xSearchProviderXaiOAuth },
    { value: 'xai_api_key', label: ds.xSearchProviderXaiApiKey },
  ]

  return (
    <div className="mx-auto flex w-full max-w-[760px] flex-col gap-6 px-1 pb-8">
      <div>
        <h2 className="text-[24px] font-semibold leading-tight tracking-normal text-[var(--c-text-heading)]">
          {ds.tools}
        </h2>
      </div>

      <SettingsGroup title={ds.desktopConnectorsTitle}>
        <SettingsCard>
          {loading && (
            <div className="flex items-center justify-center py-20">
              <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
            </div>
          )}

          {!loading && (!config || !connectorsApi) && (
            <div className="px-5 py-10 text-center text-sm text-[var(--c-text-tertiary)]">
              {ds.desktopConnectorsUnavailable}
            </div>
          )}

          {!loading && config && connectorsApi && (
            <>
              <SettingsRow
                title={ds.fetchConnectorTitle}
                description={ds.fetchConnectorDesc}
                control={(
                  <SettingsSelect
                    value={config.fetch.provider}
                    options={fetchProviderOptions}
                    onChange={(value) => patchFetch({ provider: value as FetchProvider })}
                    triggerClassName="h-9"
                  />
                )}
              />
              {config.fetch.provider === 'jina' && (
                <SettingsRow
                  title={ds.apiKeyOptionalLabel}
                  description={ds.fetchProviderJina}
                  control={(
                    <PasswordInput
                      value={config.fetch.jinaApiKey ?? ''}
                      onChange={(value) => patchFetch({ jinaApiKey: value || undefined, jinaApiKeyStored: false })}
                      placeholder="jina_..."
                      showLabel={ds.connectorShowSecret}
                      hideLabel={ds.connectorHideSecret}
                    />
                  )}
                />
              )}
              {config.fetch.provider === 'firecrawl' && (
                <>
                  <SettingsRow
                    title={ds.apiKeyLabel}
                    description={ds.fetchProviderFirecrawl}
                    control={(
                      <PasswordInput
                        value={config.fetch.firecrawlApiKey ?? ''}
                        onChange={(value) => patchFetch({ firecrawlApiKey: value || undefined, firecrawlApiKeyStored: false })}
                        placeholder="fc-..."
                        showLabel={ds.connectorShowSecret}
                        hideLabel={ds.connectorHideSecret}
                      />
                    )}
                  />
                  <SettingsRow
                    title={ds.baseUrlLabel}
                    description={ds.fetchProviderFirecrawl}
                    control={(
                      <SettingsInput
                        type="text"
                        value={config.fetch.firecrawlBaseUrl ?? ''}
                        onChange={(event) => patchFetch({ firecrawlBaseUrl: event.target.value || undefined })}
                        placeholder="https://api.firecrawl.dev"
                        variant="md"
                      />
                    )}
                  />
                </>
              )}

              <SettingsRow
                title={ds.searchConnectorTitle}
                description={ds.searchConnectorDesc}
                control={(
                  <SettingsSelect
                    value={config.search.provider}
                    options={searchProviderOptions}
                    onChange={(value) => patchSearch({ provider: value as SearchProvider })}
                    triggerClassName="h-9"
                  />
                )}
              />
              {config.search.provider === 'tavily' && (
                <SettingsRow
                  title={ds.apiKeyLabel}
                  description={ds.searchProviderTavily}
                  control={(
                    <PasswordInput
                      value={config.search.tavilyApiKey ?? ''}
                      onChange={(value) => patchSearch({ tavilyApiKey: value || undefined, tavilyApiKeyStored: false })}
                      placeholder="tvly-..."
                      showLabel={ds.connectorShowSecret}
                      hideLabel={ds.connectorHideSecret}
                    />
                  )}
                />
              )}
              {config.search.provider === 'searxng' && (
                <SettingsRow
                  title={ds.baseUrlLabel}
                  description={ds.searchProviderSearxng}
                  control={(
                    <SettingsInput
                      type="text"
                      value={config.search.searxngBaseUrl ?? ''}
                      onChange={(event) => patchSearch({ searxngBaseUrl: event.target.value || undefined })}
                      placeholder="http://localhost:4000"
                      variant="md"
                    />
                  )}
                />
              )}

              <SettingsRow
                title={ds.xSearchConnectorTitle}
                description={ds.xSearchConnectorDesc}
                control={(
                  <SettingsSelect
                    value={config.xSearch.provider}
                    options={xSearchProviderOptions}
                    onChange={(value) => patchXSearch({ provider: value as XSearchProvider })}
                    triggerClassName="h-9"
                  />
                )}
              />
              {config.xSearch.provider === 'xai_oauth' && (
                <SettingsRow
                  title={ds.xSearchProviderXaiOAuth}
                  description={config.xSearch.xaiOAuthConnected ? ds.connectorConfigured : ds.xSearchProviderXai}
                  control={(
                    <button
                      type="button"
                      onClick={handleXaiOAuth}
                      disabled={oauthLoading}
                      className="flex h-9 items-center justify-center rounded-lg border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:pointer-events-none disabled:opacity-60"
                    >
                      {oauthLoading ? <Loader2 size={15} className="animate-spin" /> : ds.oauthConnect}
                    </button>
                  )}
                />
              )}
              {config.xSearch.provider === 'xai_api_key' && (
                <>
                  <SettingsRow
                    title={ds.apiKeyLabel}
                    description={ds.xSearchProviderXai}
                    control={(
                      <PasswordInput
                        value={config.xSearch.xaiApiKey ?? ''}
                        onChange={(value) => patchXSearch({ xaiApiKey: value || undefined, xaiApiKeyStored: false })}
                        placeholder="xai-..."
                        showLabel={ds.connectorShowSecret}
                        hideLabel={ds.connectorHideSecret}
                      />
                    )}
                  />
                </>
              )}
            </>
          )}
        </SettingsCard>
      </SettingsGroup>
    </div>
  )
}
