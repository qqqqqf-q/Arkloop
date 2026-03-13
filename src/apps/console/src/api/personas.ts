import { apiFetch } from './client'

export type PersonaScope = 'project' | 'platform'

function withScope(path: string, scope: PersonaScope, projectId?: string): string {
  const sep = path.includes('?') ? '&' : '?'
  let url = `${path}${sep}scope=${scope}`
  if (scope === 'project' && projectId) {
    url += `&project_id=${encodeURIComponent(projectId)}`
  }
  return url
}

export type Persona = {
  id: string
  scope: PersonaScope
  source?: 'builtin' | 'custom'
  sync_mode?: 'none' | 'platform_file_mirror'
  mirrored_file_path?: string
  last_synced_at?: string
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

type RawPersona = Omit<Persona, 'scope'> & Record<string, unknown> & { scope: string }

export type CreatePersonaRequest = {
  copy_from_repo_persona_key?: string
  scope?: PersonaScope
  project_id?: string
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
  scope?: PersonaScope
  project_id?: string
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

function normalizePersona(persona: RawPersona): Persona {
  return {
    ...persona,
    scope: persona.scope === 'platform' ? 'platform' : 'project',
  }
}

export async function listPersonas(accessToken: string, scope: PersonaScope, projectId?: string): Promise<Persona[]> {
  const personas = await apiFetch<RawPersona[]>(withScope('/v1/personas', scope, projectId), { accessToken })
  return personas.map(normalizePersona)
}

export async function createPersona(
  req: CreatePersonaRequest,
  accessToken: string,
): Promise<Persona> {
  const scope = req.scope ?? 'platform'
  const { is_active: _isActive, project_id: _pid, ...body } = req
  const persona = await apiFetch<RawPersona>(withScope('/v1/personas', scope, req.project_id), {
    method: 'POST',
    body: JSON.stringify({ ...body, scope }),
    accessToken,
  })
  return normalizePersona(persona)
}

export async function patchPersona(
  id: string,
  req: PatchPersonaRequest,
  accessToken: string,
): Promise<Persona> {
  const scope = req.scope ?? 'platform'
  const { scope: _scope, project_id: _pid, ...body } = req
  const persona = await apiFetch<RawPersona>(withScope(`/v1/personas/${id}`, scope, req.project_id), {
    method: 'PATCH',
    body: JSON.stringify(body),
    accessToken,
  })
  return normalizePersona(persona)
}

export async function deletePersona(
  id: string,
  scope: PersonaScope,
  accessToken: string,
  projectId?: string,
): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(withScope(`/v1/personas/${id}`, scope, projectId), {
    method: 'DELETE',
    accessToken,
  })
}
