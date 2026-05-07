import { useState, useEffect, useCallback } from 'react'
import { Plus, Trash2, Pencil } from 'lucide-react'
import { ConfirmDialog, useToast } from '@arkloop/shared'
import { useAuth } from '../../contexts/auth'
import { useLocale } from '../../contexts/LocaleContext'
import { SettingsButton, SettingsIconButton } from '../../components/settings/_SettingsButton'
import {
  listScheduledJobs,
  deleteScheduledJob,
  pauseScheduledJob,
  resumeScheduledJob,
  type ScheduledJob,
} from './api'
import ScheduledJobEditor from './ScheduledJobEditor'

function scheduleLabel(job: ScheduledJob, t: ReturnType<typeof useLocale>['t']): string {
  switch (job.schedule_kind) {
    case 'interval':
      return `${t.scheduledJobsInterval} ${job.interval_min ?? 0}min`
    case 'daily':
      return `${t.scheduledJobsDaily} ${job.daily_time ?? ''} (${job.timezone})`
    case 'weekdays':
      return `${t.scheduledJobsWeekdays} ${job.daily_time ?? ''} (${job.timezone})`
    case 'weekly': {
      const days = [
        t.scheduledJobsSunday,
        t.scheduledJobsMonday,
        t.scheduledJobsTuesday,
        t.scheduledJobsWednesday,
        t.scheduledJobsThursday,
        t.scheduledJobsFriday,
        t.scheduledJobsSaturday,
      ]
      const dayLabel = days[job.weekly_day ?? 1] ?? ''
      return `${t.scheduledJobsWeekly} ${dayLabel} ${job.daily_time ?? ''} (${job.timezone})`
    }
    case 'monthly':
      return `${t.scheduledJobsMonthly} ${job.monthly_day ?? ''}${t.scheduledJobsDailyTime ? ' ' : ''}${job.monthly_time ?? ''} (${job.timezone})`
    case 'at':
      return `${t.scheduledJobsAt} ${job.fire_at ?? ''} (${job.timezone})`
    case 'cron':
      return `${t.scheduledJobsCron} ${job.cron_expr ?? ''} (${job.timezone})`
    default:
      return job.schedule_kind
  }
}

