import { useState, useCallback, useEffect, useMemo } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Loader2, Plus, Trash2, Star, Send, ChevronDown } from 'lucide-react'
import type { LiteOutletContext } from '../layouts/LiteLayout'
import { PageHeader } from '../components/PageHeader'
import { FormField } from '../components/FormField'
import { Modal } from '../components/Modal'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { useToast } from '../components/useToast'
import { useLocale } from '../contexts/LocaleContext'
import type { LocaleStrings } from '../locales'
import {
  listPlatformSettings,
  updatePlatformSetting,
  listSmtpProviders,
  createSmtpProvider,
  updateSmtpProvider,
  deleteSmtpProvider,
  setDefaultSmtpProvider,
  testSmtpProvider,
  type SmtpProvider,
} from '../api/settings'
import { listLlmProviders, type LlmProvider } from '../api/llm-providers'

type Section = 'general' | 'email' | 'sandbox' | 'credits'

const SECTIONS: Section[] = ['general', 'email', 'sandbox', 'credits']

const TLS_MODES = ['starttls', 'tls', 'none'] as const
const SANDBOX_PROVIDERS = ['firecracker', 'docker'] as const

const CREDITS_ENABLED_POLICY = '{"tiers":[{"up_to_tokens":2000,"multiplier":0},{"multiplier":1}]}'
const CREDITS_DISABLED_POLICY = '{"tiers":[{"multiplier":0}]}'

function isCreditsEnabled(policyValue: string): boolean {
  if (!policyValue) return true
  try {
    const p = JSON.parse(policyValue)
    const tiers = p?.tiers
    if (!Array.isArray(tiers) || tiers.length === 0) return true
    return tiers.some((t: { multiplier?: number }) => (t.multiplier ?? 0) > 0)
  } catch {
    return true
  }
}

async function saveSetting(key: string, value: string, accessToken: string) {
  if (value.trim() === '') return
  await updatePlatformSetting(key, value, accessToken)
}

const inputCls =
  'w-full rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none placeholder:text-[var(--c-text-muted)] focus:border-[var(--c-border-focus)]'
const selectCls =
  'w-full appearance-none rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-input)] px-3 py-1.5 pr-8 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-border-focus)]'
const btnPrimaryCls =
  'rounded-md bg-[var(--c-btn-bg)] px-3 py-1.5 text-sm font-medium text-[var(--c-btn-text)] transition-colors hover:opacity-90 disabled:opacity-50'
const btnSecondaryCls =
  'rounded-md border border-[var(--c-border)] px-3 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50'

type SmtpFormData = {
  name: string
  from_addr: string
  smtp_host: string
  smtp_port: number
  smtp_user: string
  smtp_pass: string
  tls_mode: string
}

const emptySmtpForm: SmtpFormData = {
  name: '',
  from_addr: '',
  smtp_host: '',
  smtp_port: 587,
  smtp_user: '',
  smtp_pass: '',
  tls_mode: 'starttls',
}

