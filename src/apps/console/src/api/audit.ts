import { apiFetch } from './client'

export type AuditLog = {
  id: string
  account_id?: string
  actor_user_id?: string
  action: string
  target_type?: string
  target_id?: string
  trace_id: string
  metadata: Record<string, unknown>
  ip_address?: string
  user_agent?: string
  created_at: string
}

export type ListAuditLogsResponse = {
  data: AuditLog[]
  total: number
}

export type ListAuditLogsParams = {
  action?: string
  since?: string
  until?: string
  limit?: number
  offset?: number
}

export async function listAuditLogs(
  params: ListAuditLogsParams,
  accessToken: string,
): Promise<ListAuditLogsResponse> {
  const qs = new URLSearchParams()
  if (params.action) qs.set('action', params.action)
  if (params.since) qs.set('since', params.since)
  if (params.until) qs.set('until', params.until)
  if (params.limit != null) qs.set('limit', String(params.limit))
  if (params.offset != null) qs.set('offset', String(params.offset))
  const query = qs.toString()
  return apiFetch<ListAuditLogsResponse>(
    `/v1/audit-logs${query ? `?${query}` : ''}`,
    { accessToken },
  )
}
