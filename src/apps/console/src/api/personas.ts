import { apiFetch } from './client'

export type Persona = {
  id: string
  org_id: string | null
  persona_key: string
  version: string
  display_name: string
  description?: string
  prompt_md: string
  tool_allowlist: string[]
  budgets: Record<string, unknown>
  is_active: boolean
  created_at: string
  preferred_credential?: string
  executor_type: string
  executor_config: Record<string, unknown>
}

export type CreatePersonaRequest = {
  persona_key: string
  version: string
  display_name: string
  description?: string
  prompt_md: string
  tool_allowlist?: string[]
  budgets?: Record<string, unknown>
  is_active?: boolean
  preferred_credential?: string
  executor_type?: string
  executor_config?: Record<string, unknown>
}

export type PatchPersonaRequest = {
  display_name?: string
  description?: string
  prompt_md?: string
  tool_allowlist?: string[]
  budgets?: Record<string, unknown>
  is_active?: boolean
  preferred_credential?: string
  executor_type?: string
  executor_config?: Record<string, unknown>
}

export async function listPersonas(accessToken: string): Promise<Persona[]> {
  return apiFetch<Persona[]>('/v1/personas', { accessToken })
}

export async function createPersona(
  req: CreatePersonaRequest,
  accessToken: string,
): Promise<Persona> {
  return apiFetch<Persona>('/v1/personas', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function patchPersona(
  id: string,
  req: PatchPersonaRequest,
  accessToken: string,
): Promise<Persona> {
  return apiFetch<Persona>(`/v1/personas/${id}`, {
    method: 'PATCH',
    body: JSON.stringify(req),
    accessToken,
  })
}
