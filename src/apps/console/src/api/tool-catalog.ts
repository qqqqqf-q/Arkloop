import { apiFetch } from './client'

export type ToolDescriptionSource = 'default' | 'platform' | 'project'

export type ToolCatalogItem = {
  name: string
  label: string
  llm_description: string
  has_override: boolean
  description_source: ToolDescriptionSource
}

export type ToolCatalogGroup = {
  group: string
  tools: ToolCatalogItem[]
}

export async function listEffectiveToolCatalog(
  accessToken: string,
  workspaceRef?: string,
): Promise<{ groups: ToolCatalogGroup[] }> {
  const query = workspaceRef?.trim()
    ? `?workspace_ref=${encodeURIComponent(workspaceRef.trim())}`
    : ''
  return apiFetch<{ groups: ToolCatalogGroup[] }>(`/v1/tool-catalog/effective${query}`, { accessToken })
}

function withScope(path: string, scope?: string): string {
  const s = (scope ?? '').trim()
  if (!s) return path
  const sep = path.includes('?') ? '&' : '?'
  return `${path}${sep}scope=${encodeURIComponent(s)}`
}

export async function updateToolDescription(
  toolName: string,
  description: string,
  accessToken: string,
  scope?: string,
): Promise<void> {
  await apiFetch<void>(withScope(`/v1/tool-catalog/${toolName}/description`, scope), {
    method: 'PUT',
    body: JSON.stringify({ description }),
    accessToken,
  })
}

export async function deleteToolDescription(
  toolName: string,
  accessToken: string,
  scope?: string,
): Promise<void> {
  await apiFetch<void>(withScope(`/v1/tool-catalog/${toolName}/description`, scope), {
    method: 'DELETE',
    accessToken,
  })
}
