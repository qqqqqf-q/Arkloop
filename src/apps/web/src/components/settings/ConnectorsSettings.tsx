import { useState, useCallback, useEffect, useMemo } from 'react'
import {
  Loader2,
  Shield,
  Key,
  Trash2,
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
import { Modal, ConfirmDialog, useToast } from '@arkloop/shared'
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
  if (group === 'sandbox') return 'sandbox / browser'
  if (group === 'image_understanding') return 'Image understanding'
  return group
}

function mergeGroupTabs(
  catalogGroups: { group: string }[],
  providerGroups: { group_name: string }[],
): string[] {
  const seen = new Set<string>()
  const tabs: string[] = []
  catalogGroups.forEach((catalog) => {
    if (!seen.has(catalog.group)) {
      seen.add(catalog.group)
      tabs.push(catalog.group)
    }
  })
  providerGroups.forEach((provider) => {
    if (!seen.has(provider.group_name)) {
      seen.add(provider.group_name)
      tabs.push(provider.group_name)
    }
  })
  return tabs
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

function flattenConfig(
  obj: Record<string, unknown>,
  prefix = '',
  target: Record<string, string> = {},
): Record<string, string> {
  for (const [key, value] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (value != null && typeof value === 'object' && !Array.isArray(value)) {
      flattenConfig(value as Record<string, unknown>, path, target)
    } else {
      target[path] = value != null ? String(value) : ''
    }
  }
  return target
}

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------

import { settingsInputCls } from './_SettingsInput'
import { settingsLabelCls } from './_SettingsLabel'
import { settingsSectionCls } from './_SettingsSection'

const inputCls = settingsInputCls('sm')
const labelCls = settingsLabelCls('sm')
const sectionCls = settingsSectionCls
const btnIcon =
  'rounded p-1 text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)] disabled:opacity-40'

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export function ConnectorsSettings({ accessToken }: Props) {
  const { t } = useLocale()
  const ds = t.desktopSettings
  const tt = t.adminTools
  const { addToast } = useToast()

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

  const groupTabs = useMemo(() => mergeGroupTabs(catalog, groups), [catalog, groups])

  useEffect(() => {
    if (groupTabs.length === 0) {
      setSelectedGroup('')
      return
    }
    setSelectedGroup((prev) => {
      if (prev && groupTabs.includes(prev)) {
        return prev
      }
      return groupTabs[0]
    })
  }, [groupTabs])

  // ---- Derived state ----

  const activeGroup = groups.find((g) => g.group_name === selectedGroup)
  const activeProvider = activeGroup?.providers.find((provider) => provider.is_active)
  const catalogGroup = catalog.find((g) => g.group === selectedGroup)
  const configFields = activeProvider?.config_fields ?? []
  const configDirty = Object.keys(configForm).some(
    (k) => configForm[k] !== configSaved[k],
  )

  useEffect(() => {
    if (!activeProvider || !activeProvider.config_fields?.length) {
      setConfigForm({})
      setConfigSaved({})
      return
    }
    const fields = activeProvider.config_fields
    const flattened = flattenConfig(activeProvider.config_json ?? {})
    const form: Record<string, string> = {}
    fields.forEach((field) => {
      const baseValue =
        flattened[field.key] ??
        (field.key === 'base_url' && activeProvider.default_base_url
          ? activeProvider.default_base_url
          : undefined) ??
        field.default ??
        ''
      form[field.key] = baseValue
    })
    setConfigForm(form)
    setConfigSaved({ ...form })
  }, [activeProvider])

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
      addToast(tt.save, 'success')
      await fetchAll()
    } catch {
      addToast(tt.saving, 'error')
    } finally {
      setCredSaving(false)
    }
  }, [credentialModal, credentialForm, accessToken, fetchAll, tt.save, tt.saving, addToast])

  const handleCloseCredentialModal = useCallback(async () => {
    if (credSaving) return
    const trimmedKey = credentialForm.apiKey.trim()
    const trimmedBase = credentialForm.baseUrl.trim()
    if (trimmedKey || trimmedBase) {
      await handleSaveCredential()
    } else {
      setCredentialModal(null)
    }
  }, [credSaving, credentialForm, handleSaveCredential])

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
      addToast(tt.save, 'success')
      await fetchAll()
    } catch {
      addToast(tt.saving, 'error')
    } finally {
      setDescSaving(false)
    }
  }, [descEdit, descText, accessToken, fetchAll, tt.save, tt.saving, addToast])

  const handleCloseDescModal = useCallback(async () => {
    if (descSaving) return
    if (descText.trim() && descEdit && descText.trim() !== descEdit.description.trim()) {
      await handleSaveDescription()
    } else {
      setDescEdit(null)
    }
  }, [descSaving, descText, descEdit, handleSaveDescription])

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
            {groupTabs.map((group) => {
              const active = group === selectedGroup
              return (
                <button
                  key={group}
                  onClick={() => setSelectedGroup(group)}
                  className={[
                    'flex h-[30px] items-center rounded-md px-3 text-[13px] font-medium transition-colors',
                    active
                      ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]'
                      : 'text-[var(--c-text-muted)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-secondary)]',
                  ].join(' ')}
                >
                  {displayGroupName(group)}
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
                        <StatusBadge provider={p} />
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

            {/* Configuration fields */}
            {activeProvider && configFields.length > 0 && (
              <div className={sectionCls}>
                <h4 className="flex items-center gap-2 text-sm font-medium text-[var(--c-text-heading)]">
                  <Settings size={14} />
                  {tt.configSection}
                </h4>
                {activeProvider.default_base_url && (
                  <p className="mt-2 text-xs text-[var(--c-text-muted)]">
                    {tt.baseUrl}: {activeProvider.default_base_url}
                  </p>
                )}
                <div className="mt-3 space-y-3">
                  {configFields.map((field) => {
                    const value = configForm[field.key] ?? ''
                    const inputType =
                      field.type === 'password'
                        ? 'password'
                        : field.type === 'number'
                        ? 'number'
                        : 'text'
                    return (
                      <div key={field.key}>
                        <div className="flex items-center justify-between">
                          <label className={labelCls}>
                            {field.label}
                            {field.required && ' *'}
                          </label>
                          {field.group && (
                            <span className="text-[11px] text-[var(--c-text-muted)]">
                              {field.group}
                            </span>
                          )}
                        </div>
                        {field.type === 'select' ? (
                          <select
                            className={inputCls}
                            value={value}
                            onChange={setConfig(field.key)}
                          >
                            {(field.options ?? []).map((option) => (
                              <option key={option} value={option}>
                                {option}
                              </option>
                            ))}
                          </select>
                        ) : (
                          <input
                            type={inputType}
                            inputMode={field.type === 'number' ? 'numeric' : undefined}
                            placeholder={field.placeholder ?? field.default ?? ''}
                            className={inputCls}
                            value={value}
                            onChange={setConfig(field.key)}
                          />
                        )}
                        {field.placeholder && field.type !== 'select' && (
                          <p className="text-[11px] text-[var(--c-text-muted)]">
                            {field.placeholder}
                          </p>
                        )}
                      </div>
                    )
                  })}
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
                  {!configDirty && Object.keys(configSaved).length > 0 && (
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
      <Modal
        open={!!credentialModal}
        onClose={() => void handleCloseCredentialModal()}
        title={credentialModal ? `${tt.editCredentials}: ${credentialModal.provider.provider_name}` : ''}
        width="440px"
      >
        {credentialModal && (
          <div className="flex flex-col gap-3">
            {credentialModal.provider.requires_api_key && (
              <div>
                <label className={labelCls}>{tt.apiKey}</label>
                <div className="relative">
                  <input
                    type={showApiKey ? 'text' : 'password'}
                    className={inputCls}
                    placeholder={credentialModal.provider.key_prefix || tt.apiKeyPlaceholder}
                    value={credentialForm.apiKey}
                    onChange={(e) => setCredentialForm((f) => ({ ...f, apiKey: e.target.value }))}
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
            {(credentialModal.provider.requires_base_url || credentialModal.provider.base_url != null) && (
              <div>
                <label className={labelCls}>{tt.baseUrl}</label>
                <input
                  type="text"
                  className={inputCls}
                  placeholder={tt.baseUrlPlaceholder}
                  value={credentialForm.baseUrl}
                  onChange={(e) => setCredentialForm((f) => ({ ...f, baseUrl: e.target.value }))}
                />
              </div>
            )}
            {credSaving && (
              <div className="flex items-center gap-1.5 text-xs text-[var(--c-text-muted)]">
                <Loader2 size={12} className="animate-spin" />
                {tt.saving}
              </div>
            )}
          </div>
        )}
      </Modal>

      {/* ---- Clear Credential Confirm ---- */}
      <ConfirmDialog
        open={!!clearTarget}
        onClose={() => setClearTarget(null)}
        onConfirm={handleClearCredential}
        message={tt.clearCredentialsConfirm}
        confirmLabel={clearing ? '…' : tt.clearCredentials}
        cancelLabel={tt.cancel}
        loading={clearing}
      />

      {/* ---- Description Edit Modal ---- */}
      <Modal
        open={!!descEdit}
        onClose={() => void handleCloseDescModal()}
        title={descEdit ? `${tt.editDescription}: ${descEdit.label}` : ''}
        width="560px"
      >
        {descEdit && (
          <div className="flex flex-col gap-3">
            <textarea
              value={descText}
              onChange={(e) => setDescText(e.target.value)}
              rows={8}
              className={`${inputCls} resize-y`}
            />
            {descSaving && (
              <div className="flex items-center gap-1.5 text-xs text-[var(--c-text-muted)]">
                <Loader2 size={12} className="animate-spin" />
                {tt.saving}
              </div>
            )}
          </div>
        )}
      </Modal>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function StatusBadge({ provider }: { provider: ToolProviderItem }) {
  const state = provider.runtime_state ?? (provider.is_active ? 'ready' : 'inactive')
  const info = runtimeStateInfo(state)
  const reason = provider.runtime_reason ? formatRuntimeReason(provider.runtime_reason) : ''
  return (
    <span className={`inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${info.bg}`}>
      <span className={`inline-block h-1.5 w-1.5 rounded-full ${info.dot}`} />
      <span className={info.text}>{info.label}</span>
      {reason ? (
        <span className="ml-1 text-[var(--c-text-muted)]">({reason})</span>
      ) : null}
    </span>
  )
}

function runtimeStateInfo(state?: string) {
  const normalized = state ?? 'inactive'
  switch (normalized) {
  case 'ready':
    return { label: 'Ready', bg: 'bg-green-500/10 text-green-400', dot: 'bg-green-400', text: 'text-green-400' }
  case 'missing_config':
    return { label: 'Missing config', bg: 'bg-amber-500/10 text-amber-400', dot: 'bg-amber-400', text: 'text-amber-400' }
  case 'decrypt_failed':
    return { label: 'Decrypt failed', bg: 'bg-rose-500/10 text-rose-400', dot: 'bg-rose-400', text: 'text-rose-400' }
  case 'invalid_config':
    return { label: 'Invalid config', bg: 'bg-rose-500/10 text-rose-400', dot: 'bg-rose-400', text: 'text-rose-400' }
  default:
    return { label: 'Inactive', bg: 'bg-[var(--c-bg-deep)] text-[var(--c-text-muted)]', dot: 'bg-[var(--c-text-muted)]', text: 'text-[var(--c-text-muted)]' }
  }
}

function formatRuntimeReason(reason: string) {
  return reason
    .split('_')
    .map((segment) => segment.charAt(0).toUpperCase() + segment.slice(1))
    .join(' ')
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
