export {
  TRACE_ID_HEADER,
  ApiError,
  isApiError,
  apiFetch,
  silentRefresh,
  setUnauthenticatedHandler,
  setAccessTokenHandler,
  setSessionExpiredHandler,
  refreshAccessToken,
  restoreAccessSession,
} from '@arkloop/shared/api'

export type { LoginRequest, LoginResponse } from '@arkloop/shared/api/types'
export type { RunEvent } from './sse'

import {
  apiFetch,
  ApiError,
  TRACE_ID_HEADER,
  buildUrl,
  apiBaseUrl,
  readJsonSafely,
} from '@arkloop/shared/api'
import type { ErrorEnvelope } from '@arkloop/shared/api'
import type { LoginRequest, LoginResponse } from '@arkloop/shared/api/types'
import { parseSSEChunk, type RunEvent } from './sse'

export type RegisterRequest = {
  login: string
  password: string
  email: string
  invite_code?: string
  locale?: string
  cf_turnstile_token?: string
}

export type RegisterResponse = {
  user_id: string
  token_type: string
  access_token: string
  warning?: string
}

export type RegistrationModeResponse = {
  mode: 'invite_only' | 'open'
}

export type ResolveIdentityRequest = {
  identity: string
  cf_turnstile_token?: string
}

export type ResolveIdentityResponse =
  | {
      next_step: 'password'
      flow_token: string
      masked_email?: string
      otp_available: boolean
    }
  | {
      next_step: 'register'
      invite_required: boolean
      prefill?: {
        login?: string
        email?: string
      }
    }

export type MeResponse = {
  id: string
  username: string
  email?: string
  email_verified: boolean
  email_verification_required: boolean
  work_enabled: boolean
  timezone?: string | null
  account_timezone?: string | null
}

export type SkillReference = {
  skill_key: string
  version: string
}

export type SkillPackageResponse = {
  skill_key: string
  version: string
  display_name: string
  description?: string
  instruction_path: string
  manifest_key: string
  bundle_key: string
  platforms?: string[]
  is_active: boolean
  registry_provider?: string
  registry_slug?: string
  registry_owner_handle?: string
  registry_version?: string
  registry_detail_url?: string
  registry_download_url?: string
  registry_source_kind?: string
  registry_source_url?: string
  scan_status?: 'clean' | 'suspicious' | 'malicious' | 'pending' | 'error' | 'unknown'
  scan_has_warnings?: boolean
  scan_checked_at?: string
  scan_engine?: string
  scan_summary?: string
  moderation_verdict?: string
}

export type InstalledSkill = SkillPackageResponse & {
  profile_ref?: string
  workspace_ref?: string
  source?: 'official' | 'custom' | 'github' | 'platform' | 'builtin'
  is_platform?: boolean
  platform_status?: 'auto' | 'manual' | 'removed'
  created_at?: string
  updated_at?: string
}

export type DefaultSkillsResponse = {
  items: InstalledSkill[]
}

export type MarketSkill = {
  skill_key: string
  version?: string
  display_name: string
  description?: string
  source: 'official'
  updated_at?: string
  detail_url?: string
  repository_url?: string
  registry_provider?: string
  registry_slug?: string
  owner_handle?: string
  stats?: {
    comments?: number
    downloads?: number
    installs_all_time?: number
    installs_current?: number
    stars?: number
    versions?: number
  }
  scan_status?: 'clean' | 'suspicious' | 'malicious' | 'pending' | 'error' | 'unknown'
  scan_has_warnings?: boolean
  scan_summary?: string
  moderation_verdict?: string
  installed: boolean
  enabled_by_default: boolean
}

export type MarketSkillsResponse = {
  items: MarketSkill[]
}

export type SkillImportCandidate = {
  path: string
  skill_key?: string
  version?: string
  display_name?: string
}

export type GitHubImportResponse = {
  skill: SkillPackageResponse
  candidates?: SkillImportCandidate[]
}

export type Persona = {
  id: string
  project_id: string | null
  scope: 'platform' | 'user'
  source?: 'builtin' | 'custom'
  persona_key: string
  version: string
  display_name: string
  description?: string
  user_selectable: boolean
  selector_name?: string
  selector_order?: number
  prompt_md: string
  tool_allowlist: string[]
  tool_denylist: string[]
  core_tools: string[]
  budgets: Record<string, unknown>
  is_active: boolean
  created_at: string
  preferred_credential?: string
  model?: string
  reasoning_mode: string
  stream_thinking: boolean
  prompt_cache_control: string
  executor_type: string
  executor_config: Record<string, unknown>
}

export type SelectablePersona = {
  persona_key: string
  selector_name: string
  selector_order: number
}

export async function login(req: LoginRequest): Promise<LoginResponse> {
  return await apiFetch<LoginResponse>('/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function register(req: RegisterRequest): Promise<RegisterResponse> {
  return await apiFetch<RegisterResponse>('/v1/auth/register', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function getRegistrationMode(): Promise<RegistrationModeResponse> {
  return await apiFetch<RegistrationModeResponse>('/v1/auth/registration-mode', {
    method: 'GET',
  })
}

export async function resolveIdentity(req: ResolveIdentityRequest): Promise<ResolveIdentityResponse> {
  return await apiFetch<ResolveIdentityResponse>('/v1/auth/resolve', {
    method: 'POST',
    body: JSON.stringify(req),
  })
}

export async function sendResolvedEmailOTP(flowToken: string, cfTurnstileToken?: string): Promise<void> {
  await apiFetch<void>('/v1/auth/resolve/otp/send', {
    method: 'POST',
    body: JSON.stringify({ flow_token: flowToken, cf_turnstile_token: cfTurnstileToken }),
  })
}

export async function verifyResolvedEmailOTP(flowToken: string, code: string): Promise<LoginResponse> {
  return await apiFetch<LoginResponse>('/v1/auth/resolve/otp/verify', {
    method: 'POST',
    body: JSON.stringify({ flow_token: flowToken, code }),
  })
}

export async function getMe(accessToken: string): Promise<MeResponse> {
  return await apiFetch<MeResponse>('/v1/me', {
    method: 'GET',
    accessToken,
  })
}

export async function listInstalledSkills(accessToken: string): Promise<InstalledSkill[]> {
  const response = await apiFetch<{ items: InstalledSkill[] }>('/v1/profiles/me/skills', {
    method: 'GET',
    accessToken,
  })
  return response.items ?? []
}

export async function listDefaultSkills(accessToken: string): Promise<InstalledSkill[]> {
  const response = await apiFetch<DefaultSkillsResponse>('/v1/profiles/me/default-skills', {
    method: 'GET',
    accessToken,
  })
  return response.items ?? []
}

export async function replaceDefaultSkills(accessToken: string, skills: SkillReference[]): Promise<InstalledSkill[]> {
  const response = await apiFetch<DefaultSkillsResponse>('/v1/profiles/me/default-skills', {
    method: 'PUT',
    accessToken,
    body: JSON.stringify({ skills }),
  })
  return response.items ?? []
}

export async function searchMarketSkills(accessToken: string, query: string, officialOnly = false): Promise<MarketSkill[]> {
  const sp = new URLSearchParams()
  if (query.trim()) sp.set('q', query.trim())
  if (officialOnly) sp.set('official_only', 'true')
  const suffix = sp.toString() ? `?${sp.toString()}` : ''
  const response = await apiFetch<MarketSkillsResponse>(`/v1/market/skills${suffix}`, {
    method: 'GET',
    accessToken,
  })
  return response.items ?? []
}

export async function importRegistrySkill(
  accessToken: string,
  payload: { slug?: string; version?: string; skill_key?: string; detail_url?: string; repository_url?: string },
): Promise<SkillPackageResponse> {
  return await apiFetch<SkillPackageResponse>('/v1/skill-packages/import/registry', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(payload),
  })
}

export async function importSkillsMPSkill(
  accessToken: string,
  payload: { slug?: string; version?: string; skill_key?: string; detail_url?: string; repository_url?: string },
): Promise<SkillPackageResponse> {
  return await apiFetch<SkillPackageResponse>('/v1/skill-packages/import/skillsmp', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(payload),
  })
}

export async function installSkill(accessToken: string, skill: SkillReference): Promise<void> {
  await apiFetch<void>('/v1/profiles/me/skills/install', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(skill),
  })
}

