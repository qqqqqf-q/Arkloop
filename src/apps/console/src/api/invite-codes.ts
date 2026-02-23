import { apiFetch } from './client'

export type InviteCode = {
  id: string
  user_id: string
  code: string
  max_uses: number
  use_count: number
  is_active: boolean
  created_at: string
}

export type AdminInviteCode = InviteCode & {
  user_display_name: string
  user_email: string | null
}

export type Referral = {
  id: string
  inviter_user_id: string
  invitee_user_id: string
  invite_code_id: string
  credited: boolean
  created_at: string
  inviter_display_name: string
  invitee_display_name: string
}

export type ReferralTreeNode = {
  user_id: string
  display_name: string
  inviter_id: string | null
  depth: number
  created_at: string
}

export type ListAdminInviteCodesParams = {
  limit?: number
  q?: string
  before_created_at?: string
  before_id?: string
}

export async function listAdminInviteCodes(
  params: ListAdminInviteCodesParams,
  accessToken: string,
): Promise<AdminInviteCode[]> {
  const sp = new URLSearchParams()
  if (params.limit) sp.set('limit', String(params.limit))
  if (params.q) sp.set('q', params.q)
  if (params.before_created_at) sp.set('before_created_at', params.before_created_at)
  if (params.before_id) sp.set('before_id', params.before_id)
  const qs = sp.toString()
  return apiFetch<AdminInviteCode[]>(`/v1/admin/invite-codes${qs ? `?${qs}` : ''}`, { accessToken })
}

export type PatchInviteCodeRequest = {
  max_uses?: number
  is_active?: boolean
}

export async function patchAdminInviteCode(
  id: string,
  body: PatchInviteCodeRequest,
  accessToken: string,
): Promise<InviteCode> {
  return apiFetch<InviteCode>(`/v1/admin/invite-codes/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
    accessToken,
  })
}

export type ListReferralsParams = {
  limit?: number
  before_created_at?: string
  before_id?: string
}

export async function listReferrals(
  inviterUserId: string,
  params: ListReferralsParams,
  accessToken: string,
): Promise<Referral[]> {
  const sp = new URLSearchParams()
  sp.set('inviter_user_id', inviterUserId)
  if (params.limit) sp.set('limit', String(params.limit))
  if (params.before_created_at) sp.set('before_created_at', params.before_created_at)
  if (params.before_id) sp.set('before_id', params.before_id)
  return apiFetch<Referral[]>(`/v1/admin/referrals?${sp.toString()}`, { accessToken })
}

export async function getReferralTree(
  userId: string,
  accessToken: string,
): Promise<ReferralTreeNode[]> {
  return apiFetch<ReferralTreeNode[]>(`/v1/admin/referrals/tree?user_id=${userId}`, { accessToken })
}
