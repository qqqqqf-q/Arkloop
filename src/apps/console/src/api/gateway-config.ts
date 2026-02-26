import { apiFetch } from './client'

export type GatewayConfig = {
  ip_mode: 'direct' | 'cloudflare' | 'trusted_proxy'
  trusted_cidrs: string[]
  risk_reject_threshold: number
}

export type UpdateGatewayConfigRequest = {
  ip_mode: string
  trusted_cidrs: string[]
  risk_reject_threshold: number
}

export async function getGatewayConfig(accessToken: string): Promise<GatewayConfig> {
  return apiFetch<GatewayConfig>('/v1/admin/gateway-config', { accessToken })
}

export async function updateGatewayConfig(
  req: UpdateGatewayConfigRequest,
  accessToken: string,
): Promise<GatewayConfig> {
  return apiFetch<GatewayConfig>('/v1/admin/gateway-config', {
    method: 'PUT',
    body: JSON.stringify(req),
    accessToken,
  })
}