export async function deleteSkill(accessToken: string, skill: SkillReference): Promise<void> {
  await apiFetch<void>(`/v1/profiles/me/skills/${encodeURIComponent(skill.skill_key)}/${encodeURIComponent(skill.version)}`, {
    method: 'DELETE',
    accessToken,
  })
}

export type PlatformSkillItem = {
  skill_key: string
  version: string
  display_name: string
  description?: string
  platform_status: 'auto' | 'manual' | 'removed'
  is_platform: true
}

export async function listPlatformSkills(accessToken: string): Promise<PlatformSkillItem[]> {
  const response = await apiFetch<{ items: PlatformSkillItem[] }>('/v1/profiles/me/platform-skills', {
    headers: { Authorization: `Bearer ${accessToken}` },
  })
  return response.items ?? []
}

export async function setPlatformSkillOverride(
  accessToken: string,
  skillKey: string,
  version: string,
  status: 'auto' | 'manual' | 'removed',
): Promise<void> {
  await apiFetch(`/v1/profiles/me/platform-skills/${encodeURIComponent(skillKey)}/${encodeURIComponent(version)}`, {
    method: 'PUT',
    headers: { Authorization: `Bearer ${accessToken}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ status }),
  })
}

export async function importSkillFromGitHub(
  accessToken: string,
  payload: { repository_url: string; ref?: string; candidate_path?: string },
): Promise<GitHubImportResponse> {
  return await apiFetch<GitHubImportResponse>('/v1/skill-packages/import/github', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(payload),
  })
}

export async function importSkillFromUpload(
  accessToken: string,
  payload: { file: File; install_after_import?: boolean },
): Promise<SkillPackageResponse> {
  const body = new FormData()
  body.append('file', payload.file)
  if (payload.install_after_import) body.append('install_after_import', 'true')
  return await apiFetch<SkillPackageResponse>('/v1/skill-packages/import/upload', {
    method: 'POST',
    accessToken,
    body,
    headers: {},
  })
}

export async function listPersonas(accessToken: string): Promise<Persona[]> {
  return await apiFetch<Persona[]>('/v1/me/selectable-personas', {
    method: 'GET',
    accessToken,
  })
}

export async function listAllPersonas(accessToken: string): Promise<Persona[]> {
  return await apiFetch<Persona[]>('/v1/personas', {
    method: 'GET',
    accessToken,
  })
}

export async function listChannelPersonas(accessToken: string): Promise<Persona[]> {
  const [projectScoped, platformScoped] = await Promise.all([
    apiFetch<Persona[]>('/v1/personas?scope=user', {
      method: 'GET',
      accessToken,
    }),
    apiFetch<Persona[]>('/v1/personas?scope=platform', {
      method: 'GET',
      accessToken,
    }),
  ])

  const byKey = new Map<string, Persona>()
  for (const persona of [...platformScoped, ...projectScoped]) {
    if (!persona.is_active) continue
    if (!isUUIDLike(persona.id)) continue
    byKey.set(persona.persona_key, persona)
  }

  return Array.from(byKey.values())
    .filter((persona) => persona.is_active)
    .sort((left, right) => {
      const leftOrder = left.selector_order ?? 99
      const rightOrder = right.selector_order ?? 99
      if (leftOrder !== rightOrder) {
        return leftOrder - rightOrder
      }
      const leftName = (left.selector_name ?? left.display_name).trim() || left.persona_key
      const rightName = (right.selector_name ?? right.display_name).trim() || right.persona_key
      const byName = leftName.localeCompare(rightName)
      if (byName !== 0) return byName
      return left.persona_key.localeCompare(right.persona_key)
    })
}

function isUUIDLike(value: string): boolean {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value.trim())
}

export async function listSelectablePersonas(accessToken: string): Promise<SelectablePersona[]> {
  const personas = await listPersonas(accessToken)

  return personas
    .filter((persona) => persona.user_selectable)
    .map((persona) => ({
      persona_key: persona.persona_key,
      selector_name: (persona.selector_name ?? persona.display_name).trim() || persona.persona_key,
      selector_order: persona.selector_order ?? 99,
    }))
    .sort((left, right) => {
      if (left.selector_order !== right.selector_order) {
        return left.selector_order - right.selector_order
      }
      const byName = left.selector_name.localeCompare(right.selector_name)
      if (byName !== 0) return byName
      return left.persona_key.localeCompare(right.persona_key)
    })
}

export type UpdateMeRequest = {
  username?: string
  timezone?: string
}

export async function updateMe(accessToken: string, payload: UpdateMeRequest): Promise<{ username: string; timezone?: string | null }> {
  return await apiFetch<{ username: string; timezone?: string | null }>('/v1/me', {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify(payload),
  })
}

export async function sendEmailVerification(accessToken: string): Promise<void> {
  await apiFetch<void>('/v1/auth/email/verify/send', {
    method: 'POST',
    accessToken,
  })
}

export async function confirmEmailVerification(token: string): Promise<{ ok: boolean }> {
  return await apiFetch<{ ok: boolean }>('/v1/auth/email/verify/confirm', {
    method: 'POST',
    body: JSON.stringify({ token }),
  })
}

export async function sendEmailOTP(email: string, cfTurnstileToken?: string): Promise<void> {
  await apiFetch<void>('/v1/auth/email/otp/send', {
    method: 'POST',
    body: JSON.stringify({ email, cf_turnstile_token: cfTurnstileToken }),
  })
}

export async function verifyEmailOTP(email: string, code: string): Promise<LoginResponse> {
  return await apiFetch<LoginResponse>('/v1/auth/email/otp/verify', {
    method: 'POST',
    body: JSON.stringify({ email, code }),
  })
}


export type LogoutResponse = {
  ok: boolean
}

export type CaptchaConfigResponse = {
  enabled: boolean
  site_key: string
}

export async function getCaptchaConfig(): Promise<CaptchaConfigResponse> {
  return await apiFetch<CaptchaConfigResponse>('/v1/auth/captcha-config')
}

export async function logout(accessToken: string): Promise<LogoutResponse> {
  return await apiFetch<LogoutResponse>('/v1/auth/logout', {
    method: 'POST',
    accessToken,
  })
}

// Threads API

export type CreateThreadRequest = {
  title?: string
  is_private?: boolean
  mode?: ThreadMode
  project_id?: string
  collaboration_mode?: CollaborationMode
  learning_mode_enabled?: boolean
}

export type ThreadMode = 'chat' | 'work'
export type CollaborationMode = 'default' | 'plan'
export type ThreadGtdBucket = 'inbox' | 'todo' | 'waiting' | 'someday' | 'archived'

export type ThreadResponse = {
  id: string
  account_id: string
  created_by_user_id: string
  mode: ThreadMode
  sidebar_work_folder?: string | null
  sidebar_pinned_at?: string | null
  sidebar_gtd_bucket?: ThreadGtdBucket | null
  title: string | null
  project_id: string
  created_at: string
  active_run_id: string | null
  is_private: boolean
  collaboration_mode: CollaborationMode
  collaboration_mode_revision: number
  learning_mode_enabled: boolean
  title_locked?: boolean
  hidden?: boolean
  updated_at?: string
  parent_thread_id?: string | null
}

export type ThreadRunStateEvent = {
  type: 'thread.run_state'
  thread_id: string
  active_run_id: string | null
  title?: string | null
}

function parseThreadRunStateEvent(data: string): ThreadRunStateEvent | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(data) as unknown
  } catch {
    return null
  }
  if (!parsed || typeof parsed !== 'object') return null
  const value = parsed as Record<string, unknown>
  if (value.type !== 'thread.run_state') return null
  const threadId = value.thread_id
  const activeRunId = value.active_run_id
  const title = value.title
  if (typeof threadId !== 'string' || threadId.length === 0) return null
  if (activeRunId !== null && typeof activeRunId !== 'string') return null
  if (title !== undefined && title !== null && typeof title !== 'string') return null
  const event: ThreadRunStateEvent = {
    type: 'thread.run_state',
    thread_id: threadId,
    active_run_id: activeRunId,
  }
  if (title !== undefined) event.title = title
  return event
}

export async function streamThreadRunStateEvents(
  accessToken: string,
  options: {
    signal?: AbortSignal
    onEvent: (event: ThreadRunStateEvent) => void
  },
): Promise<void> {
  const response = await fetch(buildUrl('/v1/thread-run-state/events'), {
    method: 'GET',
    headers: {
      Accept: 'text/event-stream',
      Authorization: `Bearer ${accessToken}`,
    },
    signal: options.signal,
  })

  if (!response.ok) {
    const headerTraceId = response.headers.get(TRACE_ID_HEADER) ?? undefined
    const payload = await readJsonSafely(response)
    if (payload && typeof payload === 'object') {
      const env = payload as ErrorEnvelope
      const traceId = typeof env.trace_id === 'string' ? env.trace_id : headerTraceId
      const code = typeof env.code === 'string' ? env.code : undefined
      const message =
        typeof env.message === 'string'
          ? env.message
          : `请求失败（HTTP ${response.status}）`
      throw new ApiError({
        status: response.status,
        message,
        code,
        traceId,
        details: env.details,
      })
    }
    throw new ApiError({
      status: response.status,
      message: `请求失败（HTTP ${response.status}）`,
      traceId: headerTraceId,
    })
  }

  if (!response.body) {
    throw new Error('SSE 响应无 body')
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  try {
    while (true) {
      const { value, done } = await reader.read()
      if (done) return
      buffer += decoder.decode(value, { stream: true })
      const { events, remaining } = parseSSEChunk(buffer)
      buffer = remaining
      for (const event of events) {
        if (event.event !== 'thread.run_state' || !event.data) continue
        const parsed = parseThreadRunStateEvent(event.data)
        if (parsed) options.onEvent(parsed)
      }
    }
  } finally {
    reader.releaseLock()
  }
}

export type ProjectWorkspaceStatus = 'active' | 'idle' | 'unavailable'

export type ProjectWorkspace = {
	project_id: string
	workspace_ref: string
	owner_user_id: string
	status: ProjectWorkspaceStatus
	last_used_at: string
	active_session?: {
		session_ref: string
		session_type: string
		state: string
		last_used_at: string
	}
}

export type ProjectWorkspaceFileEntry = {
	name: string
	path: string
	type: 'dir' | 'file' | 'symlink'
	size?: number
	mtime_unix_ms?: number
	mime_type?: string
	has_children?: boolean
}

export type ProjectWorkspaceFilesResponse = {
	workspace_ref: string
	path: string
	items: ProjectWorkspaceFileEntry[]
}

function normalizeWorkspaceQueryPath(pathname?: string): string {
	const trimmed = (pathname ?? '').trim()
	if (!trimmed || trimmed === '/') return '/'
	return trimmed.startsWith('/') ? trimmed : `/${trimmed}`
}

export async function getProjectWorkspace(accessToken: string, projectId: string): Promise<ProjectWorkspace> {
	return await apiFetch<ProjectWorkspace>(`/v1/projects/${projectId}/workspace`, {
		method: 'GET',
		accessToken,
	})
}

export async function listProjectWorkspaceFiles(
	accessToken: string,
	projectId: string,
	pathname = '/',
): Promise<ProjectWorkspaceFilesResponse> {
	const sp = new URLSearchParams()
	const normalizedPath = normalizeWorkspaceQueryPath(pathname)
	if (normalizedPath !== '/') {
		sp.set('path', normalizedPath)
	}
	const query = sp.toString()
	return await apiFetch<ProjectWorkspaceFilesResponse>(`/v1/projects/${projectId}/workspace/files${query ? `?${query}` : ''}`, {
		method: 'GET',
		accessToken,
	})
}

export function buildProjectWorkspaceFileUrl(projectId: string, pathname: string): string {
	const sp = new URLSearchParams({ path: normalizeWorkspaceQueryPath(pathname) })
	return buildUrl(`/v1/projects/${projectId}/workspace/file?${sp.toString()}`)
}

export async function getThread(
  accessToken: string,
  threadId: string,
): Promise<ThreadResponse> {
  return await apiFetch<ThreadResponse>(`/v1/threads/${threadId}`, {
    method: 'GET',
    accessToken,
  })
}

export async function createThread(
  accessToken: string,
  req?: CreateThreadRequest,
): Promise<ThreadResponse> {
  return await apiFetch<ThreadResponse>('/v1/threads', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(req ?? {}),
  })
}

export type ListThreadsRequest = {
  limit?: number
  before_created_at?: string
  before_id?: string
  mode?: ThreadMode
}

export async function listThreads(
  accessToken: string,
  req?: ListThreadsRequest,
): Promise<ThreadResponse[]> {
  const sp = new URLSearchParams()
  if (req?.limit) sp.set('limit', String(req.limit))
  if (req?.before_created_at) sp.set('before_created_at', req.before_created_at)
  if (req?.before_id) sp.set('before_id', req.before_id)
  if (req?.mode) sp.set('mode', req.mode)
  const suffix = sp.toString() ? `?${sp.toString()}` : ''
  return await apiFetch<ThreadResponse[]>(`/v1/threads${suffix}`, {
    method: 'GET',
    accessToken,
  })
}

export async function searchThreads(
  accessToken: string,
  q: string,
  mode?: ThreadMode,
  limit = 50,
): Promise<ThreadResponse[]> {
  const sp = new URLSearchParams({ q, limit: String(limit) })
  if (mode) sp.set('mode', mode)
  return await apiFetch<ThreadResponse[]>(`/v1/threads/search?${sp.toString()}`, {
    method: 'GET',
    accessToken,
  })
}

export async function listStarredThreadIds(accessToken: string): Promise<string[]> {
  return await apiFetch<string[]>('/v1/threads/starred', {
    method: 'GET',
    accessToken,
  })
}

export async function starThread(accessToken: string, threadId: string): Promise<void> {
  await apiFetch<void>(`/v1/threads/${threadId}:star`, {
    method: 'POST',
    accessToken,
  })
}

export async function unstarThread(accessToken: string, threadId: string): Promise<void> {
  await apiFetch<void>(`/v1/threads/${threadId}:star`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function updateThreadTitle(
  accessToken: string,
  threadId: string,
  title: string,
): Promise<ThreadResponse> {
  return await apiFetch<ThreadResponse>(`/v1/threads/${threadId}`, {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify({ title }),
  })
}

export async function updateThreadCollaborationMode(
  accessToken: string,
  threadId: string,
  collaborationMode: CollaborationMode,
): Promise<ThreadResponse> {
  return await apiFetch<ThreadResponse>(`/v1/threads/${threadId}`, {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify({ collaboration_mode: collaborationMode }),
  })
}

export async function updateThreadLearningMode(
  accessToken: string,
  threadId: string,
  enabled: boolean,
): Promise<ThreadResponse> {
  return await apiFetch<ThreadResponse>(`/v1/threads/${threadId}`, {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify({ learning_mode_enabled: enabled }),
  })
}

export async function updateThreadMode(
  accessToken: string,
  threadId: string,
  mode: ThreadMode,
): Promise<ThreadResponse> {
  return await apiFetch<ThreadResponse>(`/v1/threads/${threadId}`, {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify({ mode }),
  })
}

export type UpdateThreadSidebarRequest = {
  sidebar_work_folder?: string | null
  sidebar_pinned?: boolean
  sidebar_gtd_bucket?: ThreadGtdBucket | null
}

export async function updateThreadSidebarState(
  accessToken: string,
  threadId: string,
  req: UpdateThreadSidebarRequest,
): Promise<ThreadResponse> {
  return await apiFetch<ThreadResponse>(`/v1/threads/${threadId}`, {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify(req),
  })
}

export async function deleteThread(accessToken: string, threadId: string): Promise<void> {
  await apiFetch<void>(`/v1/threads/${threadId}`, {
    method: 'DELETE',
    accessToken,
  })
}

export type ForkThreadResponse = ThreadResponse & {
  id_mapping?: Array<{ old_id: string; new_id: string }>
}

export async function forkThread(
  accessToken: string,
  threadId: string,
  messageId: string,
  isPrivate?: boolean,
): Promise<ForkThreadResponse> {
  const body: Record<string, unknown> = { message_id: messageId }
  if (isPrivate !== undefined) body.is_private = isPrivate
  return await apiFetch<ForkThreadResponse>(`/v1/threads/${threadId}:fork`, {
    method: 'POST',
    accessToken,
    body: JSON.stringify(body),
  })
}

// Messages API

export type MessageAttachmentRef = {
  key: string
  filename: string
  mime_type: string
  size: number
}

export type MessageContentPart =
  | { type: 'text'; text: string }
  | { type: 'image'; attachment: MessageAttachmentRef }
  | { type: 'file'; attachment: MessageAttachmentRef; extracted_text: string }

export type MessageContent = {
  parts: MessageContentPart[]
}

export type CreateMessageRequest = {
  content?: string
  content_json?: MessageContent
}

export type MessageResponse = {
  id: string
  account_id: string
  thread_id: string
  created_by_user_id: string
  role: string
  content: string
  content_json?: MessageContent
  created_at: string
  run_id?: string
}

export type UploadedThreadAttachment = {
  key: string
  filename: string
  mime_type: string
  size: number
  kind: 'image' | 'file'
  extracted_text?: string
}

export async function uploadStagingAttachment(
  accessToken: string,
  file: File,
): Promise<UploadedThreadAttachment> {
  const body = new FormData()
  body.append('file', file)
  return await apiFetch<UploadedThreadAttachment>('/v1/attachments/stage', {
    method: 'POST',
    accessToken,
    body,
    headers: {},
  })
}

export async function createMessage(
  accessToken: string,
  threadId: string,
  req: CreateMessageRequest,
): Promise<MessageResponse> {
  return await apiFetch<MessageResponse>(`/v1/threads/${threadId}/messages`, {
    method: 'POST',
    accessToken,
    body: JSON.stringify(req),
  })
}

export async function listMessages(
  accessToken: string,
  threadId: string,
  limit = 200,
): Promise<MessageResponse[]> {
  return await apiFetch<MessageResponse[]>(
    `/v1/threads/${threadId}/messages?limit=${limit}`,
    {
      method: 'GET',
      accessToken,
    },
  )
}

export async function editMessage(
  accessToken: string,
  threadId: string,
  messageId: string,
  content: string,
  contentJson?: MessageContent,
  personaId?: string,
  modelOverride?: string,
  workDir?: string,
  reasoningMode?: RunReasoningMode,
): Promise<CreateRunResponse> {
  return await apiFetch<CreateRunResponse>(`/v1/threads/${threadId}/messages/${messageId}`, {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify({
      content,
      content_json: contentJson,
      ...(personaId ? { persona_id: personaId } : {}),
      ...(modelOverride ? { model: modelOverride } : {}),
      ...(workDir ? { work_dir: workDir } : {}),
      ...(reasoningMode ? { reasoning_mode: reasoningMode } : {}),
    }),
  })
}

export async function retryMessage(
  accessToken: string,
  threadId: string,
  messageId: string,
  personaId?: string,
  modelOverride?: string,
  workDir?: string,
  reasoningMode?: RunReasoningMode,
): Promise<CreateRunResponse> {
  return await apiFetch<CreateRunResponse>(`/v1/threads/${threadId}/messages/${messageId}:retry`, {
    method: 'POST',
    accessToken,
    body: JSON.stringify({
      ...(personaId ? { persona_id: personaId } : {}),
      ...(modelOverride ? { model: modelOverride } : {}),
      ...(workDir ? { work_dir: workDir } : {}),
      ...(reasoningMode ? { reasoning_mode: reasoningMode } : {}),
    }),
  })
}

// Runs API

export type CreateRunResponse = {
  run_id: string
  trace_id: string
}

export type RunReasoningMode =
  | 'auto'
  | 'enabled'
  | 'disabled'
  | 'none'
  | 'off'
  | 'minimal'
  | 'low'
  | 'medium'
  | 'high'
  | 'max'
  | 'xhigh'

export type CreateRunOptions = {
  resumeFromRunId?: string | null
}

export async function createRun(
  accessToken: string,
  threadId: string,
  personaId?: string,
  modelOverride?: string,
  workDir?: string,
  reasoningMode?: RunReasoningMode,
  options?: CreateRunOptions,
): Promise<CreateRunResponse> {
  const resumeFromRunId = options?.resumeFromRunId?.trim()
  const hasBody = personaId || modelOverride || workDir || reasoningMode || resumeFromRunId
  return await apiFetch<CreateRunResponse>(`/v1/threads/${threadId}/runs`, {
    method: 'POST',
    accessToken,
    body: hasBody
      ? JSON.stringify({
          ...(personaId ? { persona_id: personaId } : {}),
          ...(modelOverride ? { model: modelOverride } : {}),
          ...(workDir ? { work_dir: workDir } : {}),
          ...(reasoningMode ? { reasoning_mode: reasoningMode } : {}),
          ...(resumeFromRunId ? { resume_from_run_id: resumeFromRunId } : {}),
        })
      : undefined,
  })
}

export type ThreadRunResponse = {
  run_id: string
  status: 'running' | 'cancelling' | 'completed' | 'failed' | 'cancelled' | 'interrupted'
  created_at: string
  resume_from_run_id?: string | null
}

export async function listThreadRuns(
  accessToken: string,
  threadId: string,
  limit = 50,
): Promise<ThreadRunResponse[]> {
  return await apiFetch<ThreadRunResponse[]>(
    `/v1/threads/${threadId}/runs?limit=${limit}`,
    {
      method: 'GET',
      accessToken,
    },
  )
}

export async function listRunEvents(
  accessToken: string,
  runId: string,
  options?: { afterSeq?: number; follow?: boolean },
): Promise<RunEvent[]> {
  const sp = new URLSearchParams()
  sp.set('follow', options?.follow === true ? 'true' : 'false')
  sp.set('after_seq', String(options?.afterSeq ?? 0))

  const response = await fetch(buildUrl(`/v1/runs/${runId}/events?${sp.toString()}`), {
    method: 'GET',
    headers: {
      Accept: 'text/event-stream',
      Authorization: `Bearer ${accessToken}`,
    },
  })

  if (!response.ok) {
    const headerTraceId = response.headers.get(TRACE_ID_HEADER) ?? undefined
    const payload = await readJsonSafely(response)
    if (payload && typeof payload === 'object') {
      const env = payload as ErrorEnvelope
      const traceId = typeof env.trace_id === 'string' ? env.trace_id : headerTraceId
      const code = typeof env.code === 'string' ? env.code : undefined
      const message =
        typeof env.message === 'string'
          ? env.message
          : `请求失败（HTTP ${response.status}）`
      throw new ApiError({
        status: response.status,
        message,
        code,
        traceId,
        details: env.details,
      })
    }
    throw new ApiError({
      status: response.status,
      message: `请求失败（HTTP ${response.status}）`,
      traceId: headerTraceId,
    })
  }

  const text = await response.text()
  if (text.trim() === '') return []

  const { events } = parseSSEChunk(text.endsWith('\n') ? text : `${text}\n`)
  const runEvents: RunEvent[] = []
  for (const event of events) {
    if (!event.data) continue
    try {
      runEvents.push(JSON.parse(event.data) as RunEvent)
    } catch {
      // ignore malformed item
    }
  }
  return runEvents.sort((left, right) => left.seq - right.seq)
}

export type RunPipelineEvent = {
  event_id: string
  run_id: string
  seq: number
  ts: string
  type: string
  data: Record<string, unknown>
  middleware?: string
}

export async function listRunPipelineEvents(
  accessToken: string,
  runId: string,
): Promise<RunPipelineEvent[]> {
  return await apiFetch<RunPipelineEvent[]>(`/v1/admin/runs/${runId}/pipeline-events`, {
    accessToken,
  })
}

export type CancelRunResponse = {
  ok: boolean
}

export async function cancelRun(
  accessToken: string,
  runId: string,
  lastSeenSeq?: number,
): Promise<CancelRunResponse> {
  const normalizedSeq = typeof lastSeenSeq === 'number' && Number.isFinite(lastSeenSeq)
    ? Math.max(0, lastSeenSeq)
    : 0
  return await apiFetch<CancelRunResponse>(`/v1/runs/${runId}:cancel`, {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ last_seen_seq: normalizedSeq }),
  })
}

export type ProvideInputResponse = {
  ok: boolean
}

export async function provideInput(
  accessToken: string,
  runId: string,
  content: string,
): Promise<ProvideInputResponse> {
  return await apiFetch<ProvideInputResponse>(`/v1/runs/${runId}/input`, {
    method: 'POST',
    body: JSON.stringify({ content }),
    accessToken,
  })
}

// Credits API

export type CreditTransaction = {
  id: string
  account_id: string
  amount: number
  type: string
  reference_type?: string
  reference_id?: string
  note?: string
  thread_title?: string
  created_at: string
}

export type MeCreditsResponse = {
  balance: number
  transactions: CreditTransaction[]
}

export async function getMyCredits(
  accessToken: string,
  from?: string,
  to?: string,
): Promise<MeCreditsResponse> {
  const params = new URLSearchParams()
  if (from) params.set('from', from)
  if (to) params.set('to', to)
  const qs = params.size > 0 ? `?${params.toString()}` : ''
  return await apiFetch<MeCreditsResponse>(`/v1/me/credits${qs}`, {
    method: 'GET',
    accessToken,
  })
}

export type MeUsageSummary = {
  account_id: string
  year: number
  month: number
  total_input_tokens: number
  total_output_tokens: number
  total_cache_creation_tokens: number
  total_cache_read_tokens: number
  total_cached_tokens: number
  total_cost_usd: number
  record_count: number
}

export async function getMyUsage(
  accessToken: string,
  year: number,
  month: number,
): Promise<MeUsageSummary> {
  return await apiFetch<MeUsageSummary>(`/v1/me/usage?year=${year}&month=${month}`, {
    method: 'GET',
    accessToken,
  })
}

export type MeDailyUsageItem = {
  date: string
  input_tokens: number
  output_tokens: number
  cost_usd: number
  record_count: number
}

export async function getMyDailyUsage(
  accessToken: string,
  start: string,
  end: string,
): Promise<MeDailyUsageItem[]> {
  const query = new URLSearchParams({ start, end }).toString()
  return await apiFetch<MeDailyUsageItem[]>(`/v1/me/usage/daily?${query}`, {
    method: 'GET',
    accessToken,
  })
}

export type MeHourlyUsageItem = {
  hour: string
  input_tokens: number
  output_tokens: number
  cost_usd: number
  record_count: number
}

export async function getMyHourlyUsage(
  accessToken: string,
  start: string,
  end: string,
): Promise<MeHourlyUsageItem[]> {
  const query = new URLSearchParams({ start, end }).toString()
  return await apiFetch<MeHourlyUsageItem[]>(`/v1/me/usage/hourly?${query}`, {
    method: 'GET',
    accessToken,
  })
}

export type MeModelUsageItem = {
  model: string
  input_tokens: number
  output_tokens: number
  cache_creation_tokens: number
  cache_read_tokens: number
  cached_tokens: number
  cost_usd: number
  record_count: number
}

export async function getMyUsageByModel(
  accessToken: string,
  year: number,
  month: number,
): Promise<MeModelUsageItem[]> {
  return await apiFetch<MeModelUsageItem[]>(`/v1/me/usage/by-model?year=${year}&month=${month}`, {
    method: 'GET',
    accessToken,
  })
}

export type RedeemCodeResponse = {
  code: string
  type: string
  value: string
}

export async function redeemCode(
  accessToken: string,
  code: string,
): Promise<RedeemCodeResponse> {
  return await apiFetch<RedeemCodeResponse>('/v1/me/redeem', {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ code }),
  })
}

// Invite Code API

export type InviteCodeResponse = {
  id: string
  user_id: string
  code: string
  max_uses: number
  use_count: number
  is_active: boolean
  created_at: string
}

export async function getMyInviteCode(
  accessToken: string,
): Promise<InviteCodeResponse> {
  return await apiFetch<InviteCodeResponse>('/v1/me/invite-code', {
    method: 'GET',
    accessToken,
  })
}

export async function resetMyInviteCode(
  accessToken: string,
): Promise<InviteCodeResponse> {
  return await apiFetch<InviteCodeResponse>('/v1/me/invite-code/reset', {
    method: 'POST',
    accessToken,
  })
}

// Notifications API

export type NotificationItem = {
  id: string
  user_id: string
  account_id: string
  type: string
  title: string
  body: string
  payload: Record<string, unknown>
  read_at?: string
  created_at: string
}

export async function listNotifications(
  accessToken: string,
  opts?: { unreadOnly?: boolean; type?: string },
): Promise<{ data: NotificationItem[] }> {
  const params = new URLSearchParams()
  if (opts?.unreadOnly) params.set('unread_only', 'true')
  if (opts?.type) params.set('type', opts.type)
  const query = params.toString()
  return await apiFetch<{ data: NotificationItem[] }>(`/v1/notifications${query ? `?${query}` : ''}`, {
    method: 'GET',
    accessToken,
  })
}

export async function markAllNotificationsRead(
  accessToken: string,
): Promise<{ ok: boolean; count: number }> {
  return await apiFetch<{ ok: boolean; count: number }>('/v1/notifications', {
    method: 'PATCH',
    accessToken,
  })
}

export async function markNotificationRead(
  accessToken: string,
  id: string,
): Promise<{ ok: boolean }> {
  return await apiFetch<{ ok: boolean }>(`/v1/notifications/${id}`, {
    method: 'PATCH',
    accessToken,
  })
}

// ASR Credentials

export type AsrCredential = {
  id: string
  account_id: string | null
  scope: string
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

export interface UpdateAsrCredentialRequest {
  name?: string
  base_url?: string
  model?: string
  is_default?: boolean
}

export async function updateAsrCredential(
  id: string,
  req: UpdateAsrCredentialRequest,
  accessToken: string,
): Promise<AsrCredential> {
  return apiFetch<AsrCredential>(`/v1/asr-credentials/${id}`, {
    method: 'PUT',
    body: JSON.stringify(req),
    accessToken,
  })
}

export async function setDefaultAsrCredential(id: string, accessToken: string): Promise<{ ok: boolean }> {
  return apiFetch<{ ok: boolean }>(`/v1/asr-credentials/${id}/set-default`, {
    method: 'POST',
    accessToken,
  })
}

export async function transcribeAudio(
  accessToken: string,
  audioBlob: Blob,
  filename: string,
  language?: string,
): Promise<{ text: string }> {
  const form = new FormData()
  form.append('file', audioBlob, filename)
  if (language) form.append('language', language)

  const base = apiBaseUrl()
  const url = base ? `${base}/v1/asr/transcribe` : `/v1/asr/transcribe`

  const headers = new Headers()
  headers.set('Accept', 'application/json')
  headers.set('Authorization', `Bearer ${accessToken}`)

  const response = await fetch(url, { method: 'POST', body: form, headers })
  if (!response.ok) {
    const headerTraceId = response.headers.get(TRACE_ID_HEADER) ?? undefined
    const payload = await readJsonSafely(response)
    const env = payload && typeof payload === 'object' ? (payload as ErrorEnvelope) : null
    throw new ApiError({
      status: response.status,
      message: typeof env?.message === 'string' ? env.message : `转写失败（HTTP ${response.status}）`,
      traceId: headerTraceId,
    })
  }
  return response.json() as Promise<{ text: string }>
}

// Share API

export type ShareResponse = {
  id: string
  token: string
  url: string
  access_type: 'public' | 'password'
  password?: string
  live_update: boolean
  snapshot_turn_count: number
  created_at: string
}

export async function createThreadShare(
  accessToken: string,
  threadId: string,
  accessType: 'public' | 'password',
  password?: string,
  liveUpdate?: boolean,
): Promise<ShareResponse> {
  return await apiFetch<ShareResponse>(`/v1/threads/${threadId}:share`, {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ access_type: accessType, password, live_update: liveUpdate }),
  })
}

export async function listThreadShares(
  accessToken: string,
  threadId: string,
): Promise<ShareResponse[]> {
  return await apiFetch<ShareResponse[]>(`/v1/threads/${threadId}:share`, {
    method: 'GET',
    accessToken,
  })
}

export async function deleteThreadShare(
  accessToken: string,
  threadId: string,
  shareId: string,
): Promise<void> {
  await apiFetch<void>(`/v1/threads/${threadId}:share?id=${shareId}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function createThreadReport(
  accessToken: string,
  threadId: string,
  categories: string[],
  feedback?: string,
): Promise<void> {
  await apiFetch<void>(`/v1/threads/${threadId}:report`, {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ categories, feedback: feedback || undefined }),
  })
}

export async function createSuggestionFeedback(
  accessToken: string,
  feedback: string,
): Promise<void> {
  await apiFetch<void>('/v1/me/feedback', {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ feedback }),
  })
}