export function SettingsPage() {
  const { accessToken } = useOutletContext<LiteOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.settingsPage

  const [loading, setLoading] = useState(true)
  const [section, setSection] = useState<Section>('general')

  const [llmProviders, setLlmProviders] = useState<LlmProvider[]>([])
  const [smtpList, setSmtpList] = useState<SmtpProvider[]>([])

  // General
  const [titleModel, setTitleModel] = useState('')

  // Sandbox
  const [sandboxProvider, setSandboxProvider] = useState('')
  const [sandboxBaseUrl, setSandboxBaseUrl] = useState('')
  const [sandboxDockerImage, setSandboxDockerImage] = useState('')

  // Credits
  const [creditsOn, setCreditsOn] = useState(true)

  // SMTP
  const [selectedSmtpId, setSelectedSmtpId] = useState<string>('')
  const [showAddSmtp, setShowAddSmtp] = useState(false)
  const [smtpForm, setSmtpForm] = useState<SmtpFormData>({ ...emptySmtpForm })
  const [smtpSaving, setSmtpSaving] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<SmtpProvider | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [testTarget, setTestTarget] = useState<SmtpProvider | null>(null)
  const [testTo, setTestTo] = useState('')
  const [testing, setTesting] = useState(false)

  // Save states
  const [savingGeneral, setSavingGeneral] = useState(false)
  const [savingSandbox, setSavingSandbox] = useState(false)
  const [savingCredits, setSavingCredits] = useState(false)

  const allModels = useMemo(() => {
    const result: { id: string; label: string }[] = []
    for (const p of llmProviders) {
      for (const m of p.models) {
        result.push({ id: m.model, label: `${m.model} (${p.name})` })
      }
    }
    return result
  }, [llmProviders])

  const selectedSmtp = useMemo(
    () => smtpList.find((s) => s.id === selectedSmtpId) ?? null,
    [smtpList, selectedSmtpId],
  )

  const loadData = useCallback(async () => {
    try {
      const [settings, providers, smtp] = await Promise.all([
        listPlatformSettings(accessToken),
        listLlmProviders(accessToken),
        listSmtpProviders(accessToken),
      ])
      const map: Record<string, string> = {}
      for (const s of settings) map[s.key] = s.value
      setLlmProviders(providers)
      setSmtpList(smtp)

      setTitleModel(map['title_summarizer.model'] ?? '')
      setSandboxProvider(map['sandbox.provider'] ?? '')
      setSandboxBaseUrl(map['sandbox.base_url'] ?? '')
      setSandboxDockerImage(map['sandbox.docker_image'] ?? '')
      setCreditsOn(isCreditsEnabled(map['credit.deduction_policy'] ?? ''))

      // 默认选中第一个 SMTP provider
      if (smtp.length > 0) setSelectedSmtpId(smtp[0].id)
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc.toastFailed])

  useEffect(() => {
    loadData()
  }, [loadData])

  // 选中 SMTP 时填充表单
  useEffect(() => {
    if (selectedSmtp) {
      setSmtpForm({
        name: selectedSmtp.name,
        from_addr: selectedSmtp.from_addr,
        smtp_host: selectedSmtp.smtp_host,
        smtp_port: selectedSmtp.smtp_port,
        smtp_user: selectedSmtp.smtp_user,
        smtp_pass: '',
        tls_mode: selectedSmtp.tls_mode,
      })
    }
  }, [selectedSmtp])

  // --- Save handlers ---

  const saveGeneral = useCallback(async () => {
    setSavingGeneral(true)
    try {
      await saveSetting('title_summarizer.model', titleModel, accessToken)
      addToast(tc.toastSaved, 'success')
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setSavingGeneral(false)
    }
  }, [titleModel, accessToken, addToast, tc])

  const handleSaveSandbox = useCallback(async () => {
    setSavingSandbox(true)
    try {
      await Promise.all([
        saveSetting('sandbox.provider', sandboxProvider, accessToken),
        saveSetting('sandbox.base_url', sandboxBaseUrl, accessToken),
        saveSetting('sandbox.docker_image', sandboxDockerImage, accessToken),
      ])
      addToast(tc.toastSaved, 'success')
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setSavingSandbox(false)
    }
  }, [sandboxProvider, sandboxBaseUrl, sandboxDockerImage, accessToken, addToast, tc])

  const handleSaveCredits = useCallback(async () => {
    setSavingCredits(true)
    try {
      const policy = creditsOn ? CREDITS_ENABLED_POLICY : CREDITS_DISABLED_POLICY
      await updatePlatformSetting('credit.deduction_policy', policy, accessToken)
      addToast(tc.toastSaved, 'success')
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setSavingCredits(false)
    }
  }, [creditsOn, accessToken, addToast, tc])

  const handleSaveSmtp = useCallback(async () => {
    setSmtpSaving(true)
    try {
      await updateSmtpProvider(selectedSmtpId, { ...smtpForm, smtp_port: smtpForm.smtp_port || 587 }, accessToken)
      const updated = await listSmtpProviders(accessToken)
      setSmtpList(updated)
      addToast(tc.toastSaved, 'success')
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setSmtpSaving(false)
    }
  }, [selectedSmtpId, smtpForm, accessToken, addToast, tc])

  const handleCreateSmtp = useCallback(async () => {
    setSmtpSaving(true)
    try {
      const created = await createSmtpProvider(
        { ...smtpForm, smtp_port: smtpForm.smtp_port || 587 },
        accessToken,
      )
      const updated = await listSmtpProviders(accessToken)
      setSmtpList(updated)
      setSelectedSmtpId(created.id)
      setShowAddSmtp(false)
      addToast(tc.toastSaved, 'success')
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setSmtpSaving(false)
    }
  }, [smtpForm, accessToken, addToast, tc])

  const handleDeleteSmtp = useCallback(async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteSmtpProvider(deleteTarget.id, accessToken)
      const updated = await listSmtpProviders(accessToken)
      setSmtpList(updated)
      if (selectedSmtpId === deleteTarget.id) {
        setSelectedSmtpId(updated.length > 0 ? updated[0].id : '')
      }
      addToast(tc.toastDeleted, 'success')
    } catch {
      addToast(tc.toastFailed, 'error')
    } finally {
      setDeleting(false)
      setDeleteTarget(null)
    }
  }, [deleteTarget, accessToken, selectedSmtpId, addToast, tc])

  const handleSetDefault = useCallback(
    async (id: string) => {
      try {
        await setDefaultSmtpProvider(id, accessToken)
        const updated = await listSmtpProviders(accessToken)
        setSmtpList(updated)
        addToast(tc.toastSaved, 'success')
      } catch {
        addToast(tc.toastFailed, 'error')
      }
    },
    [accessToken, addToast, tc],
  )

  const handleTestSmtp = useCallback(async () => {
    if (!testTarget) return
    setTesting(true)
    try {
      await testSmtpProvider(testTarget.id, testTo, accessToken)
      addToast(tc.toastTestSent, 'success')
      setTestTarget(null)
      setTestTo('')
    } catch {
      addToast(tc.toastTestFailed, 'error')
    } finally {
      setTesting(false)
    }
  }, [testTarget, testTo, accessToken, addToast, tc])

  const sectionLabels: Record<Section, string> = {
    general: tc.sectionGeneral,
    email: tc.sectionEmail,
    sandbox: tc.sectionSandbox,
    credits: tc.sectionCredits,
  }

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} />

      {loading ? (
        <div className="flex flex-1 items-center justify-center">
          <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
        </div>
      ) : (
        <div className="flex flex-1 overflow-hidden">
          {/* Section sidebar */}
          <div className="w-[160px] shrink-0 overflow-y-auto border-r border-[var(--c-border-console)] p-2">
            <div className="flex flex-col gap-[3px]">
              {SECTIONS.map((s) => (
                <button
                  key={s}
                  onClick={() => setSection(s)}
                  className={[
                    'flex h-[30px] items-center rounded-[5px] px-3 text-sm font-medium transition-colors',
                    s === section
                      ? 'bg-[var(--c-bg-sub)] text-[var(--c-text-primary)]'
                      : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]',
                  ].join(' ')}
                >
                  {sectionLabels[s]}
                </button>
              ))}
            </div>
          </div>

          {/* Content */}
          {section === 'email' ? (
            /* Email section: ModelsPage-style split layout */
            <div className="flex flex-1 overflow-hidden">
              {/* SMTP provider list sidebar */}
              <div className="w-[200px] shrink-0 flex flex-col overflow-hidden border-r border-[var(--c-border-console)]">
                <div className="flex-1 overflow-y-auto px-2 pt-2">
                  <div className="flex flex-col gap-[3px]">
                    {smtpList.map((s) => (
                      <button
                        key={s.id}
                        onClick={() => setSelectedSmtpId(s.id)}
                        className={[
                          'flex items-center justify-between rounded-[5px] px-3 py-1.5 text-sm font-medium transition-colors text-left',
                          s.id === selectedSmtpId
                            ? 'bg-[var(--c-bg-sub)] text-[var(--c-text-primary)]'
                            : 'text-[var(--c-text-tertiary)] hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]',
                        ].join(' ')}
                      >
                        <span className="truncate">{s.name}</span>
                        {s.is_default && (
                          <span className="ml-2 shrink-0 rounded bg-[var(--c-status-success-bg)] px-1.5 py-0.5 text-[10px] text-[var(--c-status-success-text)]">
                            {tc.smtpDefault}
                          </span>
                        )}
                      </button>
                    ))}
                    {smtpList.length === 0 && (
                      <p className="px-3 py-4 text-center text-xs text-[var(--c-text-muted)]">{tc.smtpNoProviders}</p>
                    )}
                  </div>
                </div>
                <div className="border-t border-[var(--c-border-console)] px-3 py-3">
                  <button
                    onClick={() => {
                      setShowAddSmtp(true)
                      setSmtpForm({ ...emptySmtpForm })
                    }}
                    className="flex h-7 w-full items-center justify-center gap-1.5 rounded-md text-sm text-[var(--c-text-muted)] transition-colors hover:bg-[var(--c-bg-sub)] hover:text-[var(--c-text-secondary)]"
                  >
                    <Plus size={14} />
                    {tc.addSmtp}
                  </button>
                </div>
              </div>

              {/* SMTP detail form */}
              <div className="flex-1 overflow-y-auto p-6">
                {selectedSmtp ? (
                  <SmtpDetailPanel
                    smtp={selectedSmtp}
                    form={smtpForm}
                    setForm={setSmtpForm}
                    saving={smtpSaving}
                    onSave={handleSaveSmtp}
                    onDelete={setDeleteTarget}
                    onSetDefault={handleSetDefault}
                    onTest={setTestTarget}
                    tc={tc}
                    tCommon={t.common}
                  />
                ) : (
                  <div className="flex h-full items-center justify-center">
                    <p className="text-sm text-[var(--c-text-muted)]">{tc.smtpNoProviders}</p>
                  </div>
                )}
              </div>
            </div>
          ) : (
            /* Other sections: centered form */
            <div className="flex-1 overflow-y-auto p-6">
              <div className="mx-auto max-w-xl space-y-6">
                {section === 'general' && (
                  <GeneralSection
                    titleModel={titleModel}
                    setTitleModel={setTitleModel}
                    allModels={allModels}
                    saving={savingGeneral}
                    onSave={saveGeneral}
                    tc={tc}
                    tCommon={t.common}
                  />
                )}
                {section === 'sandbox' && (
                  <SandboxSection
                    sandboxProvider={sandboxProvider}
                    setSandboxProvider={setSandboxProvider}
                    sandboxBaseUrl={sandboxBaseUrl}
                    setSandboxBaseUrl={setSandboxBaseUrl}
                    sandboxDockerImage={sandboxDockerImage}
                    setSandboxDockerImage={setSandboxDockerImage}
                    saving={savingSandbox}
                    onSave={handleSaveSandbox}
                    tc={tc}
                    tCommon={t.common}
                  />
                )}
                {section === 'credits' && (
                  <CreditsSection
                    creditsOn={creditsOn}
                    setCreditsOn={setCreditsOn}
                    saving={savingCredits}
                    onSave={handleSaveCredits}
                    tc={tc}
                    tCommon={t.common}
                  />
                )}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Add SMTP modal */}
      <Modal
        open={showAddSmtp}
        onClose={() => setShowAddSmtp(false)}
        title={tc.addSmtp}
        width="480px"
      >
        <div className="space-y-4">
          <SmtpFormFields form={smtpForm} setForm={setSmtpForm} passPlaceholder="" />
          <div className="flex justify-end gap-2 pt-2">
            <button onClick={() => setShowAddSmtp(false)} className={btnSecondaryCls}>
              {t.common.cancel}
            </button>
            <button
              onClick={handleCreateSmtp}
              disabled={smtpSaving || !smtpForm.name.trim() || !smtpForm.from_addr.trim()}
              className={btnPrimaryCls}
            >
              {smtpSaving ? '...' : t.common.save}
            </button>
          </div>
        </div>
      </Modal>

      {/* Delete confirm */}
      <ConfirmDialog
        open={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        onConfirm={handleDeleteSmtp}
        message={tc.smtpDeleteConfirm}
        loading={deleting}
      />

      {/* Test email dialog */}
      <Modal
        open={!!testTarget}
        onClose={() => { setTestTarget(null); setTestTo('') }}
        title={`${tc.smtpTest} - ${testTarget?.name ?? ''}`}
        width="400px"
      >
        <div className="space-y-4">
          <FormField label={tc.smtpTestTo}>
            <input
              type="email"
              value={testTo}
              onChange={(e) => setTestTo(e.target.value)}
              className={inputCls}
              placeholder="test@example.com"
            />
          </FormField>
          <div className="flex justify-end gap-2 pt-2">
            <button onClick={() => { setTestTarget(null); setTestTo('') }} className={btnSecondaryCls}>
              {t.common.cancel}
            </button>
            <button
              onClick={handleTestSmtp}
              disabled={testing || !testTo.includes('@')}
              className={btnPrimaryCls}
            >
              {testing ? '...' : tc.smtpTest}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  )
}

// --- Shared SMTP form fields ---

type SettingsLocale = LocaleStrings['settingsPage']
type CommonLocale = LocaleStrings['common']

function SmtpFormFields({
  form,
  setForm,
  passPlaceholder,
}: {
  form: SmtpFormData
  setForm: (v: SmtpFormData) => void
  passPlaceholder: string
}) {
  const { t } = useLocale()
  const tc = t.settingsPage
  const upd = (patch: Partial<SmtpFormData>) => setForm({ ...form, ...patch })

  return (
    <>
      <FormField label={tc.smtpName}>
        <input value={form.name} onChange={(e) => upd({ name: e.target.value })} className={inputCls} />
      </FormField>
      <FormField label={tc.smtpFrom}>
        <input
          type="email"
          value={form.from_addr}
          onChange={(e) => upd({ from_addr: e.target.value })}
          className={inputCls}
          placeholder="noreply@example.com"
        />
      </FormField>
      <div className="grid grid-cols-3 gap-3">
        <div className="col-span-2">
          <FormField label={tc.smtpHost}>
            <input value={form.smtp_host} onChange={(e) => upd({ smtp_host: e.target.value })} className={inputCls} placeholder="smtp.example.com" />
          </FormField>
        </div>
        <FormField label={tc.smtpPort}>
          <input
            type="number"
            value={form.smtp_port}
            onChange={(e) => upd({ smtp_port: parseInt(e.target.value) || 587 })}
            className={inputCls}
          />
        </FormField>
      </div>
      <FormField label={tc.smtpUser}>
        <input value={form.smtp_user} onChange={(e) => upd({ smtp_user: e.target.value })} className={inputCls} />
      </FormField>
      <FormField label={tc.smtpPass}>
        <input
          type="password"
          value={form.smtp_pass}
          onChange={(e) => upd({ smtp_pass: e.target.value })}
          className={inputCls}
          placeholder={passPlaceholder}
        />
      </FormField>
      <FormField label={tc.smtpTls}>
        <div className="relative">
          <select value={form.tls_mode} onChange={(e) => upd({ tls_mode: e.target.value })} className={selectCls}>
            {TLS_MODES.map((m) => (
              <option key={m} value={m}>
                {m.toUpperCase()}
              </option>
            ))}
          </select>
          <ChevronDown size={14} className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]" />
        </div>
      </FormField>
    </>
  )
}

