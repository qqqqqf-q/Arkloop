import { apiFetch } from './client'

export type Report = {
  id: string
  thread_id: string
  reporter_id: string
  reporter_email: string
  categories: string[]
  feedback: string | null
  created_at: string
}

export type ListReportsResponse = {
  data: Report[]
  total: number
}

export type ListReportsParams = {
  report_id?: string
  thread_id?: string
  reporter_id?: string
  reporter_email?: string
  category?: string
  feedback?: string
  since?: string
  until?: string
  limit?: number
  offset?: number
}

export async function listReports(
  params: ListReportsParams,
  accessToken: string,
): Promise<ListReportsResponse> {
  const qs = new URLSearchParams()
  if (params.report_id) qs.set('report_id', params.report_id)
  if (params.thread_id) qs.set('thread_id', params.thread_id)
  if (params.reporter_id) qs.set('reporter_id', params.reporter_id)
  if (params.reporter_email) qs.set('reporter_email', params.reporter_email)
  if (params.category) qs.set('category', params.category)
  if (params.feedback) qs.set('feedback', params.feedback)
  if (params.since) qs.set('since', params.since)
  if (params.until) qs.set('until', params.until)
  if (params.limit != null) qs.set('limit', String(params.limit))
  if (params.offset != null) qs.set('offset', String(params.offset))
  const query = qs.toString()
  return apiFetch<ListReportsResponse>(
    `/v1/admin/reports${query ? `?${query}` : ''}`,
    { accessToken },
  )
}