export type SharedThreadResponse = {
  requires_password: boolean
  thread?: {
    title: string | null
    created_at: string
  }
  messages?: Array<{
    id: string
    role: string
    content: string
    content_json?: MessageContent
    created_at: string
  }>
}

export async function getSharedThread(
  token: string,
  sessionToken?: string,
): Promise<SharedThreadResponse> {
  const params = new URLSearchParams()
  if (sessionToken) params.set('session_token', sessionToken)
  const qs = params.toString()
  return await apiFetch<SharedThreadResponse>(`/v1/s/${token}${qs ? `?${qs}` : ''}`)
}

export type VerifyShareResponse = {
  session_token: string
}

export async function verifySharePassword(
  token: string,
  password: string,
): Promise<VerifyShareResponse> {
  return await apiFetch<VerifyShareResponse>(`/v1/s/${token}/verify`, {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
}

// LLM Providers API (BYOK)

export type LlmProviderModel = {
  id: string
  provider_id: string
  model: string
  priority: number
  is_default: boolean
  show_in_picker: boolean
  tags: string[]
  when: Record<string, unknown>
  advanced_json?: Record<string, unknown> | null
  multiplier: number
  cost_per_1k_input?: number | null
  cost_per_1k_output?: number | null
  cost_per_1k_cache_write?: number | null
  cost_per_1k_cache_read?: number | null
}

export type LlmProvider = {
  id: string
  account_id?: string | null
  scope: string
  provider: string
  name: string
  key_prefix: string | null
  base_url: string | null
  openai_api_mode: string | null
  advanced_json?: Record<string, unknown> | null
  created_at: string
  models: LlmProviderModel[]
}

export type CreateLlmProviderRequest = {
  scope?: string
  name: string
  provider: string
  api_key: string
  base_url?: string
  openai_api_mode?: string
  advanced_json?: Record<string, unknown>
}

export type UpdateLlmProviderRequest = {
  scope?: string
  name?: string
  provider?: string
  api_key?: string
  base_url?: string | null
  openai_api_mode?: string | null
  advanced_json?: Record<string, unknown> | null
}

export type CreateModelRequest = {
  scope?: string
  model: string
  priority?: number
  is_default?: boolean
  show_in_picker?: boolean
  tags?: string[]
  /** worker compact 只认 advanced_json.available_catalog.context_length */
  advanced_json?: Record<string, unknown>
}

export {
  AVAILABLE_CATALOG_ADVANCED_KEY,
  routeAdvancedJsonFromAvailableCatalog,
} from '@arkloop/shared/llm/available-catalog-advanced-json'

export type AvailableModel = {
  id: string
  name: string
  configured: boolean
  type?: string
  context_length?: number | null
  max_output_tokens?: number | null
  tool_calling?: boolean | null
  reasoning?: boolean | null
  default_temperature?: number | null
  input_modalities?: string[]
  output_modalities?: string[]
}

export type TestLlmProviderModelResponse = {
  success: boolean
  latency_ms?: number | null
  error?: string
}

const BYOK_SCOPE = 'user'

function withScope(path: string, scope: string): string {
  const sep = path.includes('?') ? '&' : '?'
  return `${path}${sep}scope=${scope}`
}

export async function listLlmProviders(accessToken: string): Promise<LlmProvider[]> {
  return await apiFetch<LlmProvider[]>(withScope('/v1/llm-providers', BYOK_SCOPE), {
    method: 'GET',
    accessToken,
  })
}

export async function createLlmProvider(
  accessToken: string,
  req: CreateLlmProviderRequest,
): Promise<LlmProvider> {
  return await apiFetch<LlmProvider>(withScope('/v1/llm-providers', BYOK_SCOPE), {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ ...req, scope: BYOK_SCOPE }),
  })
}

