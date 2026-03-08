import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Copy, Check } from 'lucide-react'
import type { LiteOutletContext } from '../layouts/LiteLayout'
import { PageHeader } from '../components/PageHeader'
import { FormField } from '../components/FormField'
import { Modal } from '../components/Modal'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { useToast } from '../components/useToast'
import { useLocale } from '../contexts/LocaleContext'
import { listLlmProviders, type LlmProvider } from '../api/llm-providers'
import {
  getPlatformSetting,
  setPlatformSetting,
  deletePlatformSetting,
  getCreditsMode,
  setCreditsMode,
  listEmailConfigs,
  createEmailConfig,
  updateEmailConfig,
  deleteEmailConfig,
  setDefaultEmailConfig,
  sendTestEmail,
  type EmailConfig,
  type CreateEmailConfigRequest,
} from '../api/settings'

const TITLE_SUMMARIZER_KEY = 'title_summarizer.model'

const SANDBOX_INSTALL_CMD = `docker compose up -d sandbox`
const BROWSER_INSTALL_CMD = `docker compose up -d browser`
const FULL_CONSOLE_URL = '/console'

type SettingsSection = 'general' | 'email' | 'system'

const inputCls =
  'w-full rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] focus:outline-none'
const btnSecCls =
  'rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-40'
const btnPrimCls =
  'rounded-lg bg-[var(--c-bg-tag)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-40'

// ── General 子面板 ─────────────────────────────────────────────────────────