// --- SMTP detail panel (right side when provider selected) ---

function SmtpDetailPanel({
  smtp,
  form,
  setForm,
  saving,
  onSave,
  onDelete,
  onSetDefault,
  onTest,
  tc,
  tCommon,
}: {
  smtp: SmtpProvider
  form: SmtpFormData
  setForm: (v: SmtpFormData) => void
  saving: boolean
  onSave: () => void
  onDelete: (s: SmtpProvider) => void
  onSetDefault: (id: string) => void
  onTest: (s: SmtpProvider) => void
  tc: SettingsLocale
  tCommon: CommonLocale
}) {
  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <SmtpFormFields form={form} setForm={setForm} passPlaceholder={smtp.pass_set ? '******' : ''} />

      <div className="flex items-center justify-between border-t border-[var(--c-border-console)] pt-4">
        <div className="flex gap-2">
          {!smtp.is_default && (
            <button
              onClick={() => onSetDefault(smtp.id)}
              className={btnSecondaryCls + ' flex items-center gap-1.5'}
            >
              <Star size={14} />
              {tc.smtpSetDefault}
            </button>
          )}
          <button
            onClick={() => onTest(smtp)}
            className={btnSecondaryCls + ' flex items-center gap-1.5'}
          >
            <Send size={14} />
            {tc.smtpTest}
          </button>
          {!smtp.is_default && (
            <button
              onClick={() => onDelete(smtp)}
              className="flex items-center gap-1.5 rounded-md border border-[var(--c-border)] px-3 py-1.5 text-sm text-[var(--c-status-error-text)] transition-colors hover:bg-[var(--c-status-error-bg)]"
            >
              <Trash2 size={14} />
              {tCommon.delete}
            </button>
          )}
        </div>
        <button onClick={onSave} disabled={saving} className={btnPrimaryCls}>
          {saving ? '...' : tCommon.save}
        </button>
      </div>
    </div>
  )
}