export async function updateLlmProvider(
  accessToken: string,
  id: string,
  req: UpdateLlmProviderRequest,
): Promise<LlmProvider> {
  return await apiFetch<LlmProvider>(withScope(`/v1/llm-providers/${id}`, BYOK_SCOPE), {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify({ ...req, scope: BYOK_SCOPE }),
  })
}

export async function deleteLlmProvider(
  accessToken: string,
  id: string,
): Promise<{ ok: boolean }> {
  return await apiFetch<{ ok: boolean }>(withScope(`/v1/llm-providers/${id}`, BYOK_SCOPE), {
    method: 'DELETE',
    accessToken,
  })
}

export async function createProviderModel(
  accessToken: string,
  providerId: string,
  req: CreateModelRequest,
): Promise<LlmProviderModel> {
  return await apiFetch<LlmProviderModel>(
    withScope(`/v1/llm-providers/${providerId}/models`, BYOK_SCOPE),
    {
      method: 'POST',
      accessToken,
      body: JSON.stringify({ ...req, scope: BYOK_SCOPE }),
    },
  )
}

export async function deleteProviderModel(
  accessToken: string,
  providerId: string,
  modelId: string,
): Promise<{ ok: boolean }> {
  return await apiFetch<{ ok: boolean }>(
    withScope(`/v1/llm-providers/${providerId}/models/${modelId}`, BYOK_SCOPE),
    {
      method: 'DELETE',
      accessToken,
    },
  )
}

