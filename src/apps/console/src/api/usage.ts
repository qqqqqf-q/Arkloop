import { apiFetch } from './client'

export type UsageSummary = {
  org_id: string
  year: number
  month: number
  total_input_tokens: number
  total_output_tokens: number
  total_cost_usd: number
  record_count: number
}

export async function getOrgUsage(
  orgId: string,
  year: number,
  month: number,
  accessToken: string,
): Promise<UsageSummary> {
  return apiFetch<UsageSummary>(
    `/v1/orgs/${encodeURIComponent(orgId)}/usage?year=${year}&month=${month}`,
    { accessToken },
  )
}
