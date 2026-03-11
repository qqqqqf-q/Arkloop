import { apiFetch } from './client'

export type Project = {
  id: string
  org_id?: string
  owner_user_id?: string | null
  name: string
  description?: string | null
  visibility: string
  is_default: boolean
  created_at: string
}

export async function listProjects(accessToken: string): Promise<Project[]> {
  return apiFetch<Project[]>('/v1/projects', {
    method: 'GET',
    accessToken,
  })
}