export async function patchProviderModel(
  accessToken: string,
  providerId: string,
  modelId: string,
  data: { show_in_picker?: boolean; is_default?: boolean; tags?: string[]; advanced_json?: Record<string, unknown> | null },
): Promise<LlmProviderModel> {
  return await apiFetch<LlmProviderModel>(
    withScope(`/v1/llm-providers/${providerId}/models/${modelId}`, BYOK_SCOPE),
    {
      method: 'PATCH',
      accessToken,
      body: JSON.stringify({ ...data, scope: BYOK_SCOPE }),
    },
  )
}

export async function listAvailableModels(
  accessToken: string,
  providerId: string,
): Promise<{ models: AvailableModel[] }> {
  return await apiFetch<{ models: AvailableModel[] }>(
    withScope(`/v1/llm-providers/${providerId}/available-models`, BYOK_SCOPE),
    {
      method: 'GET',
      accessToken,
    },
  )
}

export async function testLlmProviderModel(
  accessToken: string,
  providerId: string,
  modelId: string,
): Promise<TestLlmProviderModelResponse> {
  return await apiFetch<TestLlmProviderModelResponse>(
    withScope(`/v1/llm-providers/${providerId}/models/${modelId}/test`, BYOK_SCOPE),
    {
      method: 'POST',
      accessToken,
    },
  )
}

