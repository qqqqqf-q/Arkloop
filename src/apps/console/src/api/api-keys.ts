import { apiFetch } from './client'

export type APIKey = {
  id: string
  org_id: string
  user_id: string
  name: string
  key_prefix: string
  scopes: string[]
  revoked_at?: string
  last_used_at?: string
  created_at: string
}

export type CreateAPIKeyRequest = {
  name: string
  scopes?: string[]
}

export type CreateAPIKeyResponse = APIKey & {
  key: string
}

export async function listAPIKeys(accessToken: string): Promise<APIKey[]> {
  return apiFetch<APIKey[]>('/v1/api-keys', { accessToken })
}

export async function createAPIKey(
  req: CreateAPIKeyRequest,
  accessToken: string,
): Promise<CreateAPIKeyResponse> {
  return apiFetch<CreateAPIKeyResponse>('/v1/api-keys', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function revokeAPIKey(id: string, accessToken: string): Promise<void> {
  await apiFetch<void>(`/v1/api-keys/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}
