import { useState, useCallback, useEffect } from 'react'
import {
  Loader2,
  Shield,
  Key,
  Trash2,
  X,
  Check,
  Settings,
  Power,
  Eye,
  EyeOff,
  Pencil,
  RotateCcw,
  Ban,
  Plug,
} from 'lucide-react'
import { useLocale } from '../../contexts/LocaleContext'
import {
  type ToolProviderGroup,
  type ToolProviderItem,
  type ToolCatalogGroup,
  type ToolCatalogItem,
  listToolProviders,
  activateToolProvider,
  deactivateToolProvider,
  updateToolProviderCredential,
  clearToolProviderCredential,
  updateToolProviderConfig,
  listToolCatalog,
  updateToolDescription,
  deleteToolDescription,
  updateToolDisabled,
} from '../../api-admin'

type Props = {
  accessToken: string
}

type CredentialModal = { group: string; provider: ToolProviderItem } | null
type DescEditTarget = { toolName: string; label: string; description: string } | null

function displayGroupName(group: string): string {
  return group === 'sandbox' ? 'sandbox / browser' : group
}

function flatSet(
  obj: Record<string, unknown>,
  dotPath: string,
  value: string,
): Record<string, unknown> {
  const parts = dotPath.split('.')
  const root = { ...obj }
  let cur: Record<string, unknown> = root
  for (let i = 0; i < parts.length - 1; i++) {
    const p = parts[i]
    if (typeof cur[p] !== 'object' || cur[p] == null) cur[p] = {}
    cur[p] = { ...(cur[p] as Record<string, unknown>) }
    cur = cur[p] as Record<string, unknown>
  }
  cur[parts[parts.length - 1]] = value
  return root
}

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------

const inputCls =
  'w-full rounded-md border border-[var(--c-border-subtle)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border)]'
const labelCls = 'mb-1 block text-xs font-medium text-[var(--c-text-secondary)]'
const sectionCls =
  'rounded-xl border border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] p-5'
