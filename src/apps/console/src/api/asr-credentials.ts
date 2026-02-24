import { apiFetch } from './client'

export type AsrCredential = {
  id: string
  org_id: string
  provider: string
  name: string
  key_prefix: string | null
  base_url: string | null
  model: string
  is_default: boolean
  created_at: string
}

export type CreateAsrCredentialRequest = {
  name: string
  provider: string
  api_key: string
  base_url?: string
  model: string
  is_default: boolean
}

export async function listAsrCredentials(accessToken: string): Promise<AsrCredential[]> {
  return apiFetch<AsrCredential[]>('/v1/asr-credentials', { accessToken })
}

export async function createAsrCredential(
  req: CreateAsrCredentialRequest,
  accessToken: string,
): Promise<AsrCredential> {
  return apiFetch<AsrCredential>('/v1/asr-credentials', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function deleteAsrCredential(id: string, accessToken: string): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/asr-credentials/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function setDefaultAsrCredential(id: string, accessToken: string): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/asr-credentials/${id}/set-default`, {
    method: 'POST',
    accessToken,
  })
}
