import { apiFetch } from './client'

export type GlobalRun = {
  run_id: string
  account_id: string
  thread_id: string
  status: string
  model?: string
  persona_id?: string
  parent_run_id?: string
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
  account_id: string
  thread_id: string
  status: string
  model?: string
  persona_id?: string
  provider_kind?: string
  credential_name?: string
  persona_model?: string
  duration_ms?: number
  total_input_tokens?: number
  total_output_tokens?: number
  total_cost_usd?: number
  cache_hit_rate?: number
  credits_used?: number
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
  limit?: number
  offset?: number
}

export async function listRuns(
  params: ListRunsParams,
  accessToken: string,
): Promise<ListRunsResponse> {
  const qs = new URLSearchParams()
  if (params.limit != null) qs.set('limit', String(params.limit))
  if (params.offset != null) qs.set('offset', String(params.offset))
  const query = qs.toString()
  return apiFetch<ListRunsResponse>(`/v1/runs${query ? `?${query}` : ''}`, { accessToken })
}

export async function getRunDetail(
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
  if (!res.ok) throw new Error(`failed to load run events: ${res.status}`)

  const text = await res.text()
  const events: RunEventRaw[] = []

  for (const block of text.split('\n\n')) {
    const dataLine = block.split('\n').find((line) => line.startsWith('data:'))
    if (!dataLine) continue
    try {
      const parsed = JSON.parse(dataLine.slice(5).trim()) as RunEventRaw
      events.push(parsed)
    } catch {
      // 跳过损坏的事件块
    }
  }

  return events
}
