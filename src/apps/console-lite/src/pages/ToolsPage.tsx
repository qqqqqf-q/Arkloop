import { useState, useCallback, useEffect } from 'react'
import { useOutletContext, useSearchParams } from 'react-router-dom'
import {
  Loader2, Save, CheckCircle2, Ban, Pencil, Trash2, RotateCcw,
} from 'lucide-react'
import type { LiteOutletContext } from '../layouts/LiteLayout'
import { PageHeader } from '../components/PageHeader'
import { Modal } from '../components/Modal'
import { FormField } from '../components/FormField'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { useToast } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import {
  loadToolProvidersAndCatalog,
  activateToolProvider,
  deactivateToolProvider,
  updateToolProviderCredential,
  clearToolProviderCredential,
  updateToolProviderConfig,
  updateToolDescription,
  deleteToolDescription,
  updateToolDisabled,
  type ToolProviderGroup,
  type ToolProviderItem,
  type ToolCatalogGroup,
  type ToolCatalogItem,
} from '../api/tool-providers'
import { listPlatformSettings, updatePlatformSetting } from '../api/settings'
import { notifyToolCatalogChanged } from '../lib/toolCatalogRefresh'
import { bridgeClient } from '../api/bridge'

const SANDBOX_DEFAULTS: Record<string, string> = {
  allow_egress: 'true',
  image: 'arkloop/sandbox-agent:latest',
  max_sessions: '50',
  agent_port: '8080',
  boot_timeout_s: '30',
  'pool.lite': '3',
  'pool.pro': '2',
  'refill.interval_s': '5',
  'refill.concurrency': '2',
  'timeout.idle_lite_s': '180',
  'timeout.idle_pro_s': '300',
  'timeout.max_lifetime_s': '1800',
}

const SANDBOX_BROWSER_SETTINGS: Record<string, string> = {
  browser_enabled: 'browser.enabled',
  browser_image: 'sandbox.browser_docker_image',
  warm_browser: 'sandbox.warm_browser',
  idle_browser: 'sandbox.idle_timeout_browser_s',
  max_lifetime_browser: 'sandbox.max_lifetime_browser_s',
}

const SANDBOX_BROWSER_DEFAULTS: Record<string, string> = {
  browser_enabled: 'false',
  browser_image: 'arkloop/sandbox-browser:dev',
  warm_browser: '1',
  idle_browser: '120',
  max_lifetime_browser: '600',
}

const MEMORY_DEFAULTS: Record<string, string> = {
  'embedding.provider': 'openai',
  'embedding.model': '',
  'embedding.api_key': '',
  'embedding.api_base': '',
  'embedding.dimension': '1024',
  'vlm.provider': 'litellm',
  'vlm.model': '',
  'vlm.api_key': '',
  'vlm.api_base': '',
  cost_per_commit: '0',
}

type EditTarget = { group: string; provider: ToolProviderItem }
type DescEditTarget = { toolName: string; label: string; description: string }

function displayGroupName(group: string): string {
  return group === 'sandbox' ? 'sandbox/browser' : group
}

function flatGet(obj: Record<string, unknown>, dotPath: string): string {
  const parts = dotPath.split('.')
  let cur: unknown = obj
  for (const p of parts) {
    if (cur == null || typeof cur !== 'object') return ''
    cur = (cur as Record<string, unknown>)[p]
  }
  return cur != null ? String(cur) : ''
}