export type PatchPersonaRequest = {
  model?: string
  reasoning_mode?: string
  stream_thinking?: boolean
  preferred_credential?: string
  budgets?: Record<string, unknown>
}

export async function patchPersona(
  accessToken: string,
  personaId: string,
  req: PatchPersonaRequest,
  scope?: string,
): Promise<Persona> {
  const qs = scope ? `?scope=${scope}` : ''
  return await apiFetch<Persona>(`/v1/personas/${personaId}${qs}`, {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify(req),
  })
}

export type RunDetail = {
  run_id: string
  thread_id: string
  status: string
  model?: string
  persona_id?: string
  total_input_tokens?: number
  total_output_tokens?: number
  total_cost_usd?: number
  duration_ms?: number
  cache_hit_rate?: number
  credits_used?: number
  created_at: string
  completed_at?: string
  failed_at?: string
  created_by_user_id?: string
  created_by_user_name?: string
  created_by_email?: string
  user_prompt?: string
  thread_messages?: MessageResponse[]
}

export async function getRunDetail(
  accessToken: string,
  runId: string,
): Promise<RunDetail> {
  return await apiFetch<RunDetail>(`/v1/admin/runs/${runId}`, { accessToken })
}

export type Run = {
  run_id: string
  thread_id: string
  status: string
  model?: string
  persona_id?: string
  total_input_tokens?: number
  total_output_tokens?: number
  total_cost_usd?: number
  duration_ms?: number
  cache_hit_rate?: number
  credits_used?: number
  created_at: string
  completed_at?: string
  failed_at?: string
  created_by_user_name?: string
  created_by_email?: string
}

