import { apiFetch } from './client'

export type RedemptionCode = {
  id: string
  code: string
  type: string
  value: string
  max_uses: number
  use_count: number
  expires_at?: string
  is_active: boolean
  batch_id?: string
  created_by_user_id: string
  created_at: string
}

export type ListRedemptionCodesParams = {
  limit?: number
  q?: string
  type?: string
  before_created_at?: string
  before_id?: string
}

export async function listRedemptionCodes(
  params: ListRedemptionCodesParams,
  accessToken: string,
): Promise<RedemptionCode[]> {
  const sp = new URLSearchParams()
  if (params.limit) sp.set('limit', String(params.limit))
  if (params.q) sp.set('q', params.q)
  if (params.type) sp.set('type', params.type)
  if (params.before_created_at) sp.set('before_created_at', params.before_created_at)
  if (params.before_id) sp.set('before_id', params.before_id)
  const qs = sp.toString()
  return apiFetch<RedemptionCode[]>(`/v1/admin/redemption-codes${qs ? `?${qs}` : ''}`, { accessToken })
}

export type BatchCreateRequest = {
  count: number
  type: string
  value: string
  max_uses: number
  expires_at?: string
  batch_id?: string
}

export async function batchCreateRedemptionCodes(
  body: BatchCreateRequest,
  accessToken: string,
): Promise<RedemptionCode[]> {
  return apiFetch<RedemptionCode[]>('/v1/admin/redemption-codes/batch', {
    method: 'POST',
    body: JSON.stringify(body),
    accessToken,
  })
}

export async function deactivateRedemptionCode(
  id: string,
  accessToken: string,
): Promise<RedemptionCode> {
  return apiFetch<RedemptionCode>(`/v1/admin/redemption-codes/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ is_active: false }),
    accessToken,
  })
}
