import { apiFetch } from './client'

export type Persona = {
  id: string
  org_id: string | null
  source?: 'builtin' | 'custom'
  persona_key: string
  version: string
  display_name: string
  user_selectable: boolean
  selector_name?: string
  selector_order?: number
  description?: string
  prompt_md: string
  tool_allowlist: string[]
  tool_denylist: string[]
  budgets: Record<string, unknown>
  is_active: boolean
  created_at: string
  preferred_credential?: string
  model?: string
  reasoning_mode: string
  prompt_cache_control: string
  executor_type: string
  executor_config: Record<string, unknown>
}

export type CreatePersonaRequest = {
  copy_from_repo_persona_key?: string
  persona_key: string
  version: string
  display_name: string
  description?: string
  prompt_md: string
  tool_allowlist?: string[]
  tool_denylist?: string[]
  budgets?: Record<string, unknown>
  is_active?: boolean
  preferred_credential?: string
  model?: string
  reasoning_mode?: string
  prompt_cache_control?: string
  executor_type?: string
  executor_config?: Record<string, unknown>
}

export type PatchPersonaRequest = {
  display_name?: string
  description?: string
  prompt_md?: string
  tool_allowlist?: string[]
  tool_denylist?: string[]
  budgets?: Record<string, unknown>
  is_active?: boolean
  preferred_credential?: string
  model?: string
  reasoning_mode?: string
  prompt_cache_control?: string
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

export async function deletePersona(
  id: string,
  accessToken: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/personas/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}
