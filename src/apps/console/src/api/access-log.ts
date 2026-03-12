import { apiFetch } from './client'

export type AccessLogEntry = {
  id: string
  timestamp: string
  trace_id: string
  method: string
  path: string
  status_code: number
  duration_ms: number
  client_ip: string
  country: string
  city: string
  user_agent: string
  ua_type: string
  risk_score: number
  identity_type: string
  account_id: string
  user_id: string
  username: string
}

export type AccessLogResponse = {
  data: AccessLogEntry[]
  has_more: boolean
  next_before?: string
}

export type AccessLogParams = {
  limit?: number
  before?: string
  since?: string
  method?: string
  path?: string
  ip?: string
  country?: string
  risk_min?: number
  ua_type?: string
}

export async function listAccessLog(
  params: AccessLogParams,
  accessToken: string,
): Promise<AccessLogResponse> {
  const qs = new URLSearchParams()
  if (params.limit) qs.set('limit', String(params.limit))
  if (params.before) qs.set('before', params.before)
  if (params.since) qs.set('since', params.since)
  if (params.method) qs.set('method', params.method)
  if (params.path) qs.set('path', params.path)
  if (params.ip) qs.set('ip', params.ip)
  if (params.country) qs.set('country', params.country)
  if (params.risk_min) qs.set('risk_min', String(params.risk_min))
  if (params.ua_type) qs.set('ua_type', params.ua_type)
  const query = qs.toString()
  const url = `/v1/admin/access-log${query ? `?${query}` : ''}`
  return apiFetch<AccessLogResponse>(url, { accessToken })
}
