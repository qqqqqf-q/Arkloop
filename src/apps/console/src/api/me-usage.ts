import { apiFetch } from './client'
import type { DailyUsage, ModelUsage } from './usage'

export type MeUsageSummary = {
  account_id: string
  year: number
  month: number
  total_input_tokens: number
  total_output_tokens: number
  total_cost_usd: number
  record_count: number
}

export type MeCreditsResponse = {
  balance: number
  transactions: {
    id: string
    account_id: string
    amount: number
    type: string
    reference_type?: string
    reference_id?: string
    note?: string
    created_at: string
  }[]
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

export async function getMeDailyUsage(
  start: string,
  end: string,
  accessToken: string,
): Promise<DailyUsage[]> {
  return apiFetch<DailyUsage[]>(
    `/v1/me/usage/daily?start=${start}&end=${end}`,
    { accessToken },
  )
}

export async function getMeUsageByModel(
  year: number,
  month: number,
  accessToken: string,
): Promise<ModelUsage[]> {
  return apiFetch<ModelUsage[]>(
    `/v1/me/usage/by-model?year=${year}&month=${month}`,
    { accessToken },
  )
}

export async function getMeCredits(
  accessToken: string,
): Promise<MeCreditsResponse> {
  return apiFetch<MeCreditsResponse>(
    '/v1/me/credits',
    { accessToken },
  )
}