export type ListRunsResponse = {
  data: Run[]
  total: number
}

export async function listRuns(
  accessToken: string,
  params: { limit?: number; offset?: number } = {},
): Promise<ListRunsResponse> {
  const qs = new URLSearchParams()
  if (params.limit != null) qs.set('limit', String(params.limit))
  if (params.offset != null) qs.set('offset', String(params.offset))
  const query = qs.toString()
  return apiFetch<ListRunsResponse>(`/v1/runs${query ? `?${query}` : ''}`, { accessToken })
}

export type SpawnProfile = {
  profile: string
  resolved_model: string
  has_override: boolean
  is_auto?: boolean
  auto_model?: string
}

export type ResolveOpenVikingConfigRequest = {
  vlm_selector?: string
  embedding_selector?: string
  embedding_dimension_hint?: number
  rerank_selector?: string
}

export type ResolvedOpenVikingModel = {
  selector: string
  credential_name: string
  provider: string
  model: string
  api_base: string
  api_key: string
  extra_headers?: Record<string, string>
}

export type ResolvedOpenVikingEmbedding = ResolvedOpenVikingModel & {
  dimension: number
}

export type ResolveOpenVikingConfigResponse = {
  vlm?: ResolvedOpenVikingModel
  embedding?: ResolvedOpenVikingEmbedding
  rerank?: ResolvedOpenVikingModel
}

export type AccountSettingsResponse = {
  pipeline_trace_enabled: boolean
  prompt_cache_debug_enabled: boolean
}

export async function listSpawnProfiles(accessToken: string): Promise<SpawnProfile[]> {
  return apiFetch<SpawnProfile[]>('/v1/accounts/me/spawn-profiles', { accessToken })
}

export async function setSpawnProfile(accessToken: string, name: string, model: string): Promise<void> {
  await apiFetch<void>(`/v1/accounts/me/spawn-profiles/${name}`, {
    method: 'PUT',
    accessToken,
    body: JSON.stringify({ model }),
  })
}

export async function deleteSpawnProfile(accessToken: string, name: string): Promise<void> {
  await apiFetch<void>(`/v1/accounts/me/spawn-profiles/${name}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function resolveOpenVikingConfig(
  accessToken: string,
  req: ResolveOpenVikingConfigRequest,
): Promise<ResolveOpenVikingConfigResponse> {
  return apiFetch<ResolveOpenVikingConfigResponse>('/v1/account/openviking/resolve', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(req),
  })
}

export async function getAccountSettings(accessToken: string): Promise<AccountSettingsResponse> {
  return apiFetch<AccountSettingsResponse>('/v1/account/settings', {
    method: 'GET',
    accessToken,
  })
}

export async function updateAccountSettings(
  accessToken: string,
  settings: Partial<AccountSettingsResponse>,
): Promise<AccountSettingsResponse> {
  return apiFetch<AccountSettingsResponse>('/v1/account/settings', {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify(settings),
  })
}

export type MemoryErrorEvent = {
  event_id: string
  run_id: string
  ts: string
  type: string
  data: Record<string, unknown> | null
}

export type MemoryErrorsResponse = {
  errors: MemoryErrorEvent[]
  total: number
}

export async function listMemoryErrors(
  accessToken: string,
  limit = 20,
): Promise<MemoryErrorsResponse> {
  return apiFetch<MemoryErrorsResponse>(`/v1/account/memory/errors?limit=${limit}`, {
    method: 'GET',
    accessToken,
  })
}

// --- Channels ---

export type ChannelResponse = {
  id: string
  account_id: string
  channel_type: string
  persona_id: string | null
  webhook_url: string | null
  is_active: boolean
  config_json: Record<string, unknown>
  has_credentials?: boolean
  created_at: string
  updated_at: string
}

export type CreateChannelRequest = {
  channel_type: string
  bot_token: string
  persona_id?: string
  config_json?: Record<string, unknown>
}

export type UpdateChannelRequest = {
  bot_token?: string
  persona_id?: string | null
  is_active?: boolean
  config_json?: Record<string, unknown>
}

export async function createChannel(accessToken: string, req: CreateChannelRequest): Promise<ChannelResponse> {
  return apiFetch<ChannelResponse>('/v1/channels', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(req),
  })
}

export async function listChannels(accessToken: string): Promise<ChannelResponse[]> {
  return apiFetch<ChannelResponse[]>('/v1/channels', { accessToken })
}

export async function updateChannel(accessToken: string, id: string, req: UpdateChannelRequest): Promise<ChannelResponse> {
  return apiFetch<ChannelResponse>(`/v1/channels/${id}`, {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify(req),
  })
}

export async function deleteChannel(accessToken: string, id: string): Promise<void> {
  await apiFetch<void>(`/v1/channels/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}

export type ChannelVerifyResponse = {
  ok: boolean
  bot_username?: string
  bot_user_id?: string
  application_name?: string
  application_id?: string
  error?: string
}

export async function verifyChannel(accessToken: string, id: string): Promise<ChannelVerifyResponse> {
  return apiFetch<ChannelVerifyResponse>(`/v1/channels/${id}/verify`, {
    method: 'POST',
    accessToken,
  })
}

// --- Channel Binds ---

export type BindCodeResponse = {
  id: string
  token: string
  channel_type: string | null
  expires_at: string
  created_at: string
}

export type ChannelIdentityResponse = {
  id: string
  channel_type: string
  platform_subject_id: string
  display_name: string | null
  avatar_url: string | null
  metadata: Record<string, unknown>
  created_at: string
}

export type ChannelBindingResponse = {
  binding_id: string
  channel_identity_id: string
  display_name: string | null
  platform_subject_id: string
  is_owner: boolean
  heartbeat_enabled: boolean
  heartbeat_interval_minutes: number
  heartbeat_model: string | null
}

export type UpdateChannelBindingRequest = {
  make_owner?: boolean
  heartbeat_enabled?: boolean
  heartbeat_interval_minutes?: number
  heartbeat_model?: string | null
}

export async function createChannelBindCode(accessToken: string, channelType?: string): Promise<BindCodeResponse> {
  return apiFetch<BindCodeResponse>('/v1/me/channel-binds', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(channelType ? { channel_type: channelType } : {}),
  })
}

export async function listMyChannelIdentities(accessToken: string): Promise<ChannelIdentityResponse[]> {
  return apiFetch<ChannelIdentityResponse[]>('/v1/me/channel-identities', { accessToken })
}