function GeneralPanel({ accessToken }: { accessToken: string }) {
  const { t } = useLocale()
  const tc = t.settingsPage
  const { addToast } = useToast()

  const [providers, setProviders] = useState<LlmProvider[]>([])
  const [selectedModel, setSelectedModel] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [provs, setting] = await Promise.all([
        listLlmProviders(accessToken),
        getPlatformSetting(TITLE_SUMMARIZER_KEY, accessToken),
      ])
      setProviders(provs)
      setSelectedModel(setting?.value ?? '')
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => { void load() }, [load])

  const modelOptions = providers.flatMap((p) =>
    (p.models ?? []).map((m) => ({
      value: `${p.name}^${m.model}`,
      label: `${p.name} · ${m.model}`,
    })),
  )
  // 当前值不在列表中时插入
  if (selectedModel && !modelOptions.some((o) => o.value === selectedModel)) {
    modelOptions.unshift({ value: selectedModel, label: selectedModel })
  }

  const handleSave = useCallback(async () => {
    setSaving(true)
    try {
      if (selectedModel.trim()) {
        await setPlatformSetting(TITLE_SUMMARIZER_KEY, selectedModel.trim(), accessToken)
      } else {
        await deletePlatformSetting(TITLE_SUMMARIZER_KEY, accessToken).catch(() => {})
      }
      addToast(tc.toastSaved, 'success')
    } catch {
      addToast(tc.toastSaveFailed, 'error')
    } finally {
      setSaving(false)
    }
  }, [accessToken, addToast, selectedModel, tc])

  return (
    <div className="flex flex-col gap-6">
      {loading ? (
        <p className="text-sm text-[var(--c-text-muted)]">{t.common.loading}</p>
      ) : (
        <FormField label={tc.titleSummarizer}>
          <select value={selectedModel} onChange={(e) => setSelectedModel(e.target.value)} className={inputCls}>
            <option value="">{tc.titleSummarizerNone}</option>
            {modelOptions.map((o) => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </select>
        </FormField>
      )}
      <div className="flex justify-end">
        <button onClick={handleSave} disabled={saving || loading} className={btnPrimCls}>
          {saving ? '...' : t.common.save}
        </button>
      </div>
    </div>
  )
}

// ── Email 子面板 ───────────────────────────────────────────────────────────

type EmailFormState = {
  name: string
  from_addr: string
  smtp_host: string
  smtp_port: string
  smtp_user: string
  smtp_pass: string
  smtp_tls_mode: string
  is_default: boolean
}

const emptyEmailForm = (): EmailFormState => ({
  name: '',
  from_addr: '',
  smtp_host: '',
  smtp_port: '587',
  smtp_user: '',
  smtp_pass: '',
  smtp_tls_mode: 'starttls',
  is_default: false,
})

function EmailPanel({ accessToken }: { accessToken: string }) {
  const { t } = useLocale()
  const tc = t.settingsPage
  const { addToast } = useToast()

  const [configs, setConfigs] = useState<EmailConfig[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [mutating, setMutating] = useState(false)

  const [modalOpen, setModalOpen] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [form, setForm] = useState<EmailFormState>(emptyEmailForm())

  const [deleteTarget, setDeleteTarget] = useState<EmailConfig | null>(null)
  const [testTo, setTestTo] = useState('')
  const [testModalOpen, setTestModalOpen] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const list = await listEmailConfigs(accessToken)
      setConfigs(list)
      if (list.length > 0 && !selectedId) {
        setSelectedId(list.find((c) => c.is_default)?.id ?? list[0].id)
      }
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed, selectedId])

  useEffect(() => { void load() }, [load])

  const openCreate = () => {
    setEditingId(null)
    setForm(emptyEmailForm())
    setModalOpen(true)
  }

  const openEdit = (c: EmailConfig) => {
    setEditingId(c.id)
    setForm({
      name: c.name,
      from_addr: c.from_addr,
      smtp_host: c.smtp_host,
      smtp_port: c.smtp_port,
      smtp_user: c.smtp_user,
      smtp_pass: '',
      smtp_tls_mode: c.smtp_tls_mode,
      is_default: c.is_default,
    })
    setModalOpen(true)
  }

  const handleSave = useCallback(async () => {
    if (!form.name.trim()) return
    setMutating(true)
    try {
      if (editingId) {
        await updateEmailConfig(editingId, {
          name: form.name,
          from_addr: form.from_addr,
          smtp_host: form.smtp_host,
          smtp_port: form.smtp_port,
          smtp_user: form.smtp_user,
          smtp_tls_mode: form.smtp_tls_mode,
          ...(form.smtp_pass ? { smtp_pass: form.smtp_pass } : {}),
        }, accessToken)
        addToast(tc.toastUpdated, 'success')
      } else {
        const req: CreateEmailConfigRequest = {
          name: form.name,
          from_addr: form.from_addr,
          smtp_host: form.smtp_host,
          smtp_port: form.smtp_port,
          smtp_user: form.smtp_user,
          smtp_pass: form.smtp_pass,
          smtp_tls_mode: form.smtp_tls_mode,
          is_default: form.is_default,
        }
        await createEmailConfig(req, accessToken)
        addToast(tc.toastCreated, 'success')
      }
      setModalOpen(false)
      await load()
    } catch {
      addToast(editingId ? tc.toastSaveFailed : tc.toastSaveFailed, 'error')
    } finally {
      setMutating(false)
    }
  }, [editingId, form, accessToken, addToast, tc, load])

  const handleDelete = useCallback(async () => {
    if (!deleteTarget) return
    setMutating(true)
    try {
      await deleteEmailConfig(deleteTarget.id, accessToken)
      addToast(tc.toastDeleted, 'success')
      setDeleteTarget(null)
      if (selectedId === deleteTarget.id) setSelectedId(null)
      await load()
    } catch {
      addToast(tc.toastSaveFailed, 'error')
    } finally {
      setMutating(false)
    }
  }, [deleteTarget, accessToken, addToast, tc, load, selectedId])

  const handleSetDefault = useCallback(async (id: string) => {
    setMutating(true)
    try {
      await setDefaultEmailConfig(id, accessToken)
      addToast(tc.toastSetDefault, 'success')
      await load()
    } catch {
      addToast(tc.toastSaveFailed, 'error')
    } finally {
      setMutating(false)
    }
  }, [accessToken, addToast, tc, load])

  const handleSendTest = useCallback(async () => {
    if (!testTo.trim()) return
    setMutating(true)
    try {
      await sendTestEmail(testTo.trim(), accessToken)
      addToast(tc.toastTestSent, 'success')
      setTestModalOpen(false)
      setTestTo('')
    } catch {
      addToast(tc.toastTestFailed, 'error')
    } finally {
      setMutating(false)
    }
  }, [testTo, accessToken, addToast, tc])

  const setField = <K extends keyof EmailFormState>(k: K, v: EmailFormState[K]) =>
    setForm((prev) => ({ ...prev, [k]: v }))

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-[var(--c-text-secondary)]">{tc.emailConfigs}</span>
        <div className="flex gap-2">
          <button onClick={() => setTestModalOpen(true)} disabled={configs.length === 0} className={btnSecCls}>
            {tc.sendTest}
          </button>
          <button onClick={openCreate} className={btnPrimCls}>{tc.newConfig}</button>
        </div>
      </div>

      {loading ? (
        <p className="text-sm text-[var(--c-text-muted)]">{t.common.loading}</p>
      ) : configs.length === 0 ? (
        <p className="text-sm text-[var(--c-text-muted)]">{tc.noConfigs}</p>
      ) : (
        <div className="flex flex-col gap-2">
          {configs.map((c) => (
            <div
              key={c.id}
              onClick={() => setSelectedId(c.id)}
              className={`flex cursor-pointer items-center justify-between rounded-lg border px-4 py-3 transition-colors ${
                selectedId === c.id
                  ? 'border-[var(--c-border-focus)] bg-[var(--c-bg-sub)]'
                  : 'border-[var(--c-border)] hover:bg-[var(--c-bg-sub)]'
              }`}
            >
              <div className="flex items-center gap-2.5">
                <span className="text-sm font-medium text-[var(--c-text-primary)]">{c.name}</span>
                {c.is_default && (
                  <span className="rounded bg-[var(--c-bg-tag)] px-1.5 py-0.5 text-xs text-[var(--c-text-tertiary)]">
                    {t.common.default}
                  </span>
                )}
                {c.from_addr && (
                  <span className="text-xs text-[var(--c-text-muted)]">{c.from_addr}</span>
                )}
              </div>
              <div className="flex gap-1.5" onClick={(e) => e.stopPropagation()}>
                {!c.is_default && (
                  <button onClick={() => handleSetDefault(c.id)} disabled={mutating} className={btnSecCls}>
                    {tc.setDefault}
                  </button>
                )}
                <button onClick={() => openEdit(c)} className={btnSecCls}>{t.common.edit}</button>
                <button
                  onClick={() => setDeleteTarget(c)}
                  disabled={mutating}
                  className="rounded-lg border border-[var(--c-border)] px-3 py-1.5 text-xs font-medium text-[var(--c-status-error-text)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-40"
                >
                  {t.common.delete}
                </button>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* 创建/编辑 Modal */}
      <Modal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        title={editingId ? tc.editConfig : tc.newConfig}
        width="520px"
      >
        <div className="flex flex-col gap-4">
          <FormField label={tc.configName}>
            <input value={form.name} onChange={(e) => setField('name', e.target.value)} className={inputCls} />
          </FormField>
          <FormField label={tc.fromAddr}>
            <input value={form.from_addr} onChange={(e) => setField('from_addr', e.target.value)} className={inputCls} placeholder="noreply@example.com" />
          </FormField>
          <div className="grid grid-cols-2 gap-4">
            <FormField label={tc.smtpHost}>
              <input value={form.smtp_host} onChange={(e) => setField('smtp_host', e.target.value)} className={inputCls} placeholder="smtp.example.com" />
            </FormField>
            <FormField label={tc.smtpPort}>
              <input value={form.smtp_port} onChange={(e) => setField('smtp_port', e.target.value)} className={inputCls} placeholder="587" />
            </FormField>
          </div>
          <div className="grid grid-cols-2 gap-4">
            <FormField label={tc.smtpUser}>
              <input value={form.smtp_user} onChange={(e) => setField('smtp_user', e.target.value)} className={inputCls} />
            </FormField>
            <FormField label={tc.smtpPass}>
              <input
                type="password"
                value={form.smtp_pass}
                onChange={(e) => setField('smtp_pass', e.target.value)}
                className={inputCls}
                placeholder={editingId ? tc.smtpPassPlaceholder : ''}
              />
            </FormField>
          </div>
          <FormField label={tc.smtpTlsMode}>
            <select value={form.smtp_tls_mode} onChange={(e) => setField('smtp_tls_mode', e.target.value)} className={inputCls}>
              <option value="starttls">STARTTLS</option>
              <option value="tls">TLS</option>
              <option value="none">None</option>
            </select>
          </FormField>
          {!editingId && (
            <label className="flex items-center gap-2 text-sm text-[var(--c-text-secondary)]">
              <input
                type="checkbox"
                checked={form.is_default}
                onChange={(e) => setField('is_default', e.target.checked)}
                className="h-4 w-4 rounded"
              />
              {tc.setDefault}
            </label>
          )}
        </div>
        <div className="mt-5 flex justify-end gap-2">
          <button onClick={() => setModalOpen(false)} className={btnSecCls}>{t.common.cancel}</button>
          <button onClick={handleSave} disabled={mutating || !form.name.trim()} className={btnPrimCls}>
            {mutating ? '...' : t.common.save}
          </button>
        </div>
      </Modal>

      {/* 测试发送 Modal */}
      <Modal open={testModalOpen} onClose={() => setTestModalOpen(false)} title={tc.sendTest} width="400px">
        <FormField label={tc.testTo}>
          <input
            type="email"
            value={testTo}
            onChange={(e) => setTestTo(e.target.value)}
            className={inputCls}
            placeholder="you@example.com"
          />
        </FormField>
        <div className="mt-5 flex justify-end gap-2">
          <button onClick={() => setTestModalOpen(false)} className={btnSecCls}>{t.common.cancel}</button>
          <button onClick={handleSendTest} disabled={mutating || !testTo.trim()} className={btnPrimCls}>
            {mutating ? '...' : tc.sendTest}
          </button>
        </div>
      </Modal>

      <ConfirmDialog
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleDelete}
        title={tc.deleteConfig}
        message={deleteTarget ? tc.deleteConfigConfirm(deleteTarget.name) : ''}
        loading={mutating}
      />
    </div>
  )
}

// ── System 子面板 ──────────────────────────────────────────────────────────

function CopyCommandButton({ cmd, label }: { cmd: string; label: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    void navigator.clipboard.writeText(cmd).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    })
  }

  return (
    <div className="flex items-center justify-between rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-4 py-3">
      <div>
        <p className="text-sm font-medium text-[var(--c-text-secondary)]">{label}</p>
        <p className="mt-0.5 font-mono text-xs text-[var(--c-text-muted)]">{cmd}</p>
      </div>
      <button onClick={handleCopy} className={btnSecCls + ' flex items-center gap-1'}>
        {copied ? <Check size={12} /> : <Copy size={12} />}
      </button>
    </div>
  )
}

