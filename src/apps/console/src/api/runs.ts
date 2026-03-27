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

export type AdminRunUsageItem = {
  run_id: string
  account_id: string
  thread_id: string
  parent_run_id?: string
  status: string
  persona_id?: string
  model?: string
  provider_kind?: string
  credential_name?: string
  persona_model?: string
  duration_ms?: number
  total_input_tokens?: number
  total_output_tokens?: number
  total_cost_usd?: number
  cache_hit_rate?: number
  cache_creation_tokens?: number
  cache_read_tokens?: number
  cached_tokens?: number
  credits_used?: number
  created_at: string
  completed_at?: string
  failed_at?: string
}

export type AdminRunUsageAggregate = {
  total_input_tokens?: number
  total_output_tokens?: number
  total_cost_usd?: number
  credits_used?: number
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
  thread_messages?: MessageResponse[]
  events_stats: AdminRunEventsStats
  children?: AdminRunUsageItem[]
  total_aggregate?: AdminRunUsageAggregate
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

export type MessageContentPart = {
  type: string
  text?: string
}

export type MessageContent = {
  parts?: MessageContentPart[]
}

export type MessageResponse = {
  id: string
  account_id: string
  thread_id: string
  created_by_user_id?: string | null
  role: string
  content: string
  content_json?: MessageContent
  created_at: string
  run_id?: string | null
}

export type ListRunsResponse = {
  data: GlobalRun[]
  total: number
}

export type ListRunsParams = {
  run_id?: string
  status?: string
  account_id?: string
  thread_id?: string
  user_id?: string
  parent_run_id?: string
  model?: string
  persona_id?: string
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
  if (params.run_id) qs.set('run_id', params.run_id)
  if (params.status) qs.set('status', params.status)
  if (params.account_id) qs.set('account_id', params.account_id)
  if (params.thread_id) qs.set('thread_id', params.thread_id)
  if (params.user_id) qs.set('user_id', params.user_id)
  if (params.parent_run_id) qs.set('parent_run_id', params.parent_run_id)
  if (params.model) qs.set('model', params.model)
  if (params.persona_id) qs.set('persona_id', params.persona_id)
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
  lastSeenSeq: number,
): Promise<{ ok: boolean }> {
  const body = JSON.stringify({ last_seen_seq: Math.max(0, lastSeenSeq) })
  return apiFetch<{ ok: boolean }>(`/v1/runs/${runId}:cancel`, {
    method: 'POST',
    accessToken,
    body,
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

  for (const block of text.split('\n\n')) {
    const dataLine = block.split('\n').find((line) => line.startsWith('data:'))
    if (!dataLine) continue
    try {
      const parsed = JSON.parse(dataLine.slice(5).trim()) as RunEventRaw
      events.push(parsed)
    } catch {
      // ignore malformed lines
    }
  }

  return events.sort((left, right) => left.seq - right.seq)
}
