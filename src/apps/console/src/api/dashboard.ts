import { apiFetch } from './client'

export type DashboardData = {
  total_users: number
  active_users_30d: number
  total_runs: number
  runs_today: number
  total_input_tokens: number
  total_output_tokens: number
  total_cost_usd: number
  active_accounts: number
}

export async function getDashboard(accessToken: string): Promise<DashboardData> {
  return apiFetch<DashboardData>('/v1/admin/dashboard', { accessToken })
}