function SystemPanel({ accessToken }: { accessToken: string }) {
  const { t } = useLocale()
  const tc = t.settingsPage
  const { addToast } = useToast()

  const [creditsEnabled, setCreditsEnabled] = useState(true)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const mode = await getCreditsMode(accessToken)
      setCreditsEnabled(mode.enabled)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastLoadFailed])

  useEffect(() => { void load() }, [load])

  const handleCreditsToggle = useCallback(async (enabled: boolean) => {
    setSaving(true)
    try {
      await setCreditsMode(enabled, accessToken)
      setCreditsEnabled(enabled)
      addToast(tc.toastSaved, 'success')
    } catch {
      addToast(tc.toastSaveFailed, 'error')
    } finally {
      setSaving(false)
    }
  }, [accessToken, addToast, tc])

  return (
    <div className="flex flex-col gap-6">
      {/* 模块安装 */}
      <div className="flex flex-col gap-3">
        <CopyCommandButton cmd={SANDBOX_INSTALL_CMD} label={tc.installSandbox} />
        <CopyCommandButton cmd={BROWSER_INSTALL_CMD} label={tc.installBrowser} />
      </div>

      {/* 积分开关 */}
      <div className="flex flex-col gap-2">
        <div className="flex items-center justify-between rounded-lg border border-[var(--c-border)] px-4 py-3">
          <div>
            <p className="text-sm font-medium text-[var(--c-text-secondary)]">{tc.useCredits}</p>
            <p className="mt-0.5 text-xs text-[var(--c-text-muted)]">{tc.creditsHint}</p>
          </div>
          {loading ? (
            <div className="h-5 w-9 animate-pulse rounded-full bg-[var(--c-border)]" />
          ) : (
            <button
              role="switch"
              aria-checked={creditsEnabled}
              disabled={saving}
              onClick={() => handleCreditsToggle(!creditsEnabled)}
              className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors disabled:opacity-40 ${
                creditsEnabled ? 'bg-[var(--c-accent)]' : 'bg-[var(--c-border)]'
              }`}
            >
              <span
                className={`inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform ${
                  creditsEnabled ? 'translate-x-[18px]' : 'translate-x-[3px]'
                }`}
              />
            </button>
          )}
        </div>
      </div>

      {/* 切换到完整 Console */}
      <a
        href={FULL_CONSOLE_URL}
        className="flex items-center justify-center rounded-lg border border-[var(--c-border)] py-2.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)]"
      >
        {tc.switchToFullConsole}
      </a>
    </div>
  )
}

// ── SettingsPage 主组件 ───────────────────────────────────────────────────

const NAV_ITEMS: { key: SettingsSection; labelKey: keyof ReturnType<typeof useSettingsLabels> }[] = [
  { key: 'general', labelKey: 'general' },
  { key: 'email', labelKey: 'email' },
  { key: 'system', labelKey: 'system' },
]

function useSettingsLabels() {
  const { t } = useLocale()
  return {
    general: t.settingsPage.general,
    email: t.settingsPage.email,
    system: t.settingsPage.system,
  }
}

export function SettingsPage() {
  const { accessToken } = useOutletContext<LiteOutletContext>()
  const { t } = useLocale()
  const [section, setSection] = useState<SettingsSection>('general')
  const labels = useSettingsLabels()

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={t.nav.settings} />
      <div className="flex flex-1 overflow-hidden">
        {/* 子 Sidebar */}
        <aside className="flex w-40 flex-shrink-0 flex-col border-r border-[var(--c-border)] py-4">
          {NAV_ITEMS.map(({ key, labelKey }) => (
            <button
              key={key}
              onClick={() => setSection(key)}
              className={`px-5 py-2 text-left text-sm transition-colors ${
                section === key
                  ? 'font-medium text-[var(--c-text-primary)]'
                  : 'text-[var(--c-text-tertiary)] hover:text-[var(--c-text-secondary)]'
              }`}
            >
              {labels[labelKey]}
            </button>
          ))}
        </aside>

        {/* 主配置区 */}
        <main className="flex-1 overflow-auto p-6">
          {section === 'general' && <GeneralPanel accessToken={accessToken} />}
          {section === 'email' && <EmailPanel accessToken={accessToken} />}
          {section === 'system' && <SystemPanel accessToken={accessToken} />}
        </main>
      </div>
    </div>
  )
}