function flatSet(obj: Record<string, unknown>, dotPath: string, value: string): Record<string, unknown> {
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

export function ToolsPage() {
  const { accessToken } = useOutletContext<LiteOutletContext>()
  const [searchParams, setSearchParams] = useSearchParams()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.tools
  const groupParam = searchParams.get('group')?.trim() ?? ''

  const [providerGroups, setProviderGroups] = useState<ToolProviderGroup[]>([])
  const [catalogGroups, setCatalogGroups] = useState<ToolCatalogGroup[]>([])
  const [selectedGroup, setSelectedGroup] = useState<string>(groupParam)
  const [loading, setLoading] = useState(true)
  const [mutating, setMutating] = useState(false)

  const [editTarget, setEditTarget] = useState<EditTarget | null>(null)
  const [apiKey, setApiKey] = useState('')
  const [baseURL, setBaseURL] = useState('')
  const [editError, setEditError] = useState('')
  const [saving, setSaving] = useState(false)

  const [clearTarget, setClearTarget] = useState<EditTarget | null>(null)
  const [clearing, setClearing] = useState(false)

  const [configForm, setConfigForm] = useState<Record<string, string>>({})
  const [configSaved, setConfigSaved] = useState<Record<string, string>>({})
  const [configSaving, setConfigSaving] = useState(false)

  const [descEdit, setDescEdit] = useState<DescEditTarget | null>(null)
  const [descText, setDescText] = useState('')
  const [descSaving, setDescSaving] = useState(false)
  const [toolToggling, setToolToggling] = useState('')


  const fetchAll = useCallback(async () => {
    setLoading(true)
    try {
      const data = await loadToolProvidersAndCatalog(accessToken)
      setProviderGroups(data.providerGroups)
      setCatalogGroups(data.catalogGroups)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => { void fetchAll() }, [fetchAll])

  const syncGroupParam = useCallback((group: string, replace = false) => {
    const next = new URLSearchParams(searchParams)
    if (group) {
      next.set('group', group)
    } else {
      next.delete('group')
    }
    setSearchParams(next, { replace })
  }, [searchParams, setSearchParams])

  useEffect(() => {
    if (catalogGroups.length === 0) {
      if (groupParam) {
        syncGroupParam('', true)
      }
      setSelectedGroup('')
      return
    }

    if (catalogGroups.some((group) => group.group === groupParam)) {
      if (selectedGroup !== groupParam) {
        setSelectedGroup(groupParam)
      }
      return
    }

    const fallbackGroup = catalogGroups.some((group) => group.group === selectedGroup)
      ? selectedGroup
      : catalogGroups[0].group

    if (selectedGroup !== fallbackGroup) {
      setSelectedGroup(fallbackGroup)
      return
    }
    if (groupParam !== fallbackGroup) {
      syncGroupParam(fallbackGroup, true)
    }
  }, [catalogGroups, groupParam, selectedGroup, syncGroupParam])

  useEffect(() => {
    let cancelled = false

    const loadConfig = async () => {
      const grp = providerGroups.find((g) => g.group_name === selectedGroup)
      if (!grp) {
        if (!cancelled) {
          setConfigForm({})
          setConfigSaved({})
        }
        return
      }
      const active = grp.providers.find((p) => p.is_active)
      if (!active) {
        if (!cancelled) {
          setConfigForm({})
          setConfigSaved({})
        }
        return
      }
      const cfg = active.config_json ?? {}
      const defaults = selectedGroup === 'sandbox'
        ? { ...SANDBOX_DEFAULTS, ...SANDBOX_BROWSER_DEFAULTS }
        : selectedGroup === 'memory'
          ? MEMORY_DEFAULTS
          : {}
      const form: Record<string, string> = {}
      for (const [k, def] of Object.entries(defaults)) {
        if (selectedGroup === 'sandbox' && k in SANDBOX_BROWSER_SETTINGS) {
          form[k] = def
          continue
        }
        form[k] = flatGet(cfg, k) || def
      }
      if (selectedGroup === 'sandbox') {
        const settings = await listPlatformSettings(accessToken)
        for (const [field, key] of Object.entries(SANDBOX_BROWSER_SETTINGS)) {
          const matched = settings.find((item) => item.key === key)
          if (matched && matched.value !== '') {
            form[field] = matched.value
          }
        }
      }
      if (!cancelled) {
        setConfigForm(form)
        setConfigSaved({ ...form })
      }
    }

    void loadConfig()
    return () => { cancelled = true }
  }, [selectedGroup, providerGroups, accessToken])

  const activeGroup = providerGroups.find((g) => g.group_name === selectedGroup)
  const activeProvider = activeGroup?.providers.find((p) => p.is_active)
  const catalogGroup = catalogGroups.find((g) => g.group === selectedGroup)
  const hasConfig = selectedGroup === 'sandbox' || selectedGroup === 'memory'
  const configDirty = hasConfig && Object.keys(configForm).some((k) => configForm[k] !== configSaved[k])

  const handleActivate = useCallback(async (groupName: string, providerName: string) => {
    if (mutating) return
    setMutating(true)
    try {
      await activateToolProvider(groupName, providerName, accessToken)
      addToast(tc.toastUpdated, 'success')
      notifyToolCatalogChanged()
      await fetchAll()
    } catch {
      addToast(tc.toastUpdateFailed, 'error')
    } finally {
      setMutating(false)
    }
  }, [mutating, accessToken, fetchAll, addToast, tc])

  const handleDeactivate = useCallback(async (groupName: string, providerName: string) => {
    if (mutating) return
    setMutating(true)
    try {
      await deactivateToolProvider(groupName, providerName, accessToken)
      addToast(tc.toastUpdated, 'success')
      notifyToolCatalogChanged()
      await fetchAll()
    } catch {
      addToast(tc.toastUpdateFailed, 'error')
    } finally {
      setMutating(false)
    }
  }, [mutating, accessToken, fetchAll, addToast, tc])

  const openEdit = useCallback((group: string, provider: ToolProviderItem) => {
    setEditTarget({ group, provider })
    setApiKey('')
    setBaseURL(provider.base_url ?? '')
    setEditError('')
  }, [])

  const closeEdit = useCallback(() => {
    if (saving) return
    setEditTarget(null)
  }, [saving])

  const handleSaveCredential = useCallback(async () => {
    if (!editTarget) return
    const trimmedKey = apiKey.trim()
    const trimmedBase = baseURL.trim()
    if (editTarget.provider.requires_api_key && !trimmedKey && !editTarget.provider.key_prefix) {
      setEditError(tc.errApiKeyRequired)
      return
    }
    if (editTarget.provider.requires_base_url && !trimmedBase) {
      setEditError(tc.errBaseUrlRequired)
      return
    }
    const payload: Record<string, string> = {}
    if (trimmedKey) payload.api_key = trimmedKey
    if (trimmedBase) payload.base_url = trimmedBase
    if (Object.keys(payload).length === 0) { setEditTarget(null); return }

    setSaving(true)
    setEditError('')
    try {
      await updateToolProviderCredential(editTarget.group, editTarget.provider.provider_name, payload, accessToken)
      addToast(tc.toastUpdated, 'success')
      setEditTarget(null)
      notifyToolCatalogChanged()
      await fetchAll()
    } catch {
      addToast(tc.toastUpdateFailed, 'error')
    } finally {
      setSaving(false)
    }
  }, [editTarget, apiKey, baseURL, accessToken, fetchAll, addToast, tc])

  const handleClear = useCallback(async () => {
    if (!clearTarget) return
    setClearing(true)
    try {
      await clearToolProviderCredential(clearTarget.group, clearTarget.provider.provider_name, accessToken)
      addToast(tc.toastUpdated, 'success')
      setClearTarget(null)
      notifyToolCatalogChanged()
      await fetchAll()
    } catch {
      addToast(tc.toastUpdateFailed, 'error')
    } finally {
      setClearing(false)
    }
  }, [clearTarget, accessToken, fetchAll, addToast, tc])

  const handleSaveConfig = useCallback(async () => {
    if (!activeProvider || !hasConfig) return
    setConfigSaving(true)
    try {
      let configJSON: Record<string, unknown> = {}
      for (const [k, v] of Object.entries(configForm)) {
        if (selectedGroup === 'sandbox' && k in SANDBOX_BROWSER_SETTINGS) continue
        configJSON = flatSet(configJSON, k, v)
      }
      await updateToolProviderConfig(selectedGroup, activeProvider.provider_name, configJSON, accessToken)
      if (selectedGroup === 'sandbox') {
        await Promise.all(
          Object.entries(SANDBOX_BROWSER_SETTINGS)
            .filter(([field]) => configForm[field] !== configSaved[field])
            .map(([field, key]) => updatePlatformSetting(key, configForm[field].trim(), accessToken)),
        )
      }
      setConfigSaved({ ...configForm })
      addToast(tc.toastSaved, 'success')
      notifyToolCatalogChanged()
    } catch {
      addToast(tc.toastSaveFailed, 'error')
    } finally {
      setConfigSaving(false)
    }
  }, [activeProvider, hasConfig, configForm, selectedGroup, accessToken, addToast, tc])

  const handleSaveDescription = useCallback(async () => {
    if (!descEdit) return
    setDescSaving(true)
    try {
      await updateToolDescription(descEdit.toolName, descText, accessToken)
      addToast(tc.toastUpdated, 'success')
      setDescEdit(null)
      notifyToolCatalogChanged()
      await fetchAll()
    } catch {
      addToast(tc.toastUpdateFailed, 'error')
    } finally {
      setDescSaving(false)
    }
  }, [descEdit, descText, accessToken, fetchAll, addToast, tc])

  const handleToggleToolDisabled = useCallback(async (tool: ToolCatalogItem) => {
    if (toolToggling) return
    setToolToggling(tool.name)
    try {
      await updateToolDisabled(tool.name, !tool.is_disabled, accessToken)
      addToast(tc.toastUpdated, 'success')
      notifyToolCatalogChanged()
      await fetchAll()
    } catch {
      addToast(tc.toastUpdateFailed, 'error')
    } finally {
      setToolToggling('')
    }
  }, [toolToggling, accessToken, fetchAll, addToast, tc])

  const handleResetDescription = useCallback(async (toolName: string) => {
    try {
      await deleteToolDescription(toolName, accessToken)
      addToast(tc.toastUpdated, 'success')
      notifyToolCatalogChanged()
      await fetchAll()
    } catch {
      addToast(tc.toastUpdateFailed, 'error')
    }
  }, [accessToken, fetchAll, addToast, tc])

  const inputCls =
    'w-full rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-border-focus)]'
  const labelCls = 'mb-1 block text-xs font-medium text-[var(--c-text-secondary)]'
  const sectionCls = 'rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5'
  const editInputCls =
    'rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none'

  const setConfig = (key: string) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    setConfigForm((prev) => ({ ...prev, [key]: e.target.value }))

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} />

      {loading ? (
        <div className="flex flex-1 items-center justify-center">
          <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
        </div>
      ) : (
        <div className="flex flex-1 overflow-hidden">
          {/* Left: group list */}
          <div className="w-[160px] shrink-0 overflow-y-auto border-r border-[var(--c-border-console)] p-2">
            <div className="flex flex-col gap-[3px]">
              {catalogGroups.map((g) => {
                const active = g.group === selectedGroup
                return (
                  <button
                    key={g.group}
                    onClick={() => {
                      setSelectedGroup(g.group)
                      syncGroupParam(g.group)
                    }}
                    className={[
                      'flex h-[30px] items-center rounded-[5px] px-3 text-sm font-medium transition-colors',
                      active
                        ? 'bg-[var(--c-bg-sub)] text-[var(--c-text-primary)]'
                        : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]',
                    ].join(' ')}
                  >
                    {displayGroupName(g.group)}
                  </button>
                )
              })}
            </div>
          </div>

          {/* Right: detail panel */}
          <div className="flex-1 overflow-y-auto p-6">
            <div className="mx-auto max-w-xl space-y-6">
              {/* Provider section */}
              {activeGroup && (
                <div className={sectionCls}>
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.sectionProvider}</h3>
                  <div className="mt-4 space-y-3">
                    {activeGroup.providers.map((p) => (
                      <div
                        key={p.provider_name}
                        className="flex items-center justify-between rounded-md border border-[var(--c-border-console)] px-3 py-2"
                      >
                        <div className="flex items-center gap-3">
                          <span className="font-mono text-xs text-[var(--c-text-primary)]">{p.provider_name}</span>
                          <div className="flex items-center gap-1.5">
                            {p.is_active ? (
                              <span className="rounded-full bg-[var(--c-status-success-bg)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-status-success-text)]">{tc.statusActive}</span>
                            ) : (
                              <span className="rounded-full bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-text-muted)]">{tc.statusInactive}</span>
                            )}
                            {p.is_active && !p.configured && (
                              <span className="rounded-full bg-[var(--c-status-warning-bg)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-status-warning-text)]">{tc.statusUnconfigured}</span>
                            )}
                          </div>
                        </div>
                        <div className="flex items-center gap-1">
                          {!p.is_active ? (
                            <button
                              disabled={mutating}
                              onClick={() => handleActivate(p.group_name, p.provider_name)}
                              className="rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-status-success-text)] disabled:opacity-40"
                              title={tc.activate}
                            >
                              <CheckCircle2 size={14} />
                            </button>
                          ) : (
                            <button
                              disabled={mutating}
                              onClick={() => handleDeactivate(p.group_name, p.provider_name)}
                              className="rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)] disabled:opacity-40"
                              title={tc.deactivate}
                            >
                              <Ban size={14} />
                            </button>
                          )}
                          <button
                            disabled={mutating}
                            onClick={() => openEdit(activeGroup.group_name, p)}
                            className="rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)] disabled:opacity-40"
                            title={tc.configure}
                          >
                            <Pencil size={14} />
                          </button>
                          <button
                            disabled={mutating}
                            onClick={() => setClearTarget({ group: activeGroup.group_name, provider: p })}
                            className="rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-status-error-text)] disabled:opacity-40"
                            title={tc.clearCredential}
                          >
                            <Trash2 size={14} />
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Sandbox config */}
              {selectedGroup === 'sandbox' && activeProvider && (
                <SandboxConfigSection
                  form={configForm}
                  onChange={setConfig}
                  inputCls={inputCls}
                  labelCls={labelCls}
                  sectionCls={sectionCls}
                  tc={tc}
                  providerName={activeProvider.provider_name}
                />
              )}

              {/* Memory config */}
              {selectedGroup === 'memory' && activeProvider && (
                <MemoryConfigSection
                  form={configForm}
                  onChange={setConfig}
                  onSave={handleSaveConfig}
                  configSaving={configSaving}
                  configDirty={configDirty}
                  inputCls={inputCls}
                  labelCls={labelCls}
                  sectionCls={sectionCls}
                  tc={tc}
                />
              )}

              {/* Config save (non-memory groups only — memory has its own merged save+apply) */}
              {hasConfig && activeProvider && selectedGroup !== 'memory' && (
                <div className="border-t border-[var(--c-border-console)] pt-4">
                  <button
                    onClick={handleSaveConfig}
                    disabled={configSaving || !configDirty}
                    className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
                  >
                    {configSaving ? <Loader2 size={12} className="animate-spin" /> : <Save size={12} />}
                    {tc.save}
                  </button>
                </div>
              )}

              {/* Tool descriptions */}
              {catalogGroup && catalogGroup.tools.length > 0 && (
                <div className={sectionCls}>
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.sectionToolDescriptions}</h3>
                  <div className="mt-4 space-y-3">
                    {catalogGroup.tools.map((tool) => (
                      <ToolDescriptionRow
                        key={tool.name}
                        tool={tool}
                        tc={tc}
                        onEdit={() => {
                          setDescEdit({ toolName: tool.name, label: tool.label, description: tool.llm_description })
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
      )}

      {/* Credential edit modal */}
      <Modal
        open={!!editTarget}
        onClose={closeEdit}
        title={editTarget ? `${tc.modalTitle}: ${editTarget.provider.provider_name}` : tc.modalTitle}
      >
        {editTarget && (
          <div className="flex flex-col gap-4">
            {editTarget.provider.requires_api_key && (
              <div className="flex flex-col gap-2">
                <FormField label={tc.fieldApiKey} error={editError}>
                  <input
                    type="password"
                    value={apiKey}
                    onChange={(e) => { setApiKey(e.target.value); setEditError('') }}
                    className={editInputCls}
                    placeholder={editTarget.provider.key_prefix ?? ''}
                  />
                </FormField>
                {editTarget.provider.key_prefix && (
                  <p className="text-xs text-[var(--c-text-muted)]">
                    {tc.currentKeyPrefix}: {editTarget.provider.key_prefix}
                  </p>
                )}
              </div>
            )}
            {editTarget.provider.requires_base_url ? (
              <FormField label={tc.fieldBaseUrl} error={editError}>
                <input
                  type="text"
                  value={baseURL}
                  onChange={(e) => { setBaseURL(e.target.value); setEditError('') }}
                  className={editInputCls}
                />
              </FormField>
            ) : (
              <FormField label={tc.fieldBaseUrlOptional} error={editError}>
                <input
                  type="text"
                  value={baseURL}
                  onChange={(e) => { setBaseURL(e.target.value); setEditError('') }}
                  className={editInputCls}
                  placeholder={editTarget.provider.base_url ?? ''}
                />
              </FormField>
            )}
            {editError && <p className="text-xs text-[var(--c-status-error-text)]">{editError}</p>}
            <div className="mt-2 flex justify-end gap-2">
              <button
                onClick={closeEdit}
                disabled={saving}
                className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              >
                {tc.cancel}
              </button>
              <button
                onClick={handleSaveCredential}
                disabled={saving}
                className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              >
                {saving ? '...' : tc.save}
              </button>
            </div>
          </div>
        )}
      </Modal>

      {/* Clear credential dialog */}
      <ConfirmDialog
        open={!!clearTarget}
        onClose={() => setClearTarget(null)}
        onConfirm={handleClear}
        title={tc.clearTitle}
        message={clearTarget ? tc.clearMessage(clearTarget.provider.provider_name) : ''}
        confirmLabel={tc.clearConfirm}
        loading={clearing}
      />

      {/* Description edit modal */}
      <Modal
        open={!!descEdit}
        onClose={() => { if (!descSaving) setDescEdit(null) }}
        title={descEdit ? descEdit.label : ''}
      >
        {descEdit && (
          <div className="flex flex-col gap-4">
            <textarea
              value={descText}
              onChange={(e) => setDescText(e.target.value)}
              rows={12}
              className="w-full rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-2 text-sm text-[var(--c-text-primary)] placeholder:text-[var(--c-text-muted)] focus:outline-none"
              placeholder={tc.descriptionPlaceholder}
            />
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setDescEdit(null)}
                disabled={descSaving}
                className="rounded-lg border border-[var(--c-border)] px-3.5 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              >
                {tc.cancel}
              </button>
              <button
                onClick={handleSaveDescription}
                disabled={descSaving || !descText.trim()}
                className="rounded-lg bg-[var(--c-bg-tag)] px-3.5 py-1.5 text-sm font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
              >
                {descSaving ? '...' : tc.save}
              </button>
            </div>
          </div>
        )}
      </Modal>
    </div>
  )
}

function SandboxConfigSection({
  form, onChange, inputCls, labelCls, sectionCls, tc, providerName,
}: {
  form: Record<string, string>
  onChange: (key: string) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => void
  inputCls: string
  labelCls: string
  sectionCls: string
  tc: {
    sectionConfig: string
    sectionPool: string
    sectionTimeout: string
    sectionBrowser: string
    fieldAllowEgress: string
    fieldDockerImage: string
    fieldMaxSessions: string
    fieldBootTimeout: string
    fieldRefillInterval: string
    fieldRefillConcurrency: string
    fieldMaxLifetime: string
    fieldBrowserEnabled: string
    fieldBrowserDockerImage: string
    fieldWarmBrowser: string
    fieldIdleBrowser: string
    fieldMaxLifetimeBrowser: string
  }
  providerName: string
}) {
  const isDocker = providerName.includes('docker')

  return (
    <>
      <div className={sectionCls}>
        <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.sectionConfig}</h3>
        <div className="mt-4 space-y-4">
          <div>
            <label className={labelCls}>{tc.fieldAllowEgress}</label>
            <select value={form.allow_egress ?? 'true'} onChange={onChange('allow_egress')} className={inputCls}>
              <option value="true">true</option>
              <option value="false">false</option>
            </select>
          </div>
          {isDocker && (
            <div>
              <label className={labelCls}>{tc.fieldDockerImage}</label>
              <input type="text" className={inputCls} value={form.image ?? ''} onChange={onChange('image')} />
            </div>
          )}
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className={labelCls}>{tc.fieldMaxSessions}</label>
              <input type="number" min={1} className={inputCls} value={form.max_sessions ?? ''} onChange={onChange('max_sessions')} />
            </div>
            <div>
              <label className={labelCls}>{tc.fieldBootTimeout}</label>
              <input type="number" min={1} className={inputCls} value={form.boot_timeout_s ?? ''} onChange={onChange('boot_timeout_s')} />
            </div>
          </div>
        </div>
      </div>

      <div className={sectionCls}>
        <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.sectionPool}</h3>
        <div className="mt-4 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className={labelCls}>Lite</label>
              <input type="number" min={0} className={inputCls} value={form['pool.lite'] ?? ''} onChange={onChange('pool.lite')} />
            </div>
            <div>
              <label className={labelCls}>Pro</label>
              <input type="number" min={0} className={inputCls} value={form['pool.pro'] ?? ''} onChange={onChange('pool.pro')} />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className={labelCls}>{tc.fieldRefillInterval}</label>
              <input type="number" min={1} className={inputCls} value={form['refill.interval_s'] ?? ''} onChange={onChange('refill.interval_s')} />
            </div>
            <div>
              <label className={labelCls}>{tc.fieldRefillConcurrency}</label>
              <input type="number" min={1} className={inputCls} value={form['refill.concurrency'] ?? ''} onChange={onChange('refill.concurrency')} />
            </div>
          </div>
        </div>
      </div>

      <div className={sectionCls}>
        <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.sectionTimeout}</h3>
        <div className="mt-4 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className={labelCls}>Lite (s)</label>
              <input type="number" min={1} className={inputCls} value={form['timeout.idle_lite_s'] ?? ''} onChange={onChange('timeout.idle_lite_s')} />
            </div>
            <div>
              <label className={labelCls}>Pro (s)</label>
              <input type="number" min={1} className={inputCls} value={form['timeout.idle_pro_s'] ?? ''} onChange={onChange('timeout.idle_pro_s')} />
            </div>
          </div>
          <div>
            <label className={labelCls}>{tc.fieldMaxLifetime}</label>
            <input type="number" min={1} className={inputCls} value={form['timeout.max_lifetime_s'] ?? ''} onChange={onChange('timeout.max_lifetime_s')} />
          </div>
        </div>
      </div>

      <div className={sectionCls}>
        <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.sectionBrowser}</h3>
        <div className="mt-4 space-y-4">
          <div>
            <label className={labelCls}>{tc.fieldBrowserEnabled}</label>
            <select value={form.browser_enabled ?? 'false'} onChange={onChange('browser_enabled')} className={inputCls}>
              <option value="true">true</option>
              <option value="false">false</option>
            </select>
          </div>
          {isDocker && (
            <div>
              <label className={labelCls}>{tc.fieldBrowserDockerImage}</label>
              <input type="text" className={inputCls} value={form.browser_image ?? ''} onChange={onChange('browser_image')} />
            </div>
          )}
          <div className="grid grid-cols-3 gap-4">
            <div>
              <label className={labelCls}>{tc.fieldWarmBrowser}</label>
              <input type="number" min={0} className={inputCls} value={form.warm_browser ?? ''} onChange={onChange('warm_browser')} />
            </div>
            <div>
              <label className={labelCls}>{tc.fieldIdleBrowser}</label>
              <input type="number" min={1} className={inputCls} value={form.idle_browser ?? ''} onChange={onChange('idle_browser')} />
            </div>
            <div>
              <label className={labelCls}>{tc.fieldMaxLifetimeBrowser}</label>
              <input type="number" min={1} className={inputCls} value={form.max_lifetime_browser ?? ''} onChange={onChange('max_lifetime_browser')} />
            </div>
          </div>
        </div>
      </div>
    </>
  )
}

function MemoryConfigSection({
  form, onChange, onSave, configSaving, configDirty, inputCls, labelCls, sectionCls, tc,
}: {
  form: Record<string, string>
  onChange: (key: string) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => void
  onSave: () => Promise<void>
  configSaving: boolean
  configDirty: boolean
  inputCls: string
  labelCls: string
  sectionCls: string
  tc: {
    sectionEmbeddingConfig: string
    fieldEmbeddingProvider: string
    fieldEmbeddingModel: string
    fieldEmbeddingApiKey: string
    fieldEmbeddingApiBase: string
    fieldEmbeddingDimension: string
    sectionVLMConfig: string
    fieldVLMProvider: string
    fieldVLMModel: string
    fieldVLMApiKey: string
    fieldVLMApiBase: string
    fieldVLMProviderHint: string
    sectionConfig: string
    fieldCostPerCommit: string
    fieldCostPerCommitHint: string
    ovRestartWarning: string
    applyAndRestart: string
    toastOVConfigApplied: string
    toastOVConfigFailed: string
  }
}) {
  const { addToast } = useToast()
  const [applying, setApplying] = useState(false)

  const handleApplyConfig = async () => {
    setApplying(true)
    try {
      await onSave()
      await bridgeClient.performAction('openviking', 'configure', {
        embedding_provider: form['embedding.provider'] || 'openai',
        embedding_model: form['embedding.model'] || '',
        embedding_api_key: form['embedding.api_key'] || '',
        embedding_api_base: form['embedding.api_base'] || '',
        embedding_dimension: form['embedding.dimension'] || '1024',
        vlm_provider: form['vlm.provider'] || 'litellm',
        vlm_model: form['vlm.model'] || '',
        vlm_api_key: form['vlm.api_key'] || '',
        vlm_api_base: form['vlm.api_base'] || '',
        root_api_key: '',
      })
      addToast(tc.toastOVConfigApplied, 'success')
    } catch {
      addToast(tc.toastOVConfigFailed, 'error')
    } finally {
      setApplying(false)
    }
  }

  return (
    <>
      {/* Embedding Configuration */}
      <div className={sectionCls}>
        <h3 className="text-sm font-medium text-[var(--c-text-primary)]">
          {tc.sectionEmbeddingConfig}
        </h3>
        <div className="mt-4 space-y-4">
          <div>
            <label className={labelCls}>{tc.fieldEmbeddingProvider}</label>
            <select className={inputCls} value={form['embedding.provider'] ?? 'openai'} onChange={onChange('embedding.provider')}>
              <option value="openai">OpenAI</option>
              <option value="volcengine">Volcengine</option>
              <option value="vikingdb">VikingDB</option>
              <option value="jina">Jina</option>
            </select>
          </div>
          <div>
            <label className={labelCls}>{tc.fieldEmbeddingModel}</label>
            <input type="text" className={inputCls} value={form['embedding.model'] ?? ''} onChange={onChange('embedding.model')} placeholder="e.g. text-embedding-3-small" />
          </div>
          <div>
            <label className={labelCls}>{tc.fieldEmbeddingApiKey}</label>
            <input type="password" className={inputCls} value={form['embedding.api_key'] ?? ''} onChange={onChange('embedding.api_key')} placeholder="sk-..." />
          </div>
          <div>
            <label className={labelCls}>{tc.fieldEmbeddingApiBase}</label>
            <input type="text" className={inputCls} value={form['embedding.api_base'] ?? ''} onChange={onChange('embedding.api_base')} placeholder="https://api.openai.com/v1" />
          </div>
          <div>
            <label className={labelCls}>{tc.fieldEmbeddingDimension}</label>
            <input type="number" className={inputCls} min={1} value={form['embedding.dimension'] ?? '1024'} onChange={onChange('embedding.dimension')} />
          </div>
        </div>
      </div>

      {/* VLM Configuration */}
      <div className={sectionCls}>
        <h3 className="text-sm font-medium text-[var(--c-text-primary)]">
          {tc.sectionVLMConfig}
        </h3>
        <div className="mt-4 space-y-4">
          <div>
            <label className={labelCls}>{tc.fieldVLMProvider}</label>
            <select className={inputCls} value={form['vlm.provider'] ?? 'litellm'} onChange={onChange('vlm.provider')}>
              <option value="litellm">LiteLLM (Universal)</option>
              <option value="openai">OpenAI</option>
              <option value="volcengine">Volcengine</option>
            </select>
            <p className="mt-1 text-xs text-[var(--c-text-muted)]">
              {tc.fieldVLMProviderHint}
            </p>
          </div>
          <div>
            <label className={labelCls}>{tc.fieldVLMModel}</label>
            <input type="text" className={inputCls} value={form['vlm.model'] ?? ''} onChange={onChange('vlm.model')} placeholder="e.g. gpt-4o" />
          </div>
          <div>
            <label className={labelCls}>{tc.fieldVLMApiKey}</label>
            <input type="password" className={inputCls} value={form['vlm.api_key'] ?? ''} onChange={onChange('vlm.api_key')} placeholder="sk-..." />
          </div>
          <div>
            <label className={labelCls}>{tc.fieldVLMApiBase}</label>
            <input type="text" className={inputCls} value={form['vlm.api_base'] ?? ''} onChange={onChange('vlm.api_base')} placeholder="https://api.openai.com/v1" />
          </div>
        </div>
      </div>

      {/* Billing */}
      <div className={sectionCls}>
        <h3 className="text-sm font-medium text-[var(--c-text-primary)]">{tc.sectionConfig}</h3>
        <div className="mt-4">
          <label className={labelCls}>{tc.fieldCostPerCommit}</label>
          <input type="number" min={0} step="0.001" className={inputCls} value={form.cost_per_commit ?? '0'} onChange={onChange('cost_per_commit')} />
          <p className="mt-1 text-xs text-[var(--c-text-muted)]">{tc.fieldCostPerCommitHint}</p>
        </div>
      </div>

      {/* Save & Apply */}
      <div className="border-t border-[var(--c-border-console)] pt-4">
        <p className="mb-3 text-xs text-[var(--c-status-warning-text)]">
          {tc.ovRestartWarning}
        </p>
        <button
          onClick={handleApplyConfig}
          disabled={applying || configSaving}
          className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
        >
          {(applying || configSaving) ? <Loader2 size={12} className="animate-spin" /> : <Save size={12} />}
          {tc.applyAndRestart}
        </button>
      </div>
    </>
  )
}

function ToolDescriptionRow({
  tool, tc, onEdit, onReset, onToggleDisabled, togglingDisabled,
}: {
  tool: ToolCatalogItem
  tc: { editDescription: string; resetDescription: string; disableTool: string; enableTool: string; statusDisabled: string }
  onEdit: () => void
  onReset: () => void
  onToggleDisabled: () => void
  togglingDisabled: boolean
}) {
  return (
    <div className="flex items-start justify-between gap-2 rounded-md border border-[var(--c-border-console)] px-3 py-2">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-xs font-medium text-[var(--c-text-primary)]">{tool.label}</p>
          {tool.is_disabled && (
            <span className="rounded-full bg-[var(--c-status-warning-bg)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--c-status-warning-text)]">{tc.statusDisabled}</span>
          )}
        </div>
        <p className="mt-0.5 font-mono text-[10px] text-[var(--c-text-muted)]">{tool.name}</p>
        <p className="mt-1 line-clamp-3 text-xs text-[var(--c-text-muted)]">{tool.llm_description}</p>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <button
          onClick={onToggleDisabled}
          disabled={togglingDisabled}
          className="rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)] disabled:opacity-40"
          title={tool.is_disabled ? tc.enableTool : tc.disableTool}
        >
          {togglingDisabled ? <Loader2 size={12} className="animate-spin" /> : <Ban size={12} />}
        </button>
        <button
          onClick={onEdit}
          className="rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
          title={tc.editDescription}
        >
          <Pencil size={12} />
        </button>
        {tool.has_override && (
          <button
            onClick={onReset}
            className="rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
            title={tc.resetDescription}
          >
            <RotateCcw size={12} />
          </button>
        )}
      </div>
    </div>
  )
}