const btnIcon =
  'rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)] disabled:opacity-40'

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export function ConnectorsSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const tt = t.adminTools

  // Data
  const [groups, setGroups] = useState<ToolProviderGroup[]>([])
  const [catalog, setCatalog] = useState<ToolCatalogGroup[]>([])
  const [selectedGroup, setSelectedGroup] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [mutating, setMutating] = useState(false)

  // Credential modal
  const [credentialModal, setCredentialModal] = useState<CredentialModal>(null)
  const [credentialForm, setCredentialForm] = useState({ apiKey: '', baseUrl: '' })
  const [credSaving, setCredSaving] = useState(false)
  const [showApiKey, setShowApiKey] = useState(false)

  // Clear credential
  const [clearTarget, setClearTarget] = useState<CredentialModal>(null)
  const [clearing, setClearing] = useState(false)

  // Config form (for providers with config_json)
  const [configForm, setConfigForm] = useState<Record<string, string>>({})
  const [configSaved, setConfigSaved] = useState<Record<string, string>>({})
  const [configSaving, setConfigSaving] = useState(false)

  // Tool description editing
  const [descEdit, setDescEdit] = useState<DescEditTarget>(null)
  const [descText, setDescText] = useState('')
  const [descSaving, setDescSaving] = useState(false)
  const [toolToggling, setToolToggling] = useState('')

  // ---- Data fetching ----

  const fetchAll = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [g, c] = await Promise.all([
        listToolProviders(accessToken),
        listToolCatalog(accessToken),
      ])
      setGroups(g)
      setCatalog(c)
    } catch {
      setError('Failed to load tool providers')
    } finally {
      setLoading(false)
    }
  }, [accessToken])

  useEffect(() => {
    void fetchAll()
  }, [fetchAll])

  // Auto-select first group when data loads
  useEffect(() => {
    if (catalog.length > 0 && !catalog.some((g) => g.group === selectedGroup)) {
      setSelectedGroup(catalog[0].group)
    }
  }, [catalog, selectedGroup])

  // Load config_json for active provider when group changes
  useEffect(() => {
    const grp = groups.find((g) => g.group_name === selectedGroup)
    if (!grp) {
      setConfigForm({})
      setConfigSaved({})
      return
    }
    const active = grp.providers.find((p) => p.is_active)
    if (!active || !active.config_json) {
      setConfigForm({})
      setConfigSaved({})
      return
    }
    const cfg = active.config_json
    const form: Record<string, string> = {}
    const flatten = (obj: Record<string, unknown>, prefix = '') => {
      for (const [k, v] of Object.entries(obj)) {
        const key = prefix ? `${prefix}.${k}` : k
        if (v != null && typeof v === 'object' && !Array.isArray(v)) {
          flatten(v as Record<string, unknown>, key)
        } else {
          form[key] = v != null ? String(v) : ''
        }
      }
    }
    flatten(cfg)
    setConfigForm(form)
    setConfigSaved({ ...form })
  }, [selectedGroup, groups])

  // ---- Derived state ----

  const activeGroup = groups.find((g) => g.group_name === selectedGroup)
  const activeProvider = activeGroup?.providers.find((p) => p.is_active)
  const catalogGroup = catalog.find((g) => g.group === selectedGroup)
  const configDirty = Object.keys(configForm).some(
    (k) => configForm[k] !== configSaved[k],
  )

  // ---- Handlers ----

  const handleActivate = useCallback(
    async (groupName: string, providerName: string) => {
      if (mutating) return
      setMutating(true)
      try {
        await activateToolProvider(accessToken, groupName, providerName)
        await fetchAll()
      } catch {
        /* ignore */
      } finally {
        setMutating(false)
      }
    },
    [mutating, accessToken, fetchAll],
  )

  const handleDeactivate = useCallback(
    async (groupName: string, providerName: string) => {
      if (mutating) return
      setMutating(true)
      try {
        await deactivateToolProvider(accessToken, groupName, providerName)
        await fetchAll()
      } catch {
        /* ignore */
      } finally {
        setMutating(false)
      }
    },
    [mutating, accessToken, fetchAll],
  )

  const openCredentialModal = useCallback(
    (group: string, provider: ToolProviderItem) => {
      setCredentialModal({ group, provider })
      setCredentialForm({ apiKey: '', baseUrl: provider.base_url ?? '' })
      setShowApiKey(false)
    },
    [],
  )

  const closeCredentialModal = useCallback(() => {
    if (credSaving) return
    setCredentialModal(null)
  }, [credSaving])

  const handleSaveCredential = useCallback(async () => {
    if (!credentialModal) return
    const trimmedKey = credentialForm.apiKey.trim()
    const trimmedBase = credentialForm.baseUrl.trim()
    const payload: Record<string, string> = {}
    if (trimmedKey) payload.api_key = trimmedKey
    if (trimmedBase) payload.base_url = trimmedBase
    if (Object.keys(payload).length === 0) {
      setCredentialModal(null)
      return
    }
    setCredSaving(true)
    try {
      await updateToolProviderCredential(
        accessToken,
        credentialModal.group,
        credentialModal.provider.provider_name,
        payload,
      )
      setCredentialModal(null)
      await fetchAll()
    } catch {
      /* ignore */
    } finally {
      setCredSaving(false)
    }
  }, [credentialModal, credentialForm, accessToken, fetchAll])

  const handleClearCredential = useCallback(async () => {
    if (!clearTarget) return
    setClearing(true)
    try {
      await clearToolProviderCredential(
        accessToken,
        clearTarget.group,
        clearTarget.provider.provider_name,
      )
      setClearTarget(null)
      await fetchAll()
    } catch {
      /* ignore */
    } finally {
      setClearing(false)
    }
  }, [clearTarget, accessToken, fetchAll])

  const handleSaveConfig = useCallback(async () => {
    if (!activeProvider) return
    setConfigSaving(true)
    try {
      let configJSON: Record<string, unknown> = {}
      for (const [k, v] of Object.entries(configForm)) {
        configJSON = flatSet(configJSON, k, v)
      }
      await updateToolProviderConfig(
        accessToken,
        selectedGroup,
        activeProvider.provider_name,
        configJSON,
      )
      setConfigSaved({ ...configForm })
    } catch {
      /* ignore */
    } finally {
      setConfigSaving(false)
    }
  }, [activeProvider, configForm, selectedGroup, accessToken])

  const handleSaveDescription = useCallback(async () => {
    if (!descEdit) return
    setDescSaving(true)
    try {
      await updateToolDescription(accessToken, descEdit.toolName, descText)
      setDescEdit(null)
      await fetchAll()
    } catch {
      /* ignore */
    } finally {
      setDescSaving(false)
    }
  }, [descEdit, descText, accessToken, fetchAll])

  const handleResetDescription = useCallback(
    async (toolName: string) => {
      try {
        await deleteToolDescription(accessToken, toolName)
        await fetchAll()
      } catch {
        /* ignore */
      }
    },
    [accessToken, fetchAll],
  )

  const handleToggleToolDisabled = useCallback(
    async (tool: ToolCatalogItem) => {
      if (toolToggling) return
      setToolToggling(tool.name)
      try {
        await updateToolDisabled(accessToken, tool.name, !tool.is_disabled)
        await fetchAll()
      } catch {
        /* ignore */
      } finally {
        setToolToggling('')
      }
    },
    [toolToggling, accessToken, fetchAll],
  )

  const setConfig =
    (key: string) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
      setConfigForm((prev) => ({ ...prev, [key]: e.target.value }))

  // ---- Render ----

  if (loading) {
    return (
      <div className="flex flex-col gap-4">
        <div>
          <h3 className="text-base font-semibold text-[var(--c-text-heading)]">
            {ds.connectorsTitle}
          </h3>
          <p className="mt-1 text-sm text-[var(--c-text-secondary)]">
            {ds.connectorsDesc}
          </p>
        </div>
        <div className="flex items-center justify-center py-16">
          <Loader2
            size={20}
            className="animate-spin text-[var(--c-text-muted)]"
          />
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex flex-col gap-4">
        <div>
          <h3 className="text-base font-semibold text-[var(--c-text-heading)]">
            {ds.connectorsTitle}
          </h3>
        </div>
        <div
          className="flex flex-col items-center justify-center rounded-xl bg-[var(--c-bg-menu)] py-16"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <p className="text-sm text-[var(--c-text-muted)]">{error}</p>
          <button
            onClick={() => void fetchAll()}
            className="mt-3 text-xs text-[var(--c-text-secondary)] underline"
          >
            Retry
          </button>
        </div>
      </div>
    )
  }

  if (groups.length === 0) {
    return (
      <div className="flex flex-col gap-4">
        <div>
          <h3 className="text-base font-semibold text-[var(--c-text-heading)]">
            {ds.connectorsTitle}
          </h3>
          <p className="mt-1 text-sm text-[var(--c-text-secondary)]">
            {ds.connectorsDesc}
          </p>
        </div>
        <div
          className="flex flex-col items-center justify-center rounded-xl bg-[var(--c-bg-menu)] py-16"
          style={{ border: '0.5px solid var(--c-border-subtle)' }}
        >
          <Plug size={32} className="mb-3 text-[var(--c-text-muted)]" />
          <p className="text-sm text-[var(--c-text-muted)]">{tt.noProviders}</p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {/* Header */}
      <div>
        <h3 className="text-base font-semibold text-[var(--c-text-heading)]">
          {tt.title}
        </h3>
        <p className="mt-1 text-sm text-[var(--c-text-secondary)]">
          {tt.subtitle}
        </p>
      </div>

      {/* Main layout: sidebar + content */}
      <div
        className="flex overflow-hidden rounded-xl"
        style={{ border: '0.5px solid var(--c-border-subtle)' }}
      >
        {/* Left sidebar: group tabs */}
        <div className="w-[160px] shrink-0 overflow-y-auto border-r border-[var(--c-border-subtle)] bg-[var(--c-bg-menu)] p-2">
          <div className="flex flex-col gap-[3px]">
            {catalog.map((g) => {
              const active = g.group === selectedGroup
              return (
                <button
                  key={g.group}
                  onClick={() => setSelectedGroup(g.group)}
                  className={[
                    'flex h-[30px] items-center rounded-md px-3 text-[13px] font-medium transition-colors',
                    active
                      ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]'
                      : 'text-[var(--c-text-muted)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]',
                  ].join(' ')}
                >
                  {displayGroupName(g.group)}
                </button>
              )
            })}
          </div>
        </div>

        {/* Right panel */}
        <div className="flex-1 overflow-y-auto bg-[var(--c-bg-menu)] p-5">
          <div className="mx-auto max-w-xl space-y-5">
            {/* Provider section */}
            {activeGroup && (
              <div className={sectionCls}>
                <h4 className="flex items-center gap-2 text-sm font-medium text-[var(--c-text-heading)]">
                  <Shield size={14} />
                  Providers
                </h4>
                <div className="mt-3 space-y-2">
                  {activeGroup.providers.map((p) => (
                    <div
                      key={p.provider_name}
                      className="flex items-center justify-between rounded-lg border border-[var(--c-border-subtle)] px-3 py-2"
                    >
                      <div className="flex items-center gap-3">
                        <span className="font-mono text-xs text-[var(--c-text-primary)]">
                          {p.provider_name}
                        </span>
                        <StatusBadge provider={p} tt={tt} />
                      </div>
                      <div className="flex items-center gap-1">
                        {!p.is_active ? (
                          <button
                            disabled={mutating}
                            onClick={() =>
                              handleActivate(p.group_name, p.provider_name)
                            }
                            className={btnIcon}
                            title={tt.activate}
                          >
                            <Power size={14} />
                          </button>
                        ) : (
                          <button
                            disabled={mutating}
                            onClick={() =>
                              handleDeactivate(p.group_name, p.provider_name)
                            }
                            className={btnIcon}
                            title={tt.deactivate}
                          >
                            <Ban size={14} />
                          </button>
                        )}
                        <button
                          disabled={mutating}
                          onClick={() =>
                            openCredentialModal(activeGroup.group_name, p)
                          }
                          className={btnIcon}
                          title={tt.editCredentials}
                        >
                          <Key size={14} />
                        </button>
                        <button
                          disabled={mutating}
                          onClick={() =>
                            setClearTarget({
                              group: activeGroup.group_name,
                              provider: p,
                            })
                          }
                          className={btnIcon}
                          title={tt.clearCredentials}
                        >
                          <Trash2 size={14} />
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Config section (for providers with config_json fields) */}
            {activeProvider &&
              Object.keys(configForm).length > 0 && (
                <div className={sectionCls}>
                  <h4 className="flex items-center gap-2 text-sm font-medium text-[var(--c-text-heading)]">
                    <Settings size={14} />
                    {tt.configSection}
                  </h4>
                  <div className="mt-3 space-y-3">
                    {Object.entries(configForm).map(([key, value]) => (
                      <div key={key}>
                        <label className={labelCls}>{key}</label>
                        <input
                          type="text"
                          className={inputCls}
                          value={value}
                          onChange={setConfig(key)}
                        />
                      </div>
                    ))}
                  </div>
                  <div className="mt-4 flex items-center gap-2">
                    <button
                      onClick={handleSaveConfig}
                      disabled={configSaving || !configDirty}
                      className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-50"
                    >
                      {configSaving ? (
                        <Loader2 size={12} className="animate-spin" />
                      ) : (
                        <Check size={12} />
                      )}
                      {configSaving ? tt.applying : tt.applyConfig}
                    </button>
                    {!configDirty &&
                      Object.keys(configSaved).length > 0 && (
                        <span className="text-xs text-[var(--c-text-muted)]">
                          ✓ {tt.applied}
                        </span>
                      )}
                  </div>
                </div>
              )}

            {/* Tool catalog */}
            {catalogGroup && catalogGroup.tools.length > 0 && (
              <div className={sectionCls}>
                <h4 className="text-sm font-medium text-[var(--c-text-heading)]">
                  {tt.toolDescriptions}
                </h4>
                <div className="mt-3 space-y-2">
                  {catalogGroup.tools.map((tool) => (
                    <ToolRow
                      key={tool.name}
                      tool={tool}
                      tt={tt}
                      onEdit={() => {
                        setDescEdit({
                          toolName: tool.name,
                          label: tool.label,
                          description: tool.llm_description,
                        })
                        setDescText(tool.llm_description)
                      }}
                      onReset={() => handleResetDescription(tool.name)}
                      onToggleDisabled={() => handleToggleToolDisabled(tool)}
                      togglingDisabled={toolToggling === tool.name}
                    />
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* ---- Credential Modal ---- */}
      {credentialModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div
            className="w-full max-w-md rounded-xl bg-[var(--c-bg-menu)] p-6 shadow-xl"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <div className="mb-4 flex items-center justify-between">
              <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">
                {tt.editCredentials}: {credentialModal.provider.provider_name}
              </h4>
              <button onClick={closeCredentialModal} className={btnIcon}>
                <X size={16} />
              </button>
            </div>

            {credentialModal.provider.requires_api_key && (
              <div className="mb-3">
                <label className={labelCls}>{tt.apiKey}</label>
                <div className="relative">
                  <input
                    type={showApiKey ? 'text' : 'password'}
                    className={inputCls}
                    placeholder={
                      credentialModal.provider.key_prefix || tt.apiKeyPlaceholder
                    }
                    value={credentialForm.apiKey}
                    onChange={(e) =>
                      setCredentialForm((f) => ({
                        ...f,
                        apiKey: e.target.value,
                      }))
                    }
                  />
                  <button
                    type="button"
                    onClick={() => setShowApiKey(!showApiKey)}
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]"
                  >
                    {showApiKey ? <EyeOff size={14} /> : <Eye size={14} />}
                  </button>
                </div>
                {credentialModal.provider.key_prefix && (
                  <p className="mt-1 text-xs text-[var(--c-text-muted)]">
                    Current: {credentialModal.provider.key_prefix}…
                  </p>
                )}
              </div>
            )}

            {(credentialModal.provider.requires_base_url ||
              credentialModal.provider.base_url != null) && (
              <div className="mb-3">
                <label className={labelCls}>{tt.baseUrl}</label>
                <input
                  type="text"
                  className={inputCls}
                  placeholder={tt.baseUrlPlaceholder}
                  value={credentialForm.baseUrl}
                  onChange={(e) =>
                    setCredentialForm((f) => ({
                      ...f,
                      baseUrl: e.target.value,
                    }))
                  }
                />
              </div>
            )}

            <div className="mt-4 flex justify-end gap-2">
              <button
                onClick={closeCredentialModal}
                disabled={credSaving}
                className="rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-50"
              >
                {tt.cancel}
              </button>
              <button
                onClick={handleSaveCredential}
                disabled={credSaving}
                className="rounded-md bg-[var(--c-bg-deep)] px-3 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:opacity-80 disabled:opacity-50"
              >
                {credSaving ? tt.saving : tt.save}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ---- Clear Credential Confirm ---- */}
      {clearTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div
            className="w-full max-w-sm rounded-xl bg-[var(--c-bg-menu)] p-6 shadow-xl"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <p className="text-sm text-[var(--c-text-primary)]">
              {tt.clearCredentialsConfirm}
            </p>
            <div className="mt-4 flex justify-end gap-2">
              <button
                onClick={() => setClearTarget(null)}
                disabled={clearing}
                className="rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-50"
              >
                {tt.cancel}
              </button>
              <button
                onClick={handleClearCredential}
                disabled={clearing}
                className="rounded-md bg-red-600/20 px-3 py-1.5 text-sm font-medium text-red-400 transition-colors hover:bg-red-600/30 disabled:opacity-50"
              >
                {clearing ? (
                  <Loader2 size={14} className="inline animate-spin" />
                ) : (
                  tt.clearCredentials
                )}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* ---- Description Edit Modal ---- */}
      {descEdit && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <div
            className="w-full max-w-lg rounded-xl bg-[var(--c-bg-menu)] p-6 shadow-xl"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            <div className="mb-4 flex items-center justify-between">
              <h4 className="text-sm font-semibold text-[var(--c-text-heading)]">
                {tt.editDescription}: {descEdit.label}
              </h4>
              <button
                onClick={() => {
                  if (!descSaving) setDescEdit(null)
                }}
                className={btnIcon}
              >
                <X size={16} />
              </button>
            </div>
            <textarea
              value={descText}
              onChange={(e) => setDescText(e.target.value)}
              rows={8}
              className={`${inputCls} resize-y`}
            />
            <div className="mt-4 flex justify-end gap-2">
              <button
                onClick={() => setDescEdit(null)}
                disabled={descSaving}
                className="rounded-md border border-[var(--c-border-subtle)] px-3 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-50"
              >
                {tt.cancel}
              </button>
              <button
                onClick={handleSaveDescription}
                disabled={descSaving || !descText.trim()}
                className="rounded-md bg-[var(--c-bg-deep)] px-3 py-1.5 text-sm font-medium text-[var(--c-text-primary)] transition-colors hover:opacity-80 disabled:opacity-50"
              >
                {descSaving ? tt.saving : tt.save}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function StatusBadge({
  provider,
  tt,
}: {
  provider: ToolProviderItem
  tt: { configured: string; unconfigured: string; activate: string; deactivate: string }
}) {
  if (provider.is_active && provider.configured) {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-green-500/10 px-1.5 py-0.5 text-[10px] font-medium text-green-400">
        <span className="inline-block h-1.5 w-1.5 rounded-full bg-green-400" />
        Active
      </span>
    )
  }
  if (provider.is_active && !provider.configured) {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-yellow-500/10 px-1.5 py-0.5 text-[10px] font-medium text-yellow-400">
        <span className="inline-block h-1.5 w-1.5 rounded-full bg-yellow-400" />
        {tt.unconfigured}
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-[var(--c-bg-deep)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-text-muted)]">
      <span className="inline-block h-1.5 w-1.5 rounded-full bg-[var(--c-text-muted)]" />
      Inactive
    </span>
  )
}

function ToolRow({
  tool,
  tt,
  onEdit,
  onReset,
  onToggleDisabled,
  togglingDisabled,
}: {
  tool: ToolCatalogItem
  tt: {
    enableTool: string
    disableTool: string
    editDescription: string
    resetDescription: string
    descriptionOverride: string
  }
  onEdit: () => void
  onReset: () => void
  onToggleDisabled: () => void
  togglingDisabled: boolean
}) {
  return (
    <div className="flex items-start justify-between gap-2 rounded-lg border border-[var(--c-border-subtle)] px-3 py-2">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-xs font-medium text-[var(--c-text-primary)]">
            {tool.label}
          </p>
          {tool.has_override && (
            <span className="rounded-full bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-medium text-blue-400">
              {tt.descriptionOverride}
            </span>
          )}
          {tool.is_disabled && (
            <span className="rounded-full bg-yellow-500/10 px-1.5 py-0.5 text-[10px] font-medium text-yellow-400">
              Disabled
            </span>
          )}
        </div>
        <p className="mt-0.5 font-mono text-[10px] text-[var(--c-text-muted)]">
          {tool.name}
        </p>
        <p className="mt-1 line-clamp-2 text-xs text-[var(--c-text-muted)]">
          {tool.llm_description}
        </p>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <button
          onClick={onToggleDisabled}
          disabled={togglingDisabled}
          className={btnIcon}
          title={tool.is_disabled ? tt.enableTool : tt.disableTool}
        >
          {togglingDisabled ? (
            <Loader2 size={12} className="animate-spin" />
          ) : (
            <Ban size={12} />
          )}
        </button>
        <button onClick={onEdit} className={btnIcon} title={tt.editDescription}>
          <Pencil size={12} />
        </button>
        {tool.has_override && (
          <button
            onClick={onReset}
            className={btnIcon}
            title={tt.resetDescription}
          >
            <RotateCcw size={12} />
          </button>
        )}
      </div>
    </div>
  )
}
