import { apiFetch } from '@arkloop/shared/api'
import { listThreadRuns, getRunDetail } from '../../api'

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
  schedule_kind: 'interval' | 'daily' | 'weekdays' | 'weekly' | 'monthly'
  interval_min?: number
  daily_time?: string
  monthly_day?: number
  monthly_time?: string
  weekly_day?: number
  timezone: string
  enabled: boolean
  next_fire_at: string | null
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
  schedule_kind: string
  interval_min?: number
  daily_time?: string
  monthly_day?: number
  monthly_time?: string
  weekly_day?: number
  timezone: string
}

export type UpdateJobRequest = Partial<CreateJobRequest>

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
