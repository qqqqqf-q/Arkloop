import { apiFetch } from './client'

export type MeUsageSummary = {
  org_id: string
  year: number
  month: number
  total_input_tokens: number
  total_output_tokens: number
  total_cost_usd: number
  record_count: number
}

export async function getMeUsage(
  year: number,
  month: number,
  accessToken: string,
): Promise<MeUsageSummary> {
  return apiFetch<MeUsageSummary>(
    `/v1/me/usage?year=${year}&month=${month}`,
    { accessToken },
  )
}