export async function unbindChannelIdentity(accessToken: string, id: string): Promise<void> {
  await apiFetch<void>(`/v1/me/channel-identities/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function listChannelBindings(accessToken: string, channelID: string): Promise<ChannelBindingResponse[]> {
  return apiFetch<ChannelBindingResponse[]>(`/v1/channels/${channelID}/bindings`, {
    accessToken,
  })
}

export async function updateChannelBinding(
  accessToken: string,
  channelID: string,
  bindingID: string,
  req: UpdateChannelBindingRequest,
): Promise<ChannelBindingResponse> {
  return apiFetch<ChannelBindingResponse>(`/v1/channels/${channelID}/bindings/${bindingID}`, {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify(req),
  })
}

export async function deleteChannelBinding(accessToken: string, channelID: string, bindingID: string): Promise<void> {
  await apiFetch<void>(`/v1/channels/${channelID}/bindings/${bindingID}`, {
    method: 'DELETE',
    accessToken,
  })
}

// --- NapCat (QQ channel) ---

export type NapCatStatus = {
  platform: string
  installed: boolean
  running: boolean
  logged_in: boolean
  qq?: string
  nickname?: string
  qrcode_url?: string
  qrcode_text_url?: string
  login_error?: string
  quick_login_list?: { uin: string; nickname: string; face_url: string }[]
  version?: string
  setup_phase?: string
  setup_progress?: number
  setup_total?: number
  setup_error?: string
  logs?: string[]
  onebot_ws_url?: string
  onebot_http_url?: string
}

export async function getNapCatStatus(accessToken: string): Promise<NapCatStatus> {
  return apiFetch<NapCatStatus>('/v1/napcat/status', { accessToken })
}

export async function napCatDownload(accessToken: string): Promise<void> {
  await apiFetch<void>('/v1/napcat/download', { method: 'POST', accessToken })
}

export async function napCatStart(accessToken: string): Promise<void> {
  await apiFetch<void>('/v1/napcat/start', { method: 'POST', accessToken })
}

export async function napCatStop(accessToken: string): Promise<void> {
  await apiFetch<void>('/v1/napcat/stop', { method: 'POST', accessToken })
}

export async function napCatRefreshQR(accessToken: string): Promise<void> {
  await apiFetch<void>('/v1/napcat/refresh-qr', { method: 'POST', accessToken })
}

export async function napCatFetchQRCode(accessToken: string): Promise<string> {
  const headers: HeadersInit = { Authorization: `Bearer ${accessToken}` }
  const resp = await fetch(buildUrl('/v1/napcat/qrcode.png'), { headers, credentials: 'include' })
  if (!resp.ok) throw new Error(`qrcode fetch failed: ${resp.status}`)
  const blob = await resp.blob()
  return URL.createObjectURL(blob)
}

export async function napCatQuickLogin(accessToken: string, uin: string): Promise<void> {
  await apiFetch<void>('/v1/napcat/quick-login', {
    method: 'POST',
    accessToken,
    body: JSON.stringify({ uin }),
  })
}

// --- WeChat (weixin channel) ---

export interface WeixinQRCodeResponse {
  qrcode: string
  qrcode_img_content: string // base64 PNG
}

export interface WeixinQRCodeStatusResponse {
  status: string // "pending" | "confirmed" | "expired"
  bot_token?: string
  baseurl?: string
}

export async function getWeixinQRCode(accessToken: string): Promise<WeixinQRCodeResponse> {
  return apiFetch<WeixinQRCodeResponse>('/v1/weixin/qrcode', {
    accessToken,
  })
}

export async function getWeixinQRCodeStatus(accessToken: string, qrcode: string): Promise<WeixinQRCodeStatusResponse> {
  return apiFetch<WeixinQRCodeStatusResponse>(`/v1/weixin/qrcode-status?qrcode=${encodeURIComponent(qrcode)}`, {
    accessToken,
  })
}

// external skills

export interface ExternalSkill {
  name: string
  path: string
  instruction_path: string
}

export interface ExternalSkillDir {
  path: string
  skills: ExternalSkill[]
}

export async function discoverExternalSkills(
  accessToken: string,
): Promise<{ dirs: ExternalSkillDir[] }> {
  return await apiFetch<{ dirs: ExternalSkillDir[] }>(
    '/v1/external-skills/discover',
    { method: 'GET', accessToken },
  )
}

export async function getExternalDirs(accessToken: string): Promise<string[]> {
  const res = await apiFetch<{ value: string }>(
    '/v1/admin/platform-settings/skills.external_dirs',
    { method: 'GET', accessToken },
  )
  try { return JSON.parse(res.value) as string[] } catch { return [] }
}

export async function setExternalDirs(accessToken: string, dirs: string[]): Promise<void> {
  await apiFetch<void>(
    '/v1/admin/platform-settings/skills.external_dirs',
    { method: 'PUT', accessToken, body: JSON.stringify({ value: JSON.stringify(dirs) }) },
  )
}

export type MCPInstall = {
  id: string
  install_key: string
  account_id: string
  profile_ref: string
  display_name: string
  source_kind: string
  source_uri?: string
  sync_mode: string
  transport: 'stdio' | 'http_sse' | 'streamable_http'
  launch_spec: Record<string, unknown>
  has_auth: boolean
  host_requirement: string
  discovery_status: string
  last_error_code?: string
  last_error_message?: string
  last_checked_at?: string
  created_at: string
  updated_at: string
  workspace_state?: {
    workspace_ref: string
    enabled: boolean
    enabled_at?: string
  }
}

export type MCPDiscoveryProposal = {
  install_key: string
  display_name: string
  transport: 'stdio' | 'http_sse' | 'streamable_http'
  launch_spec: Record<string, unknown>
  host_requirement: string
  has_auth?: boolean
}

export type MCPDiscoverySource = {
  source_uri: string
  source_kind: string
  installable: boolean
  validation_errors: string[]
  host_warnings: string[]
  proposed_installs: MCPDiscoveryProposal[]
}

export type UpsertMCPInstallRequest = {
  install_key?: string
  display_name?: string
  transport?: 'stdio' | 'http_sse' | 'streamable_http'
  launch_spec?: Record<string, unknown>
  auth_headers?: Record<string, string>
  env_secrets?: Record<string, string>
  bearer_token?: string
  host_requirement?: string
  clear_auth?: boolean
}

export async function listMCPInstalls(accessToken: string): Promise<MCPInstall[]> {
  return apiFetch<MCPInstall[]>('/v1/mcp-installs', { accessToken })
}

export async function createMCPInstall(
  accessToken: string,
  req: UpsertMCPInstallRequest,
): Promise<MCPInstall> {
  return apiFetch<MCPInstall>('/v1/mcp-installs', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(req),
  })
}

export async function updateMCPInstall(
  accessToken: string,
  id: string,
  req: UpsertMCPInstallRequest,
): Promise<MCPInstall> {
  return apiFetch<MCPInstall>(`/v1/mcp-installs/${id}`, {
    method: 'PATCH',
    accessToken,
    body: JSON.stringify(req),
  })
}

export async function deleteMCPInstall(accessToken: string, id: string): Promise<void> {
  await apiFetch<void>(`/v1/mcp-installs/${id}`, {
    method: 'DELETE',
    accessToken,
  })
}

export async function checkMCPInstall(accessToken: string, id: string): Promise<MCPInstall> {
  return apiFetch<MCPInstall>(`/v1/mcp-installs/${id}:check`, {
    method: 'POST',
    accessToken,
  })
}

export async function setWorkspaceMCPEnablement(
  accessToken: string,
  req: { workspace_ref?: string; install_id: string; enabled: boolean },
): Promise<void> {
  await apiFetch<void>('/v1/workspace-mcp-enablements', {
    method: 'PUT',
    accessToken,
    body: JSON.stringify(req),
  })
}

export async function importMCPInstall(
  accessToken: string,
  req: { workspace_ref?: string; source_uri: string; install_key: string },
): Promise<MCPInstall> {
  return apiFetch<MCPInstall>('/v1/mcp-installs/import', {
    method: 'POST',
    accessToken,
    body: JSON.stringify(req),
  })
}

export async function listMCPDiscoverySources(
  accessToken: string,
  options?: { workspaceRoot?: string; paths?: string[] },
): Promise<MCPDiscoverySource[]> {
  const params = new URLSearchParams()
  if (options?.workspaceRoot?.trim()) {
    params.set('workspace_root', options.workspaceRoot.trim())
  }
  for (const path of options?.paths ?? []) {
    if (path.trim()) {
      params.append('path', path.trim())
    }
  }
  const query = params.toString()
  const response = await apiFetch<{ items: MCPDiscoverySource[] }>(
    `/v1/mcp-discovery-sources${query ? `?${query}` : ''}`,
    { accessToken },
  )
  return response.items ?? []
}