// --- General section ---

function GeneralSection({
  titleModel,
  setTitleModel,
  allModels,
  saving,
  onSave,
  tc,
  tCommon,
}: {
  titleModel: string
  setTitleModel: (v: string) => void
  allModels: { id: string; label: string }[]
  saving: boolean
  onSave: () => void
  tc: SettingsLocale
  tCommon: CommonLocale
}) {
  return (
    <>
      <FormField label={tc.titleSummarizer}>
        <div className="relative">
          <select value={titleModel} onChange={(e) => setTitleModel(e.target.value)} className={selectCls}>
            <option value="">--</option>
            {allModels.map((m) => (
              <option key={m.id} value={m.id}>
                {m.label}
              </option>
            ))}
          </select>
          <ChevronDown size={14} className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]" />
        </div>
        <p className="text-xs text-[var(--c-text-muted)]">{tc.titleSummarizerHint}</p>
      </FormField>
      <div className="flex justify-end">
        <button onClick={onSave} disabled={saving} className={btnPrimaryCls}>
          {saving ? '...' : tCommon.save}
        </button>
      </div>
    </>
  )
}

// --- Sandbox section ---

function SandboxSection({
  sandboxProvider,
  setSandboxProvider,
  sandboxBaseUrl,
  setSandboxBaseUrl,
  sandboxDockerImage,
  setSandboxDockerImage,
  saving,
  onSave,
  tc,
  tCommon,
}: {
  sandboxProvider: string
  setSandboxProvider: (v: string) => void
  sandboxBaseUrl: string
  setSandboxBaseUrl: (v: string) => void
  sandboxDockerImage: string
  setSandboxDockerImage: (v: string) => void
  saving: boolean
  onSave: () => void
  tc: SettingsLocale
  tCommon: CommonLocale
}) {
  return (
    <>
      <FormField label={tc.sandboxProvider}>
        <div className="relative">
          <select value={sandboxProvider} onChange={(e) => setSandboxProvider(e.target.value)} className={selectCls}>
            <option value="">--</option>
            {SANDBOX_PROVIDERS.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
          <ChevronDown size={14} className="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-[var(--c-text-muted)]" />
        </div>
      </FormField>
      <FormField label={tc.sandboxBaseUrl}>
        <input value={sandboxBaseUrl} onChange={(e) => setSandboxBaseUrl(e.target.value)} className={inputCls} placeholder="http://localhost:8002" />
      </FormField>
      {sandboxProvider === 'docker' && (
        <FormField label={tc.sandboxDockerImage}>
          <input value={sandboxDockerImage} onChange={(e) => setSandboxDockerImage(e.target.value)} className={inputCls} />
        </FormField>
      )}
      <div className="flex justify-end">
        <button onClick={onSave} disabled={saving} className={btnPrimaryCls}>
          {saving ? '...' : tCommon.save}
        </button>
      </div>
    </>
  )
}

// --- Credits section ---

function CreditsSection({
  creditsOn,
  setCreditsOn,
  saving,
  onSave,
  tc,
  tCommon,
}: {
  creditsOn: boolean
  setCreditsOn: (v: boolean) => void
  saving: boolean
  onSave: () => void
  tc: SettingsLocale
  tCommon: CommonLocale
}) {
  return (
    <>
      <div className="flex items-center justify-between rounded-lg border border-[var(--c-border-console)] p-4">
        <div>
          <p className="text-sm font-medium text-[var(--c-text-primary)]">{tc.creditsEnabled}</p>
          <p className="mt-1 text-xs text-[var(--c-text-muted)]">{tc.creditsDisabledHint}</p>
        </div>
        <button
          onClick={() => setCreditsOn(!creditsOn)}
          className={[
            'relative h-5 w-9 shrink-0 rounded-full transition-colors',
            creditsOn ? 'bg-[var(--c-status-success-text)]' : 'bg-[var(--c-border)]',
          ].join(' ')}
        >
          <span
            className={[
              'absolute top-0.5 h-4 w-4 rounded-full bg-white transition-transform',
              creditsOn ? 'left-[18px]' : 'left-0.5',
            ].join(' ')}
          />
        </button>
      </div>
      <div className="flex justify-end">
        <button onClick={onSave} disabled={saving} className={btnPrimaryCls}>
          {saving ? '...' : tCommon.save}
        </button>
      </div>
    </>
  )
}
