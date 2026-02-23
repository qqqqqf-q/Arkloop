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
  created_at: string
  completed_at?: string
  failed_at?: string
}

export type ListRunsResponse = {
  data: GlobalRun[]
  total: number
}

export type ListRunsParams = {
  status?: string
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
