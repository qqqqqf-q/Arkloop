import { apiFetch } from './client'

export type CreditTransaction = {
  id: string
  account_id: string
  amount: number
  type: string
  reference_type?: string
  reference_id?: string
  note?: string
  created_at: string
}

export type AdminOrgCreditsResponse = {
  account_id: string
  balance: number
  transactions: CreditTransaction[]
}

export async function getAdminOrgCredits(
  projectId: string,
  accessToken: string,
  limit = 20,
  offset = 0,
): Promise<AdminOrgCreditsResponse> {
  return apiFetch<AdminOrgCreditsResponse>(
    `/v1/admin/credits?account_id=${encodeURIComponent(projectId)}&limit=${limit}&offset=${offset}`,
    { accessToken },
  )
}
