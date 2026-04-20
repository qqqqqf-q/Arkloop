import { apiFetch } from '@arkloop/shared/api'
import { listThreadRuns, getRunDetail, type RunReasoningMode } from '../../api'

export type ScheduledJobScheduleKind =
  | 'interval'
  | 'daily'
  | 'weekdays'
  | 'weekly'
  | 'monthly'
  | 'at'
  | 'cron'

export interface ScheduledJob {
  id: string
  account_id: string
  name: string
  description: string
  persona_key: string
  prompt: string
  model: string
  work_dir: string
  thread_id: string | null
  schedule_kind: ScheduledJobScheduleKind
  interval_min?: number
  daily_time?: string
  monthly_day?: number
  monthly_time?: string
  weekly_day?: number
  fire_at?: string
  cron_expr?: string
  delete_after_run?: boolean
  timezone: string
  enabled: boolean
  next_fire_at: string | null
  reasoning_mode?: RunReasoningMode
  timeout?: number
  created_at: string
  updated_at: string
}

export interface CreateJobRequest {
  name: string
  description: string
  persona_key: string
  prompt: string
  model: string
  work_dir: string
  thread_id?: string
  schedule_kind: ScheduledJobScheduleKind
  interval_min?: number
  daily_time?: string
  monthly_day?: number
  monthly_time?: string
  weekly_day?: number
  fire_at?: string
  cron_expr?: string
  delete_after_run?: boolean
  timezone: string
  reasoning_mode?: RunReasoningMode
  timeout?: number
}

export interface UpdateJobRequest extends Partial<Omit<CreateJobRequest, 'thread_id' | 'interval_min' | 'monthly_day' | 'weekly_day' | 'fire_at' | 'reasoning_mode'>> {
  thread_id?: string | null
  interval_min?: number | null
  monthly_day?: number | null
  weekly_day?: number | null
  fire_at?: string | null
  reasoning_mode?: RunReasoningMode | ''
}

export interface ScheduledJobFormValues {
  name: string
  description: string
  persona_key: string
  prompt: string
  model: string
  work_dir: string
  thread_id: string
  schedule_kind: ScheduledJobScheduleKind
  interval_min: number
  daily_time: string
  monthly_day: number
  monthly_time: string
  weekly_day: number
  fire_at: string
  cron_expr: string
  delete_after_run: boolean
  timezone: string
  reasoning_mode: RunReasoningMode | ''
  timeout: number
}

function formatDateParts(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return [
    date.getFullYear(),
    pad(date.getMonth() + 1),
    pad(date.getDate()),
  ].join('-') + `T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

export function formatScheduledJobFireAtForInput(fireAt?: string | null): string {
  if (!fireAt) return ''
  const date = new Date(fireAt)
  if (Number.isNaN(date.getTime())) return ''
  return formatDateParts(date)
}

export function normalizeScheduledJobFireAtInput(fireAt: string): string {
  if (!fireAt.trim()) return ''
  const date = new Date(fireAt)
  if (Number.isNaN(date.getTime())) return ''
  return date.toISOString().replace('.000Z', 'Z')
}

export function buildScheduledJobRequest(
  values: ScheduledJobFormValues,
  mode: 'create',
): CreateJobRequest
export function buildScheduledJobRequest(
  values: ScheduledJobFormValues,
  mode: 'update',
): UpdateJobRequest
export function buildScheduledJobRequest(
  values: ScheduledJobFormValues,
  mode: 'create' | 'update',
): CreateJobRequest | UpdateJobRequest {
  const threadID = values.thread_id.trim()
  const fireAt = normalizeScheduledJobFireAtInput(values.fire_at)
  const base = {
    name: values.name.trim(),
    description: values.description,
    persona_key: values.persona_key.trim(),
    prompt: values.prompt.trim(),
    model: values.model,
    work_dir: values.work_dir,
    schedule_kind: values.schedule_kind,
    timezone: values.timezone,
  }

  if (mode === 'create') {
    const request: CreateJobRequest = { ...base }
    if (threadID) request.thread_id = threadID
    if (values.reasoning_mode) request.reasoning_mode = values.reasoning_mode
    if (values.timeout > 0) request.timeout = values.timeout
    applyScheduledJobScheduleFields(request, values, fireAt)
    return request
  }

  const request: UpdateJobRequest = {
    ...base,
    thread_id: threadID || null,
    interval_min: null,
    daily_time: '',
    monthly_day: null,
    monthly_time: '',
    weekly_day: null,
    fire_at: null,
    cron_expr: '',
    delete_after_run: false,
    reasoning_mode: values.reasoning_mode,
    timeout: values.timeout,
  }
  applyScheduledJobScheduleFields(request, values, fireAt)
  return request
}

function applyScheduledJobScheduleFields(
  request: CreateJobRequest | UpdateJobRequest,
  values: ScheduledJobFormValues,
  fireAt: string,
): void {
  switch (values.schedule_kind) {
    case 'interval':
      request.interval_min = values.interval_min
      return
    case 'daily':
    case 'weekdays':
      request.daily_time = values.daily_time
      return
    case 'weekly':
      request.daily_time = values.daily_time
      request.weekly_day = values.weekly_day
      return
    case 'monthly':
      request.monthly_day = values.monthly_day
      request.monthly_time = values.monthly_time
      return
    case 'at':
      request.fire_at = fireAt
      request.delete_after_run = values.delete_after_run
      return
    case 'cron':
      request.cron_expr = values.cron_expr
  }
}

export async function listScheduledJobs(accessToken: string): Promise<ScheduledJob[]> {
  const resp = await apiFetch<{ jobs: ScheduledJob[] }>('/v1/scheduled-jobs', {
    method: 'GET',
    accessToken,
  })
  return resp.jobs ?? []
}

export async function getScheduledJob(accessToken: string, id: string): Promise<ScheduledJob> {
  return apiFetch<ScheduledJob>(`/v1/scheduled-jobs/${id}`, {
    method: 'GET',
    accessToken,
  })
}

export async function createScheduledJob(accessToken: string, data: CreateJobRequest): Promise<ScheduledJob> {
  return apiFetch<ScheduledJob>('/v1/scheduled-jobs', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(data),
  })
}

export async function updateScheduledJob(accessToken: string, id: string, data: UpdateJobRequest): Promise<ScheduledJob> {
  return apiFetch<ScheduledJob>(`/v1/scheduled-jobs/${id}`, {
    method: 'PUT',
    accessToken,
    body: JSON.stringify(data),
  })
}

export async function deleteScheduledJob(accessToken: string, id: string): Promise<void> {
  await apiFetch<void>(`/v1/scheduled-jobs/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function pauseScheduledJob(accessToken: string, id: string): Promise<void> {
  await apiFetch<void>(`/v1/scheduled-jobs/${id}/pause`, {
    method: 'POST',
    accessToken,
  })
}

export async function resumeScheduledJob(accessToken: string, id: string): Promise<void> {
  await apiFetch<void>(`/v1/scheduled-jobs/${id}/resume`, {
    method: 'POST',
    accessToken,
  })
}

export async function getThreadLatestRunContext(
  accessToken: string,
  threadId: string,
): Promise<{ persona_id: string | null; model: string | null }> {
  const runs = await listThreadRuns(accessToken, threadId, 1)
  if (runs.length === 0) return { persona_id: null, model: null }
  const detail = await getRunDetail(accessToken, runs[0].run_id)
  return {
    persona_id: detail.persona_id ?? null,
    model: detail.model ?? null,
  }
}
