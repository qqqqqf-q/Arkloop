import { useState, useEffect, useCallback, useRef } from 'react'
import { Folder, FolderOpen, X, Check } from 'lucide-react'
import { createPortal } from 'react-dom'
import { Modal, FormField, useToast } from '@arkloop/shared'
import { getDesktopApi, isDesktop } from '@arkloop/shared/desktop'
import { useLocale } from '../../contexts/LocaleContext'
import { SettingsSelect } from '../../components/settings/_SettingsSelect'
import {
  listAllPersonas,
  listThreads,
  listLlmProviders,
  type Persona,
  type ThreadResponse,
  type LlmProvider,
} from '../../api'
import {
  writeWorkFolder,
  readWorkRecentFolders,
  clearWorkFolder,
} from '../../storage'
import {
  createScheduledJob,
  updateScheduledJob,
  getThreadLatestRunContext,
  type ScheduledJob,
  type CreateJobRequest,
} from './api'

type Props = {
  open: boolean
  onClose: () => void
  job?: ScheduledJob | null
  onSaved: () => void
  accessToken: string
}

export default function ScheduledJobEditor({
  open,
  onClose,
  job,
  onSaved,
  accessToken,
}: Props) {
  const { t } = useLocale()
  const { addToast } = useToast()
  const isEdit = !!job
  const desktop = isDesktop()

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [personaKey, setPersonaKey] = useState('')
  const [prompt, setPrompt] = useState('')
  const [model, setModel] = useState<string>('')
  const [threadId, setThreadId] = useState('')
  const [scheduleKind, setScheduleKind] = useState<'interval' | 'daily' | 'weekdays' | 'weekly' | 'monthly'>('interval')
  const [intervalMin, setIntervalMin] = useState(60)
  const [dailyTime, setDailyTime] = useState('09:00')
  const [monthlyDay, setMonthlyDay] = useState(1)
  const [monthlyTime, setMonthlyTime] = useState('09:00')
  const [weeklyDay, setWeeklyDay] = useState(1)
  const [timezone, setTimezone] = useState(Intl.DateTimeFormat().resolvedOptions().timeZone)
  const [workDir, setWorkDir] = useState('')
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [saving, setSaving] = useState(false)

  const [personas, setPersonas] = useState<Persona[]>([])
  const [threads, setThreads] = useState<ThreadResponse[]>([])
  const [providers, setProviders] = useState<LlmProvider[]>([])

  // desktop folder picker state
  const [folderMenuOpen, setFolderMenuOpen] = useState(false)
  const [folderMenuStyle, setFolderMenuStyle] = useState<React.CSSProperties>({})
  const [recentFolders, setRecentFolders] = useState<string[]>(() => readWorkRecentFolders())
  const folderBtnRef = useRef<HTMLButtonElement>(null)
  const folderMenuRef = useRef<HTMLDivElement>(null)
  const hasInitializedThreadRef = useRef(false)

  useEffect(() => {
    if (!open) return
    setSaving(false)
    listAllPersonas(accessToken)
      .then(setPersonas)
      .catch(() => addToast(t.scheduledJobsLoadFailed, 'error'))
    listThreads(accessToken, { limit: 200 })
      .then(setThreads)
      .catch(() => { /* ignore */ })
    listLlmProviders(accessToken)
      .then(setProviders)
      .catch(() => { /* ignore */ })
  }, [open, accessToken, addToast, t.scheduledJobsLoadFailed])

  useEffect(() => {
    if (job) {
      setName(job.name)
      setDescription(job.description)
      setPersonaKey(job.persona_key)
      setPrompt(job.prompt)
      setModel(job.model || '')
      setThreadId(job.thread_id ?? '')
      setScheduleKind(job.schedule_kind)
      setIntervalMin(job.interval_min ?? 60)
      setDailyTime(job.daily_time ?? '09:00')
      setMonthlyDay(job.monthly_day ?? 1)
      setMonthlyTime(job.monthly_time ?? '09:00')
      setWeeklyDay(job.weekly_day ?? 1)
      setTimezone(job.timezone)
      setWorkDir(job.work_dir)
      setShowAdvanced(!!job.work_dir)
    } else {
      setName('')
      setDescription('')
      setPersonaKey('')
      setPrompt('')
      setModel('')
      setThreadId('')
      setScheduleKind('interval')
      setIntervalMin(60)
      setDailyTime('09:00')
      setMonthlyDay(1)
      setMonthlyTime('09:00')
      setWeeklyDay(1)
      setTimezone(Intl.DateTimeFormat().resolvedOptions().timeZone)
      setWorkDir('')
      setShowAdvanced(false)
    }
    hasInitializedThreadRef.current = false
  }, [job])

  useEffect(() => {
    if (!open) return
    if (!hasInitializedThreadRef.current) {
      hasInitializedThreadRef.current = true
      return
    }
    if (threadId.trim()) {
      getThreadLatestRunContext(accessToken, threadId.trim())
        .then((ctx) => {
          if (ctx.persona_id) setPersonaKey(ctx.persona_id)
          if (ctx.model) setModel(ctx.model)
        })
        .catch(() => { /* ignore */ })
    } else {
      setPersonaKey('')
      setModel('')
    }
  }, [threadId, accessToken, open])

  useEffect(() => {
    if (!folderMenuOpen) return
    const handler = (e: MouseEvent) => {
      const target = e.target as HTMLElement
      if (folderBtnRef.current?.contains(target)) return
      if (folderMenuRef.current && !folderMenuRef.current.contains(target)) {
        setFolderMenuOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [folderMenuOpen])

  const handleOpenFolderMenu = () => {
    if (!folderMenuOpen && folderBtnRef.current) {
      const rect = folderBtnRef.current.getBoundingClientRect()
      setFolderMenuStyle({
        position: 'fixed',
        top: rect.bottom + 4,
        left: rect.left,
        width: rect.width,
        zIndex: 9999,
      })
    }
    setFolderMenuOpen((v) => !v)
  }

  const handleSelectFolder = useCallback(async (path?: string) => {
    let folder = path
    if (!folder) {
      const api = getDesktopApi()
      if (api?.dialog) {
        folder = (await api.dialog.openFolder()) ?? undefined
      }
    }
    if (!folder) return
    writeWorkFolder(folder)
    setWorkDir(folder)
    setRecentFolders(readWorkRecentFolders())
    setFolderMenuOpen(false)
  }, [])

  const handleClearFolder = useCallback(() => {
    clearWorkFolder()
    setWorkDir('')
    setFolderMenuOpen(false)
  }, [])

  const handleSave = useCallback(async () => {
    const data: CreateJobRequest = {
      name: name.trim(),
      description,
      persona_key: personaKey.trim(),
      prompt: prompt.trim(),
      model,
      work_dir: workDir,
      schedule_kind: scheduleKind,
      timezone,
      ...(threadId.trim() ? { thread_id: threadId.trim() } : {}),
      ...(scheduleKind === 'interval' ? { interval_min: intervalMin } : {}),
      ...(scheduleKind === 'daily' || scheduleKind === 'weekdays' || scheduleKind === 'weekly'
        ? { daily_time: dailyTime }
        : {}),
      ...(scheduleKind === 'monthly'
        ? { monthly_day: monthlyDay, monthly_time: monthlyTime }
        : {}),
      ...(scheduleKind === 'weekly' ? { weekly_day: weeklyDay } : {}),
    }

    setSaving(true)
    try {
      if (isEdit && job) {
        await updateScheduledJob(accessToken, job.id, data)
      } else {
        await createScheduledJob(accessToken, data)
      }
      onSaved()
    } catch {
      addToast(t.scheduledJobsSaveFailed, 'error')
    } finally {
      setSaving(false)
    }
  }, [
    accessToken, addToast, dailyTime, description, intervalMin,
    isEdit, job, model, monthlyDay, monthlyTime, name, onSaved,
    personaKey, prompt, scheduleKind, t.scheduledJobsSaveFailed,
    threadId, timezone, weeklyDay, workDir,
  ])

  const personaOptions = [
    { value: '', label: t.scheduledJobsDefaultPersona },
    ...personas.map((p) => ({
      value: p.persona_key,
      label: p.display_name || p.persona_key,
    })),
  ]

  const threadOptions = [
    { value: '', label: t.scheduledJobsNewThread },
    ...threads.map((th) => ({
      value: th.id,
      label: th.title || th.id.slice(0, 8),
    })),
  ]

  const scheduleOptions = [
    { value: 'interval', label: t.scheduledJobsInterval },
    { value: 'daily', label: t.scheduledJobsDaily },
    { value: 'weekdays', label: t.scheduledJobsWeekdays },
    { value: 'weekly', label: t.scheduledJobsWeekly },
    { value: 'monthly', label: t.scheduledJobsMonthly },
  ]

  const weekDayOptions = [
    { value: '0', label: t.scheduledJobsSunday },
    { value: '1', label: t.scheduledJobsMonday },
    { value: '2', label: t.scheduledJobsTuesday },
    { value: '3', label: t.scheduledJobsWednesday },
    { value: '4', label: t.scheduledJobsThursday },
    { value: '5', label: t.scheduledJobsFriday },
    { value: '6', label: t.scheduledJobsSaturday },
  ]

  const modelOptions = [
    { value: '', label: t.scheduledJobsDefaultModel },
    ...providers.flatMap((p) =>
      p.models
        .filter((m) => m.show_in_picker && !m.tags.includes('embedding'))
        .map((m) => ({ value: `${p.name}^${m.model}`, label: `${p.name} · ${m.model}` })),
    ),
  ]

  const folderMenu = folderMenuOpen ? (
    <div
      ref={folderMenuRef}
      className="dropdown-menu"
      style={{
        ...folderMenuStyle,
        border: '0.5px solid var(--c-border-subtle)',
        borderRadius: '10px',
        padding: '4px',
        background: 'var(--c-bg-menu)',
        minWidth: '220px',
        boxShadow: 'var(--c-dropdown-shadow)',
      }}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
        {recentFolders.length > 0 && (
          <>
            <div style={{ padding: '4px 12px 2px', fontSize: '11px', fontWeight: 500, color: 'var(--c-text-muted)', letterSpacing: '0.3px', textTransform: 'uppercase' }}>
              Recent
            </div>
            {recentFolders.map((folder) => (
              <button
                key={folder}
                type="button"
                onClick={() => { void handleSelectFolder(folder) }}
                className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
              >
                <Folder size={13} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
                <span className="truncate" style={{ flex: 1, textAlign: 'left' }}>
                  {folder.split('/').pop() || folder}
                </span>
                {workDir === folder ? (
                  <Check size={12} style={{ flexShrink: 0, color: '#4691F6' }} />
                ) : null}
              </button>
            ))}
            <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 4px' }} />
          </>
        )}

        <button
          type="button"
          onClick={() => { void handleSelectFolder() }}
          className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
        >
          <FolderOpen size={13} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
          Choose a different folder
        </button>

        {workDir && (
          <button
            type="button"
            onClick={handleClearFolder}
            className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
          >
            <X size={13} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
            清除工作目录
          </button>
        )}
      </div>
    </div>
  ) : null

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={isEdit ? t.scheduledJobsEdit : t.scheduledJobsCreate}
      width="520px"
    >
      <div className="flex flex-col gap-4">
        <FormField label={t.scheduledJobsName}>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="h-9 w-full rounded-lg px-3 text-sm outline-none"
            style={{
              border: '0.5px solid var(--c-border-subtle)',
              background: 'var(--c-bg-input)',
              color: 'var(--c-text-primary)',
            }}
            maxLength={200}
          />
        </FormField>

        <FormField label={t.scheduledJobsDescription}>
          <input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="h-9 w-full rounded-lg px-3 text-sm outline-none"
            style={{
              border: '0.5px solid var(--c-border-subtle)',
              background: 'var(--c-bg-input)',
              color: 'var(--c-text-primary)',
            }}
          />
        </FormField>

        <FormField label={t.scheduledJobsPersona}>
          <SettingsSelect
            value={personaKey}
            onChange={setPersonaKey}
            options={personaOptions}
            placeholder={t.scheduledJobsSelectPersona}
          />
        </FormField>

        <FormField label={t.scheduledJobsPrompt}>
          <textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            rows={4}
            className="w-full resize-y rounded-lg px-3 py-2 text-sm outline-none"
            style={{
              border: '0.5px solid var(--c-border-subtle)',
              background: 'var(--c-bg-input)',
              color: 'var(--c-text-primary)',
            }}
          />
        </FormField>

        <FormField label={t.scheduledJobsModel}>
          <SettingsSelect
            value={model}
            onChange={setModel}
            options={modelOptions}
            placeholder={t.scheduledJobsSelectModel}
          />
        </FormField>

        <FormField label={t.scheduledJobsThreadId}>
          <SettingsSelect
            value={threadId}
            onChange={setThreadId}
            options={threadOptions}
            placeholder={t.scheduledJobsNewThread}
          />
        </FormField>

        <FormField label={t.scheduledJobsSchedule}>
          <SettingsSelect
            value={scheduleKind}
            onChange={(v) => setScheduleKind(v as typeof scheduleKind)}
            options={scheduleOptions}
          />
        </FormField>

        {scheduleKind === 'interval' && (
          <FormField label={t.scheduledJobsIntervalMinutes}>
            <input
              type="number"
              min={1}
              value={intervalMin}
              onChange={(e) => setIntervalMin(Number(e.target.value))}
              className="h-9 w-full rounded-lg px-3 text-sm outline-none"
              style={{
                border: '0.5px solid var(--c-border-subtle)',
                background: 'var(--c-bg-input)',
                color: 'var(--c-text-primary)',
              }}
            />
          </FormField>
        )}

        {(scheduleKind === 'daily' || scheduleKind === 'weekdays') && (
          <>
            <FormField label={t.scheduledJobsDailyTime}>
              <input
                value={dailyTime}
                onChange={(e) => setDailyTime(e.target.value)}
                placeholder="HH:MM"
                className="h-9 w-full rounded-lg px-3 text-sm outline-none"
                style={{
                  border: '0.5px solid var(--c-border-subtle)',
                  background: 'var(--c-bg-input)',
                  color: 'var(--c-text-primary)',
                }}
              />
            </FormField>
            <FormField label={t.scheduledJobsTimezone}>
              <input
                value={timezone}
                onChange={(e) => setTimezone(e.target.value)}
                className="h-9 w-full rounded-lg px-3 text-sm outline-none"
                style={{
                  border: '0.5px solid var(--c-border-subtle)',
                  background: 'var(--c-bg-input)',
                  color: 'var(--c-text-primary)',
                }}
              />
            </FormField>
          </>
        )}

        {scheduleKind === 'weekly' && (
          <>
            <FormField label={t.scheduledJobsWeeklyDay}>
              <SettingsSelect
                value={String(weeklyDay)}
                onChange={(v) => setWeeklyDay(Number(v))}
                options={weekDayOptions}
              />
            </FormField>
            <FormField label={t.scheduledJobsDailyTime}>
              <input
                value={dailyTime}
                onChange={(e) => setDailyTime(e.target.value)}
                placeholder="HH:MM"
                className="h-9 w-full rounded-lg px-3 text-sm outline-none"
                style={{
                  border: '0.5px solid var(--c-border-subtle)',
                  background: 'var(--c-bg-input)',
                  color: 'var(--c-text-primary)',
                }}
              />
            </FormField>
            <FormField label={t.scheduledJobsTimezone}>
              <input
                value={timezone}
                onChange={(e) => setTimezone(e.target.value)}
                className="h-9 w-full rounded-lg px-3 text-sm outline-none"
                style={{
                  border: '0.5px solid var(--c-border-subtle)',
                  background: 'var(--c-bg-input)',
                  color: 'var(--c-text-primary)',
                }}
              />
            </FormField>
          </>
        )}

        {scheduleKind === 'monthly' && (
          <>
            <FormField label={t.scheduledJobsMonthlyDay}>
              <input
                type="number"
                min={1}
                max={28}
                value={monthlyDay}
                onChange={(e) => setMonthlyDay(Number(e.target.value))}
                className="h-9 w-full rounded-lg px-3 text-sm outline-none"
                style={{
                  border: '0.5px solid var(--c-border-subtle)',
                  background: 'var(--c-bg-input)',
                  color: 'var(--c-text-primary)',
                }}
              />
            </FormField>
            <FormField label={t.scheduledJobsMonthlyTime}>
              <input
                value={monthlyTime}
                onChange={(e) => setMonthlyTime(e.target.value)}
                placeholder="HH:MM"
                className="h-9 w-full rounded-lg px-3 text-sm outline-none"
                style={{
                  border: '0.5px solid var(--c-border-subtle)',
                  background: 'var(--c-bg-input)',
                  color: 'var(--c-text-primary)',
                }}
              />
            </FormField>
            <FormField label={t.scheduledJobsTimezone}>
              <input
                value={timezone}
                onChange={(e) => setTimezone(e.target.value)}
                className="h-9 w-full rounded-lg px-3 text-sm outline-none"
                style={{
                  border: '0.5px solid var(--c-border-subtle)',
                  background: 'var(--c-bg-input)',
                  color: 'var(--c-text-primary)',
                }}
              />
            </FormField>
          </>
        )}

        <button
          type="button"
          onClick={() => setShowAdvanced((v) => !v)}
          className="flex items-center gap-1 text-xs font-medium"
          style={{ color: 'var(--c-text-tertiary)' }}
        >
          <span
            className="transition-transform duration-150"
            style={{
              display: 'inline-block',
              transform: showAdvanced ? 'rotate(180deg)' : 'rotate(0deg)',
            }}
          >
            ▼
          </span>
          {t.scheduledJobsAdvanced}
        </button>

        {showAdvanced && (
          <>
            {desktop && (
              <FormField label={t.scheduledJobsWorkDir}>
                <div className="relative">
                  <button
                    ref={folderBtnRef}
                    type="button"
                    onClick={handleOpenFolderMenu}
                    className="flex h-9 w-full items-center justify-between rounded-lg px-3 text-sm transition-colors hover:bg-[var(--c-bg-deep)]"
                    style={{
                      border: '0.5px solid var(--c-border-subtle)',
                      background: 'var(--c-bg-input)',
                      color: workDir ? 'var(--c-text-primary)' : 'var(--c-text-secondary)',
                    }}
                  >
                    <span className="flex items-center gap-2 truncate">
                      {workDir
                        ? <FolderOpen size={14} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
                        : <Folder size={14} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
                      }
                      <span className="truncate">
                        {workDir
                          ? (workDir.split('/').pop() || workDir)
                          : 'Work in a folder'
                        }
                      </span>
                    </span>
                  </button>
                  {folderMenu && createPortal(folderMenu, document.body)}
                </div>
              </FormField>
            )}
            {!desktop && (
              <FormField label={t.scheduledJobsWorkDir}>
                <input
                  value={workDir}
                  onChange={(e) => setWorkDir(e.target.value)}
                  className="h-9 w-full rounded-lg px-3 text-sm outline-none"
                  style={{
                    border: '0.5px solid var(--c-border-subtle)',
                    background: 'var(--c-bg-input)',
                    color: 'var(--c-text-primary)',
                  }}
                />
              </FormField>
            )}
          </>
        )}

        <div className="mt-2 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg px-4 py-2 text-sm font-medium transition-colors hover:bg-[var(--c-bg-deep)]"
            style={{
              color: 'var(--c-text-secondary)',
              border: '0.5px solid var(--c-border-subtle)',
            }}
          >
            {t.scheduledJobsCancel}
          </button>
          <button
            type="button"
            onClick={handleSave}
            disabled={saving || !name.trim() || !prompt.trim()}
            className="rounded-lg px-4 py-2 text-sm font-medium transition-opacity hover:opacity-80 disabled:opacity-40"
            style={{
              background: 'var(--c-accent)',
              color: 'var(--c-accent-fg)',
            }}
          >
            {saving ? t.scheduledJobsSaving : t.scheduledJobsSave}
          </button>
        </div>
      </div>
    </Modal>
  )
}
