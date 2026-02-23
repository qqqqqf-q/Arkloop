import { apiFetch } from './client'

export type Team = {
  id: string
  org_id: string
  name: string
  members_count: number
  created_at: string
}

export type TeamMember = {
  team_id: string
  user_id: string
  role: string
  created_at: string
}

export type CreateTeamRequest = {
  name: string
}

export type AddTeamMemberRequest = {
  user_id: string
  role: string
}

export async function listTeams(accessToken: string): Promise<Team[]> {
  return apiFetch<Team[]>('/v1/teams', { accessToken })
}

export async function createTeam(req: CreateTeamRequest, accessToken: string): Promise<Team> {
  return apiFetch<Team>('/v1/teams', {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function deleteTeam(id: string, accessToken: string): Promise<void> {
  await apiFetch<void>(`/v1/teams/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function listTeamMembers(teamId: string, accessToken: string): Promise<TeamMember[]> {
  return apiFetch<TeamMember[]>(`/v1/teams/${teamId}/members`, { accessToken })
}

export async function addTeamMember(
  teamId: string,
  req: AddTeamMemberRequest,
  accessToken: string,
): Promise<TeamMember> {
  return apiFetch<TeamMember>(`/v1/teams/${teamId}/members`, {
    method: 'POST',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function removeTeamMember(
  teamId: string,
  userId: string,
  accessToken: string,
): Promise<void> {
  await apiFetch<void>(`/v1/teams/${teamId}/members/${userId}`, {
    method: 'DELETE',
    accessToken,
  })
}
