import { apiFetch } from './client'

export type ConfigSchemaEntry = {
  key: string
  type: string
  default: string
  description: string
  sensitive: boolean
  scope: string
}

export async function listConfigSchema(accessToken: string): Promise<ConfigSchemaEntry[]> {
  return apiFetch<ConfigSchemaEntry[]>('/v1/config/schema', { accessToken })
}
