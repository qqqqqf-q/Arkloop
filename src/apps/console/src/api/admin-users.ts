import { apiFetch } from './client'

export type AdminUser = {
  id: string
  login?: string
  username: string
  email: string | null
  email_verified_at?: string
  status: string
  avatar_url?: string
  locale?: string
  timezone?: string
  last_login_at?: string
  created_at: string
}

export type AdminUserOrg = {
  account_id: string
  role: string
}

export type AdminUserDetail = AdminUser & {
  accounts: AdminUserOrg[]
}

export type ListAdminUsersParams = {
  limit?: number
  q?: string
  status?: string
  before_created_at?: string
  before_id?: string
}

export async function listAdminUsers(
  params: ListAdminUsersParams,
  accessToken: string,
): Promise<AdminUser[]> {
  const sp = new URLSearchParams()
  if (params.limit) sp.set('limit', String(params.limit))
  if (params.q) sp.set('q', params.q)
  if (params.status) sp.set('status', params.status)
  if (params.before_created_at) sp.set('before_created_at', params.before_created_at)
  if (params.before_id) sp.set('before_id', params.before_id)
  const qs = sp.toString()
  return apiFetch<AdminUser[]>(`/v1/admin/users${qs ? `?${qs}` : ''}`, { accessToken })
}

export async function getAdminUser(
  userId: string,
  accessToken: string,
): Promise<AdminUserDetail> {
  return apiFetch<AdminUserDetail>(`/v1/admin/users/${userId}`, { accessToken })
}

export async function updateAdminUserStatus(
  userId: string,
  status: 'active' | 'suspended',
  accessToken: string,
): Promise<AdminUser> {
  return apiFetch<AdminUser>(`/v1/admin/users/${userId}`, {
    method: 'PATCH',
    body: JSON.stringify({ status }),
    accessToken,
  })
}

export type UpdateAdminUserRequest = {
  username: string
  email: string | null
  locale: string | null
  timezone: string | null
  email_verified: boolean
}

export async function updateAdminUser(
  userId: string,
  req: UpdateAdminUserRequest,
  accessToken: string,
): Promise<AdminUser> {
  return apiFetch<AdminUser>(`/v1/admin/users/${userId}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
    accessToken,
  })
}

export type AdminCreditsAdjustRequest = {
  account_id: string
  amount: number
  note: string
}

export type AdminCreditsResponse = {
  account_id: string
  balance: number
}

export async function adjustAdminCredits(
  req: AdminCreditsAdjustRequest,
  accessToken: string,
): Promise<AdminCreditsResponse> {
  return apiFetch<AdminCreditsResponse>('/v1/admin/credits/adjust', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function deleteAdminUser(userId: string, accessToken: string): Promise<void> {
  await apiFetch<void>(`/v1/admin/users/${userId}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function bulkAdjustCredits(
  amount: number,
  note: string,
  accessToken: string,
): Promise<{ affected: number }> {
  return apiFetch<{ affected: number }>('/v1/admin/credits/bulk-adjust', {
    method: 'POST',
    body: JSON.stringify({ amount, note }),
    accessToken,
  })
}

export async function resetAllCredits(
  note: string,
  accessToken: string,
): Promise<{ affected: number }> {
  return apiFetch<{ affected: number }>('/v1/admin/credits/reset-all', {
    method: 'POST',
    body: JSON.stringify({ note }),
    accessToken,
  })
}