function relativeTime(iso: string | null): string {
  if (!iso) return '-'
  const diff = new Date(iso).getTime() - Date.now()
  if (diff < 0) return '-'
  const mins = Math.floor(diff / 60000)
  if (mins < 60) return `${mins}m`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h ${mins % 60}m`
  const days = Math.floor(hours / 24)
  return `${days}d`
}

export default function ScheduledJobsPage() {
  const { accessToken } = useAuth()
  const { t } = useLocale()
  const { addToast } = useToast()

  const [jobs, setJobs] = useState<ScheduledJob[]>([])
  const [loading, setLoading] = useState(true)
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null)
  const [editorOpen, setEditorOpen] = useState(false)
  const [editingJob, setEditingJob] = useState<ScheduledJob | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    listScheduledJobs(accessToken)
      .then(setJobs)
      .catch(() => addToast(t.scheduledJobsLoadFailed, 'error'))
      .finally(() => setLoading(false))
  }, [accessToken, addToast, t.scheduledJobsLoadFailed])

  useEffect(() => { load() }, [load])

  const handleToggle = useCallback(async (job: ScheduledJob) => {
    try {
      if (job.enabled) {
        await pauseScheduledJob(accessToken, job.id)
      } else {
        await resumeScheduledJob(accessToken, job.id)
      }
      load()
    } catch {
      addToast(t.requestFailed, 'error')
    }
  }, [accessToken, addToast, load, t.requestFailed])

  const handleDelete = useCallback(async (id: string) => {
    setDeleteConfirmId(null)
    try {
      await deleteScheduledJob(accessToken, id)
      load()
    } catch {
      addToast(t.scheduledJobsDeleteFailed, 'error')
    }
  }, [accessToken, addToast, load, t.scheduledJobsDeleteFailed])

  const openCreate = useCallback(() => {
    setEditingJob(null)
    setEditorOpen(true)
  }, [])

  const openEdit = useCallback((job: ScheduledJob) => {
    setEditingJob(job)
    setEditorOpen(true)
  }, [])

  const closeEditor = useCallback(() => {
    setEditorOpen(false)
    setEditingJob(null)
  }, [])

  const handleSaved = useCallback(() => {
    closeEditor()
    load()
  }, [closeEditor, load])

  const deleteTarget = deleteConfirmId ? jobs.find((j) => j.id === deleteConfirmId) : null

  return (
    <div className="mx-auto w-full max-w-[720px] px-6 py-10">
      {/* header */}
      <div className="mb-6 flex items-center justify-between">
        <h1
          className="text-[20px] font-semibold"
          style={{ color: 'var(--c-text-primary)' }}
        >
          {t.scheduledJobsTitle}
        </h1>
        <SettingsButton
          variant="primary"
          onClick={openCreate}
          icon={<Plus size={14} />}
        >
          {t.scheduledJobsCreate}
        </SettingsButton>
      </div>

      {/* content */}
      {loading ? (
        <p className="text-[13px]" style={{ color: 'var(--c-text-muted)' }}>
          {t.loading}
        </p>
      ) : jobs.length === 0 ? (
        <p className="text-[13px]" style={{ color: 'var(--c-text-muted)' }}>
          {t.scheduledJobsEmpty}
        </p>
      ) : (
        <div
          className="overflow-hidden rounded-xl"
          style={{ border: '0.5px solid var(--c-border)' }}
        >
          <table className="w-full text-left text-[13px]">
            <colgroup>
              <col />
              <col />
              <col />
              <col />
              <col className="w-[136px]" />
            </colgroup>
            <thead>
              <tr
                style={{
                  borderBottom: '0.5px solid var(--c-border)',
                  color: 'var(--c-text-tertiary)',
                }}
              >
                <th className="px-4 py-2.5 font-medium">{t.scheduledJobsName}</th>
                <th className="px-4 py-2.5 font-medium">{t.scheduledJobsSchedule}</th>
                <th className="px-4 py-2.5 font-medium">{t.scheduledJobsNextRun}</th>
                <th className="px-4 py-2.5 font-medium">{t.scheduledJobsStatus}</th>
                <th className="px-4 py-2.5 font-medium text-right" />
              </tr>
            </thead>
            <tbody>
              {jobs.map((job) => (
                <tr
                  key={job.id}
                  style={{
                    borderBottom: '0.5px solid var(--c-border-subtle)',
                    color: 'var(--c-text-primary)',
                  }}
                >
                  <td className="px-4 py-3 font-medium">{job.name}</td>
                  <td
                    className="px-4 py-3"
                    style={{ color: 'var(--c-text-secondary)' }}
                  >
                    {scheduleLabel(job, t)}
                  </td>
                  <td
                    className="px-4 py-3"
                    style={{ color: 'var(--c-text-secondary)' }}
                  >
                    {relativeTime(job.next_fire_at)}
                  </td>
                  <td className="px-4 py-3">
                    <button
                      onClick={() => handleToggle(job)}
                      className="inline-flex items-center rounded py-0.5 text-[12px] font-medium transition-colors hover:opacity-80"
                      style={{
                        color: job.enabled
                          ? 'var(--c-status-success-text)'
                          : 'var(--c-text-muted)',
                      }}
                    >
                      {job.enabled ? t.scheduledJobsEnabled : t.scheduledJobsDisabled}
                    </button>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-1">
                      <SettingsIconButton
                        label={t.scheduledJobsEdit}
                        onClick={() => openEdit(job)}
                      >
                        <Pencil size={13} />
                      </SettingsIconButton>
                      <SettingsIconButton
                        label={t.deleteThreadConfirm}
                        danger
                        onClick={() => setDeleteConfirmId(job.id)}
                      >
                        <Trash2 size={13} />
                      </SettingsIconButton>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <ConfirmDialog
        open={deleteTarget != null}
        title={t.deleteThreadConfirm}
        message={deleteTarget ? t.scheduledJobsDeleteConfirm(deleteTarget.name) : ''}
        confirmLabel={t.deleteThreadConfirm}
        cancelLabel={t.deleteThreadCancel}
        onClose={() => setDeleteConfirmId(null)}
        onConfirm={() => {
          if (deleteTarget) void handleDelete(deleteTarget.id)
        }}
      />

      {/* editor modal */}
      {editorOpen && (
        <ScheduledJobEditor
          open={editorOpen}
          onClose={closeEditor}
          job={editingJob}
          onSaved={handleSaved}
          accessToken={accessToken}
        />
      )}
    </div>
  )
}
