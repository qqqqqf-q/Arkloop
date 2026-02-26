import { apiFetch } from './client'

export type GlobalRun = {
  run_id: string
  org_id: string
  thread_id: string
  status: string
  model?: string
  skill_id?: string
  total_input_tokens?: number
  total_output_tokens?: number
  total_cost_usd?: number
  duration_ms?: number
  cache_hit_rate?: number
  credits_used?: number
  created_at: string
  completed_at?: string
  failed_at?: string
  created_by_user_id?: string
  created_by_user_name?: string
  created_by_email?: string
}

export type AdminRunEventsStats = {
  total: number
  llm_turns: number
  tool_calls: number
  provider_fallbacks: number
}

export type AdminRunDetail = {
  run_id: string
  org_id: string
  thread_id: string
  status: string
  model?: string
  skill_id?: string
  provider_kind?: string
  credential_name?: string
  agent_config_name?: string
  duration_ms?: number
  total_input_tokens?: number
  total_output_tokens?: number
  total_cost_usd?: number
  created_at: string
  completed_at?: string
  failed_at?: string
  created_by_user_id?: string
  created_by_user_name?: string
  created_by_email?: string
  user_prompt?: string
  events_stats: AdminRunEventsStats
}

export type RunEventRaw = {
  event_id: string
  run_id: string
  seq: number
  ts: string
  type: string
  data: Record<string, unknown>
  tool_name?: string
  error_class?: string
}

export type ListRunsResponse = {
  data: GlobalRun[]
  total: number
}

export type ListRunsParams = {
  status?: string
  user_id?: string
  since?: string
  until?: string
  limit?: number
  offset?: number
}

export async function listRuns(
  params: ListRunsParams,
  accessToken: string,
): Promise<ListRunsResponse> {
  const qs = new URLSearchParams()
  if (params.status) qs.set('status', params.status)
  if (params.user_id) qs.set('user_id', params.user_id)
  if (params.since) qs.set('since', params.since)
  if (params.until) qs.set('until', params.until)
  if (params.limit != null) qs.set('limit', String(params.limit))
  if (params.offset != null) qs.set('offset', String(params.offset))
  const query = qs.toString()
  return apiFetch<ListRunsResponse>(`/v1/runs${query ? `?${query}` : ''}`, { accessToken })
}

export async function cancelRun(
  runId: string,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/runs/${runId}:cancel`, {
    method: 'POST',
    accessToken,
  })
}

export async function getAdminRunDetail(
  runId: string,
  accessToken: string,
): Promise<AdminRunDetail> {
  return apiFetch<AdminRunDetail>(`/v1/admin/runs/${runId}`, { accessToken })
}

export async function fetchRunEventsOnce(
  runId: string,
  accessToken: string,
): Promise<RunEventRaw[]> {
  const res = await fetch(
    `${(import.meta.env.VITE_API_BASE_URL as string | undefined ?? '').replace(/\/$/, '')}/v1/runs/${runId}/events?follow=false`,
    { headers: { Authorization: `Bearer ${accessToken}` } },
  )
  if (!res.ok) return []

  const text = await res.text()
  const events: RunEventRaw[] = []

  // SSE 格式解析：每个 event block 以空行分隔
  for (const block of text.split('\n\n')) {
    const dataLine = block.split('\n').find((l) => l.startsWith('data:'))
    if (!dataLine) continue
    try {
      const parsed = JSON.parse(dataLine.slice(5).trim()) as RunEventRaw
      events.push(parsed)
    } catch {
      // 忽略格式异常的行
    }
  }

  return events
}

