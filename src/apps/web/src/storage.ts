import {
  canUseStorage,
} from '@arkloop/shared/storage'
import type { Theme } from '@arkloop/shared/contexts/theme'
import type { FontFamily, CodeFontFamily, FontSize, ThemePreset, ThemeDefinition } from './themes/types'

export {
  readAccessToken as readAccessTokenFromStorage,
  writeAccessToken as writeAccessTokenToStorage,
  clearAccessToken as clearAccessTokenFromStorage,
} from '@arkloop/shared/storage'

const ACTIVE_THREAD_ID_KEY = 'arkloop:web:active_thread_id'
const LOCALE_KEY = 'arkloop:web:locale'
const THEME_KEY = 'arkloop:web:theme'
const SELECTED_PERSONA_KEY = 'arkloop:web:selected_persona_key'
const APP_MODE_KEY = 'arkloop:web:app_mode'
const SELECTED_MODEL_KEY = 'arkloop:web:selected_model'
const FONT_SETTINGS_KEY = 'arkloop:web:font-settings'
const THEME_PRESET_KEY = 'arkloop:web:theme-preset'
const CUSTOM_THEME_ID_KEY = 'arkloop:web:custom-theme-id'
const CUSTOM_THEMES_KEY = 'arkloop:web:custom-themes'
const CUSTOM_BODY_FONT_KEY = 'arkloop:web:custom-body-font'

export const DEFAULT_PERSONA_KEY = 'normal'
export const SEARCH_PERSONA_KEY = 'extended-search'
export const LEARNING_PERSONA_KEY = 'stem-tutor'

export type AppMode = 'chat' | 'claw'

function canUseLocalStorage(): boolean {
  return canUseStorage()
}

function lastSeqStorageKey(runId: string): string {
  return `arkloop:sse:last_seq:${runId}`
}

export function readActiveThreadIdFromStorage(): string | null {
  if (!canUseLocalStorage()) return null
  try {
    const raw = localStorage.getItem(ACTIVE_THREAD_ID_KEY)
    if (!raw) return null
    return raw.trim() ? raw : null
  } catch {
    return null
  }
}

export function writeActiveThreadIdToStorage(threadId: string): void {
  if (!canUseLocalStorage()) return
  if (!threadId.trim()) return
  try {
    localStorage.setItem(ACTIVE_THREAD_ID_KEY, threadId)
  } catch {
    // 忽略存储失败
  }
}

export function clearActiveThreadIdInStorage(): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.removeItem(ACTIVE_THREAD_ID_KEY)
  } catch {
    // 忽略存储失败
  }
}

export function readLastSeqFromStorage(runId: string): number {
  if (!runId || !canUseLocalStorage()) return 0
  try {
    const raw = localStorage.getItem(lastSeqStorageKey(runId))
    if (!raw) return 0
    const parsed = Number.parseInt(raw, 10)
    return Number.isFinite(parsed) && parsed >= 0 ? parsed : 0
  } catch {
    return 0
  }
}

export function writeLastSeqToStorage(runId: string, seq: number): void {
  if (!runId || !canUseLocalStorage()) return
  if (!Number.isFinite(seq) || seq < 0) return
  try {
    localStorage.setItem(lastSeqStorageKey(runId), String(seq))
  } catch {
    // 忽略存储失败（无痕模式/禁用存储等）
  }
}

export function clearLastSeqInStorage(runId: string): void {
  if (!runId || !canUseLocalStorage()) return
  try {
    localStorage.removeItem(lastSeqStorageKey(runId))
  } catch {
    // 忽略存储失败
  }
}

export function readLocaleFromStorage(): import('./locales').Locale {
  if (!canUseLocalStorage()) return 'zh'
  try {
    const raw = localStorage.getItem(LOCALE_KEY)
    if (raw === 'zh' || raw === 'en') return raw
    return 'zh'
  } catch {
    return 'zh'
  }
}

export function writeLocaleToStorage(locale: import('./locales').Locale): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(LOCALE_KEY, locale)
  } catch {
    // 忽略存储失败
  }
}

export function readThemeFromStorage(): Theme {
  if (!canUseLocalStorage()) return 'system'
  try {
    const raw = localStorage.getItem(THEME_KEY)
    if (raw === 'system' || raw === 'light' || raw === 'dark') return raw
    return 'system'
  } catch {
    return 'system'
  }
}

export function writeThemeToStorage(theme: Theme): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(THEME_KEY, theme)
  } catch {
    // 忽略存储失败
  }
}

export function readAppModeFromStorage(): AppMode {
  if (!canUseLocalStorage()) return 'chat'
  try {
    const raw = localStorage.getItem(APP_MODE_KEY)
    if (raw === 'chat' || raw === 'claw') return raw
    return 'chat'
  } catch {
    return 'chat'
  }
}

export function writeAppModeToStorage(mode: AppMode): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(APP_MODE_KEY, mode)
  } catch {
    // 忽略存储失败
  }
}

export function readSelectedPersonaKeyFromStorage(): string {
  if (!canUseLocalStorage()) return DEFAULT_PERSONA_KEY
  try {
    const raw = localStorage.getItem(SELECTED_PERSONA_KEY)
    if (raw && raw.trim()) return raw.trim()
    return DEFAULT_PERSONA_KEY
  } catch {
    return DEFAULT_PERSONA_KEY
  }
}

export function writeSelectedPersonaKeyToStorage(personaKey: string): void {
  if (!canUseLocalStorage()) return
  const trimmed = personaKey.trim()
  if (!trimmed) return
  try {
    localStorage.setItem(SELECTED_PERSONA_KEY, trimmed)
  } catch {
    // 忽略存储失败
  }
}

export function readSelectedModelFromStorage(): string | null {
  if (!canUseLocalStorage()) return null
  try {
    const raw = localStorage.getItem(SELECTED_MODEL_KEY)
    return raw?.trim() || null
  } catch {
    return null
  }
}

export function writeSelectedModelToStorage(model: string | null): void {
  if (!canUseLocalStorage()) return
  try {
    if (model) {
      localStorage.setItem(SELECTED_MODEL_KEY, model)
    } else {
      localStorage.removeItem(SELECTED_MODEL_KEY)
    }
  } catch {
    // 忽略存储失败
  }
}

export type WebSource = {
  title: string
  url: string
  snippet?: string
}

function messageSourcesKey(messageId: string): string {
  return `arkloop:web:msg_sources:${messageId}`
}

export function readMessageSources(messageId: string): WebSource[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageSourcesKey(messageId))
    if (!raw) return null
    return JSON.parse(raw) as WebSource[]
  } catch {
    return null
  }
}

export function writeMessageSources(messageId: string, sources: WebSource[]): void {
  if (!canUseLocalStorage() || !messageId || sources.length === 0) return
  try {
    localStorage.setItem(messageSourcesKey(messageId), JSON.stringify(sources))
  } catch { /* ignore */ }
}

export type ArtifactRef = {
  key: string
  filename: string
  size: number
  mime_type: string
  title?: string
  display?: 'inline' | 'panel'
}

function messageArtifactsKey(messageId: string): string {
  return `arkloop:web:msg_artifacts:${messageId}`
}

export function readMessageArtifacts(messageId: string): ArtifactRef[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageArtifactsKey(messageId))
    if (!raw) return null
    return JSON.parse(raw) as ArtifactRef[]
  } catch {
    return null
  }
}

export function writeMessageArtifacts(messageId: string, artifacts: ArtifactRef[]): void {
  if (!canUseLocalStorage() || !messageId || artifacts.length === 0) return
  try {
    localStorage.setItem(messageArtifactsKey(messageId), JSON.stringify(artifacts))
  } catch { /* ignore */ }
}

export type BrowserActionRef = {
  id: string
  command: string
  output?: string
  screenshotArtifact?: ArtifactRef
  url?: string
  exitCode?: number
}

function messageBrowserActionsKey(messageId: string): string {
  return `arkloop:web:msg_browser_actions:${messageId}`
}

export function readMessageBrowserActions(messageId: string): BrowserActionRef[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageBrowserActionsKey(messageId))
    if (!raw) return null
    return JSON.parse(raw) as BrowserActionRef[]
  } catch {
    return null
  }
}

export function writeMessageBrowserActions(messageId: string, actions: BrowserActionRef[]): void {
  if (!canUseLocalStorage() || !messageId || actions.length === 0) return
  try {
    localStorage.setItem(messageBrowserActionsKey(messageId), JSON.stringify(actions))
  } catch { /* ignore */ }
}

export type CodeExecutionRef = {
  id: string
  language: 'python' | 'shell'
  code?: string
  output?: string
  exitCode?: number
  sessionId?: string
  status: 'running' | 'success' | 'failed' | 'completed'
  errorClass?: string
  errorMessage?: string
  seq?: number
}

function isCodeExecutionStatus(value: unknown): value is CodeExecutionRef['status'] {
  return value === 'running' || value === 'success' || value === 'failed' || value === 'completed'
}

function isCodeExecutionRef(value: unknown): value is CodeExecutionRef {
  if (!value || typeof value !== 'object') return false
  const item = value as Record<string, unknown>
  if (typeof item.id !== 'string' || item.id.trim() === '') return false
  if (item.language !== 'python' && item.language !== 'shell') return false
  if (!isCodeExecutionStatus(item.status)) return false
  if (item.code != null && typeof item.code !== 'string') return false
  if (item.output != null && typeof item.output !== 'string') return false
  if (item.exitCode != null && typeof item.exitCode !== 'number') return false
  if (item.sessionId != null && typeof item.sessionId !== 'string') return false
  if (item.errorClass != null && typeof item.errorClass !== 'string') return false
  if (item.errorMessage != null && typeof item.errorMessage !== 'string') return false
  return true
}

function messageCodeExecutionsKey(messageId: string): string {
  return `arkloop:web:msg_code_exec:${messageId}`
}

export function readMessageCodeExecutions(messageId: string): CodeExecutionRef[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  const cacheKey = messageCodeExecutionsKey(messageId)
  try {
    const raw = localStorage.getItem(cacheKey)
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) {
      localStorage.removeItem(cacheKey)
      return null
    }
    if (!parsed.every((item) => isCodeExecutionRef(item))) {
      localStorage.removeItem(cacheKey)
      return null
    }
    return parsed
  } catch {
    try {
      localStorage.removeItem(cacheKey)
    } catch {
      // 忽略清理失败
    }
    return null
  }
}

export function writeMessageCodeExecutions(messageId: string, executions: CodeExecutionRef[]): void {
  if (!canUseLocalStorage() || !messageId) return
  try {
    localStorage.setItem(messageCodeExecutionsKey(messageId), JSON.stringify(executions))
  } catch { /* ignore */ }
}

export type ThinkingSegmentRef = {
  segmentId: string
  kind: string
  mode: string
  label: string
  content: string
}

export type MessageThinkingRef = {
  thinkingText: string
  segments: ThinkingSegmentRef[]
}

function messageThinkingKey(messageId: string): string {
  return `arkloop:web:msg_thinking:${messageId}`
}

export function readMessageThinking(messageId: string): MessageThinkingRef | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageThinkingKey(messageId))
    if (!raw) return null
    const parsed = JSON.parse(raw) as MessageThinkingRef
    if (!parsed || typeof parsed !== 'object') return null
    const segments = Array.isArray(parsed.segments)
      ? parsed.segments.filter(
        (s): s is ThinkingSegmentRef =>
          !!s &&
          typeof s.segmentId === 'string' &&
          typeof s.kind === 'string' &&
          typeof s.mode === 'string' &&
          typeof s.label === 'string' &&
          typeof s.content === 'string',
      )
      : []
    const thinkingText = typeof parsed.thinkingText === 'string' ? parsed.thinkingText : ''
    return { thinkingText, segments }
  } catch {
    return null
  }
}

export function writeMessageThinking(messageId: string, thinking: MessageThinkingRef): void {
  if (!canUseLocalStorage() || !messageId) return
  if (thinking.thinkingText.trim() === '' && thinking.segments.length === 0) return
  try {
    localStorage.setItem(messageThinkingKey(messageId), JSON.stringify(thinking))
  } catch { /* ignore */ }
}

export type MessageSearchStepRef = {
  id: string
  kind: 'planning' | 'searching' | 'reviewing' | 'finished'
  label: string
  status: 'active' | 'done'
  queries?: string[]
}

function messageSearchStepsKey(messageId: string): string {
  return `arkloop:web:msg_search_steps:${messageId}`
}

export function readMessageSearchSteps(messageId: string): MessageSearchStepRef[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageSearchStepsKey(messageId))
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return null
    const steps = parsed
      .filter((item): item is Record<string, unknown> => item != null && typeof item === 'object')
      .map((item): MessageSearchStepRef | null => {
        const id = typeof item.id === 'string' ? item.id : ''
        const kind = item.kind
        const label = typeof item.label === 'string' ? item.label : ''
        const status = item.status
        const queries = Array.isArray(item.queries)
          ? item.queries.filter((q): q is string => typeof q === 'string')
          : undefined
        if (!id) return null
        if (kind !== 'planning' && kind !== 'searching' && kind !== 'reviewing' && kind !== 'finished') return null
        if (status !== 'active' && status !== 'done') return null
        return { id, kind, label, status, queries }
      })
      .filter((step): step is MessageSearchStepRef => step != null)
    return steps.length > 0 ? steps : null
  } catch {
    return null
  }
}

export function writeMessageSearchSteps(messageId: string, steps: MessageSearchStepRef[]): void {
  if (!canUseLocalStorage() || !messageId || steps.length === 0) return
  try {
    localStorage.setItem(messageSearchStepsKey(messageId), JSON.stringify(steps))
  } catch { /* ignore */ }
}

// -- Memory Actions --

export type MemoryActionRef = {
  id: string
  toolName: 'memory_write' | 'memory_search' | 'memory_read' | 'memory_forget'
  args: { category?: string; key?: string; query?: string; uri?: string }
  status: 'active' | 'done' | 'error'
  resultSummary?: string
}

function messageMemoryActionsKey(messageId: string): string {
  return `arkloop:web:msg_memory_actions:${messageId}`
}

export function readMessageMemoryActions(messageId: string): MemoryActionRef[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageMemoryActionsKey(messageId))
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return null
    const actions = parsed
      .filter((item): item is Record<string, unknown> => item != null && typeof item === 'object')
      .map((item): MemoryActionRef | null => {
        const id = typeof item.id === 'string' ? item.id : ''
        const toolName = item.toolName
        const args = (item.args ?? {}) as MemoryActionRef['args']
        const status = item.status
        const resultSummary = typeof item.resultSummary === 'string' ? item.resultSummary : undefined
        if (!id) return null
        if (toolName !== 'memory_write' && toolName !== 'memory_search' && toolName !== 'memory_read' && toolName !== 'memory_forget') return null
        if (status !== 'active' && status !== 'done' && status !== 'error') return null
        return { id, toolName, args, status, resultSummary }
      })
      .filter((a): a is MemoryActionRef => a != null)
    return actions.length > 0 ? actions : null
  } catch {
    return null
  }
}

export function writeMessageMemoryActions(messageId: string, actions: MemoryActionRef[]): void {
  if (!canUseLocalStorage() || !messageId || actions.length === 0) return
  try {
    localStorage.setItem(messageMemoryActionsKey(messageId), JSON.stringify(actions))
  } catch { /* ignore */ }
}

// -- COP Blocks --

export type CopBlockRef = {
  id: string
  title: string
  steps: MessageSearchStepRef[]
  sources: WebSource[]
  codeExecutions?: CodeExecutionRef[]
}

export type MessageCopBlocksRef = {
  blocks: CopBlockRef[]
  bridgeTexts: string[]
  preText?: string
}

function messageCopBlocksKey(messageId: string): string {
  return `arkloop:web:msg_cop_blocks:${messageId}`
}

function parseStepRef(s: Record<string, unknown>): MessageSearchStepRef | null {
  const id = typeof s.id === 'string' ? s.id : ''
  const kind = s.kind
  const label = typeof s.label === 'string' ? s.label : ''
  const status = s.status
  const queries = Array.isArray(s.queries)
    ? (s.queries as unknown[]).filter((q): q is string => typeof q === 'string')
    : undefined
  if (!id) return null
  if (kind !== 'planning' && kind !== 'searching' && kind !== 'reviewing' && kind !== 'finished') return null
  if (status !== 'active' && status !== 'done') return null
  return { id, kind, label, status, queries }
}

export function readMessageCopBlocks(messageId: string): MessageCopBlocksRef | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageCopBlocksKey(messageId))
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    if (typeof parsed !== 'object' || parsed == null) return null
    const obj = parsed as Record<string, unknown>
    if (!Array.isArray(obj.blocks)) return null
    const blocks: CopBlockRef[] = (obj.blocks as unknown[])
      .filter((b): b is Record<string, unknown> => b != null && typeof b === 'object')
      .map((b): CopBlockRef | null => {
        const id = typeof b.id === 'string' ? b.id : ''
        const title = typeof b.title === 'string' ? b.title : ''
        if (!id) return null
        const steps = Array.isArray(b.steps)
          ? (b.steps as unknown[])
              .filter((s): s is Record<string, unknown> => s != null && typeof s === 'object')
              .map(parseStepRef)
              .filter((s): s is MessageSearchStepRef => s != null)
          : []
        const sources: WebSource[] = Array.isArray(b.sources)
          ? (b.sources as unknown[])
              .filter((s): s is Record<string, unknown> => s != null && typeof s === 'object')
              .map((s) => ({
                title: typeof s.title === 'string' ? s.title : '',
                url: typeof s.url === 'string' ? s.url : '',
                snippet: typeof s.snippet === 'string' ? s.snippet : undefined,
              }))
              .filter((s) => !!s.url)
          : []
        const codeExecutions: CodeExecutionRef[] = Array.isArray(b.codeExecutions)
          ? (b.codeExecutions as unknown[]).filter(isCodeExecutionRef)
          : []
        return { id, title, steps, sources, codeExecutions: codeExecutions.length > 0 ? codeExecutions : undefined }
      })
      .filter((b): b is CopBlockRef => b != null)
    if (blocks.length === 0) return null
    const bridgeTexts = Array.isArray(obj.bridgeTexts)
      ? (obj.bridgeTexts as unknown[]).map(t => typeof t === 'string' ? t : '')
      : []
    const preText = typeof obj.preText === 'string' && obj.preText ? obj.preText : undefined
    return { blocks, bridgeTexts, preText }
  } catch {
    return null
  }
}

export function writeMessageCopBlocks(messageId: string, data: MessageCopBlocksRef): void {
  if (!canUseLocalStorage() || !messageId || data.blocks.length === 0) return
  try {
    localStorage.setItem(messageCopBlocksKey(messageId), JSON.stringify(data))
  } catch { /* ignore */ }
}

// -- File Operations --

export type FileOpRef = {
  id: string
  toolName: string
  label: string
  output?: string
  status: 'running' | 'success' | 'failed'
  errorMessage?: string
  seq?: number
}

function isFileOpRef(v: unknown): v is FileOpRef {
  if (!v || typeof v !== 'object') return false
  const o = v as Record<string, unknown>
  if (typeof o.id !== 'string' || !o.id) return false
  if (typeof o.toolName !== 'string') return false
  if (typeof o.label !== 'string') return false
  const s = o.status
  if (s !== 'running' && s !== 'success' && s !== 'failed') return false
  return true
}

function messageFileOpsKey(messageId: string): string {
  return `arkloop:web:msg_file_ops:${messageId}`
}

export function readMessageFileOps(messageId: string): FileOpRef[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageFileOpsKey(messageId))
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) {
      localStorage.removeItem(messageFileOpsKey(messageId))
      return null
    }
    if (!parsed.every((item) => isFileOpRef(item))) {
      localStorage.removeItem(messageFileOpsKey(messageId))
      return null
    }
    return parsed
  } catch {
    try { localStorage.removeItem(messageFileOpsKey(messageId)) } catch { /* ignore */ }
    return null
  }
}

export function writeMessageFileOps(messageId: string, ops: FileOpRef[]): void {
  if (!canUseLocalStorage() || !messageId || ops.length === 0) return
  try {
    localStorage.setItem(messageFileOpsKey(messageId), JSON.stringify(ops))
  } catch { /* ignore */ }
}

// -- Sub-Agent --

export type SubAgentStatus = 'spawning' | 'active' | 'completed' | 'failed' | 'closed'

export type SubAgentRef = {
  id: string
  subAgentId?: string
  nickname?: string
  role?: string
  personaId?: string
  contextMode?: string
  input?: string
  output?: string
  status: SubAgentStatus
  error?: string
  depth?: number
  currentRunId?: string
  seq?: number
}

function isSubAgentStatus(v: unknown): v is SubAgentStatus {
  return v === 'spawning' || v === 'active' || v === 'completed' || v === 'failed' || v === 'closed'
}

function isSubAgentRef(v: unknown): v is SubAgentRef {
  if (!v || typeof v !== 'object') return false
  const o = v as Record<string, unknown>
  if (typeof o.id !== 'string' || !o.id) return false
  if (!isSubAgentStatus(o.status)) return false
  return true
}

function messageSubAgentsKey(messageId: string): string {
  return `arkloop:web:msg_sub_agents:${messageId}`
}

export function readMessageSubAgents(messageId: string): SubAgentRef[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageSubAgentsKey(messageId))
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) {
      localStorage.removeItem(messageSubAgentsKey(messageId))
      return null
    }
    if (!parsed.every((item) => isSubAgentRef(item))) {
      localStorage.removeItem(messageSubAgentsKey(messageId))
      return null
    }
    return parsed
  } catch {
    try { localStorage.removeItem(messageSubAgentsKey(messageId)) } catch { /* ignore */ }
    return null
  }
}

export function writeMessageSubAgents(messageId: string, agents: SubAgentRef[]): void {
  if (!canUseLocalStorage() || !messageId || agents.length === 0) return
  try {
    localStorage.setItem(messageSubAgentsKey(messageId), JSON.stringify(agents))
  } catch { /* ignore */ }
}

// -- Web Fetch --

export type WebFetchRef = {
  id: string
  url: string
  title?: string
  status: 'fetching' | 'done' | 'failed'
  statusCode?: number
  seq?: number
}

function isWebFetchRef(v: unknown): v is WebFetchRef {
  if (!v || typeof v !== 'object') return false
  const o = v as Record<string, unknown>
  if (typeof o.id !== 'string' || !o.id) return false
  if (typeof o.url !== 'string') return false
  const s = o.status
  if (s !== 'fetching' && s !== 'done' && s !== 'failed') return false
  return true
}

function messageWebFetchesKey(messageId: string): string {
  return `arkloop:web:msg_web_fetches:${messageId}`
}

export function readMessageWebFetches(messageId: string): WebFetchRef[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageWebFetchesKey(messageId))
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) {
      localStorage.removeItem(messageWebFetchesKey(messageId))
      return null
    }
    if (!parsed.every((item) => isWebFetchRef(item))) {
      localStorage.removeItem(messageWebFetchesKey(messageId))
      return null
    }
    return parsed
  } catch {
    try { localStorage.removeItem(messageWebFetchesKey(messageId)) } catch { /* ignore */ }
    return null
  }
}

export function writeMessageWebFetches(messageId: string, fetches: WebFetchRef[]): void {
  if (!canUseLocalStorage() || !messageId || fetches.length === 0) return
  try {
    localStorage.setItem(messageWebFetchesKey(messageId), JSON.stringify(fetches))
  } catch { /* ignore */ }
}

// -- Developer Settings --

const DEVELOPER_SHOW_RUN_EVENTS_KEY = 'arkloop:web:developer_show_run_events'

export function readDeveloperShowRunEvents(): boolean {
  if (!canUseLocalStorage()) return false
  try {
    return localStorage.getItem(DEVELOPER_SHOW_RUN_EVENTS_KEY) === 'true'
  } catch { return false }
}

export function writeDeveloperShowRunEvents(value: boolean): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(DEVELOPER_SHOW_RUN_EVENTS_KEY, value ? 'true' : 'false')
    window.dispatchEvent(new CustomEvent('arkloop:developer_show_run_events', { detail: value }))
  } catch { /* ignore */ }
}

// -- Per-message run events (for inline debug display) --

export type MsgRunEvent = {
  event_id: string
  run_id: string
  seq: number
  ts: string
  type: string
  data: unknown
  tool_name?: string
  error_class?: string
}

function messageRunEventsKey(messageId: string): string {
  return `arkloop:web:msg_run_events:${messageId}`
}

export function readMsgRunEvents(messageId: string): MsgRunEvent[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageRunEventsKey(messageId))
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return null
    const events = parsed.filter(
      (item): item is MsgRunEvent =>
        item != null &&
        typeof item === 'object' &&
        typeof (item as Record<string, unknown>).event_id === 'string' &&
        typeof (item as Record<string, unknown>).type === 'string',
    )
    return events.length > 0 ? events : null
  } catch { return null }
}

export function writeMsgRunEvents(messageId: string, events: MsgRunEvent[]): void {
  if (!canUseLocalStorage() || !messageId || events.length === 0) return
  try {
    localStorage.setItem(messageRunEventsKey(messageId), JSON.stringify(events))
  } catch { /* ignore */ }
}

// -- Thread Mode Tracking --

const THREAD_MODES_KEY = 'arkloop:web:thread_modes'

export function writeThreadMode(threadId: string, mode: AppMode): void {
  if (!canUseLocalStorage() || !threadId) return
  try {
    const raw = localStorage.getItem(THREAD_MODES_KEY)
    const map: Record<string, string> = raw ? (JSON.parse(raw) as Record<string, string>) : {}
    map[threadId] = mode
    localStorage.setItem(THREAD_MODES_KEY, JSON.stringify(map))
  } catch { /* ignore */ }
}

export function readThreadMode(threadId: string): AppMode {
  if (!canUseLocalStorage() || !threadId) return 'chat'
  try {
    const raw = localStorage.getItem(THREAD_MODES_KEY)
    if (!raw) return 'chat'
    const map = JSON.parse(raw) as Record<string, string>
    const mode = map[threadId]
    return mode === 'claw' ? 'claw' : 'chat'
  } catch { return 'chat' }
}

// -- Claw Work Folder --

const CLAW_WORK_FOLDER_KEY = 'arkloop:web:claw_work_folder'
const CLAW_RECENT_FOLDERS_KEY = 'arkloop:web:claw_recent_folders'

export function readClawWorkFolder(): string | null {
  if (!canUseLocalStorage()) return null
  try {
    return localStorage.getItem(CLAW_WORK_FOLDER_KEY) || null
  } catch { return null }
}

export function writeClawWorkFolder(folder: string): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(CLAW_WORK_FOLDER_KEY, folder)
    // also add to recent
    const raw = localStorage.getItem(CLAW_RECENT_FOLDERS_KEY)
    const recents: string[] = raw ? (JSON.parse(raw) as string[]) : []
    const next = [folder, ...recents.filter((f) => f !== folder)].slice(0, 8)
    localStorage.setItem(CLAW_RECENT_FOLDERS_KEY, JSON.stringify(next))
  } catch { /* ignore */ }
  window.dispatchEvent(new CustomEvent('arkloop:claw-folder-changed'))
}

export function clearClawWorkFolder(): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.removeItem(CLAW_WORK_FOLDER_KEY)
  } catch { /* ignore */ }
  window.dispatchEvent(new CustomEvent('arkloop:claw-folder-changed'))
}

export function readClawRecentFolders(): string[] {
  if (!canUseLocalStorage()) return []
  try {
    const raw = localStorage.getItem(CLAW_RECENT_FOLDERS_KEY)
    if (!raw) return []
    return JSON.parse(raw) as string[]
  } catch { return [] }
}

const SEARCH_THREAD_IDS_KEY = 'arkloop:web:search_thread_ids'

export function addSearchThreadId(threadId: string): void {
  if (!canUseLocalStorage()) return
  try {
    const raw = localStorage.getItem(SEARCH_THREAD_IDS_KEY)
    const ids: string[] = raw ? (JSON.parse(raw) as string[]) : []
    if (ids.includes(threadId)) return
    ids.push(threadId)
    if (ids.length > 500) ids.splice(0, ids.length - 500)
    localStorage.setItem(SEARCH_THREAD_IDS_KEY, JSON.stringify(ids))
  } catch { /* ignore */ }
}

export function isSearchThreadId(threadId: string): boolean {
  if (!canUseLocalStorage()) return false
  try {
    const raw = localStorage.getItem(SEARCH_THREAD_IDS_KEY)
    if (!raw) return false
    return (JSON.parse(raw) as string[]).includes(threadId)
  } catch { return false }
}

// 将 fork 前旧消息 ID 对应的 localStorage 缓存迁移到新消息 ID
export function migrateMessageMetadata(mapping: Array<{ old_id: string; new_id: string }>): void {
  if (!canUseLocalStorage() || mapping.length === 0) return
  for (const { old_id, new_id } of mapping) {
    const sources = readMessageSources(old_id)
    if (sources) writeMessageSources(new_id, sources)
    const artifacts = readMessageArtifacts(old_id)
    if (artifacts) writeMessageArtifacts(new_id, artifacts)
    const codeExec = readMessageCodeExecutions(old_id)
    if (codeExec) writeMessageCodeExecutions(new_id, codeExec)
    const thinking = readMessageThinking(old_id)
    if (thinking) writeMessageThinking(new_id, thinking)
    const searchSteps = readMessageSearchSteps(old_id)
    if (searchSteps) writeMessageSearchSteps(new_id, searchSteps)
    const copBlocks = readMessageCopBlocks(old_id)
    if (copBlocks) writeMessageCopBlocks(new_id, copBlocks)
    const fileOps = readMessageFileOps(old_id)
    if (fileOps) writeMessageFileOps(new_id, fileOps)
    const webFetches = readMessageWebFetches(old_id)
    if (webFetches) writeMessageWebFetches(new_id, webFetches)
  }
}

// -- Appearance Settings --

export type FontSettings = {
  fontFamily: FontFamily
  codeFontFamily: CodeFontFamily
  fontSize: FontSize
}

export function readFontSettingsFromStorage(): FontSettings {
  if (!canUseLocalStorage()) return { fontFamily: 'inter', codeFontFamily: 'jetbrains-mono', fontSize: 'normal' }
  try {
    const raw = localStorage.getItem(FONT_SETTINGS_KEY)
    if (!raw) return { fontFamily: 'inter', codeFontFamily: 'jetbrains-mono', fontSize: 'normal' }
    const parsed = JSON.parse(raw) as Partial<FontSettings>
    return {
      fontFamily: (['inter', 'system', 'serif', 'noto-sans', 'source-sans', 'custom'] as FontFamily[]).includes(parsed.fontFamily as FontFamily) ? parsed.fontFamily as FontFamily : 'inter',
      codeFontFamily: (['jetbrains-mono', 'fira-code', 'cascadia-code', 'source-code-pro'] as CodeFontFamily[]).includes(parsed.codeFontFamily as CodeFontFamily) ? parsed.codeFontFamily as CodeFontFamily : 'jetbrains-mono',
      fontSize: (['compact', 'normal', 'relaxed'] as FontSize[]).includes(parsed.fontSize as FontSize) ? parsed.fontSize as FontSize : 'normal',
    }
  } catch {
    return { fontFamily: 'inter', codeFontFamily: 'jetbrains-mono', fontSize: 'normal' }
  }
}

export function writeFontSettingsToStorage(settings: FontSettings): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(FONT_SETTINGS_KEY, JSON.stringify(settings))
  } catch { /* ignore */ }
}

export function readThemePresetFromStorage(): ThemePreset {
  if (!canUseLocalStorage()) return 'default'
  try {
    const raw = localStorage.getItem(THEME_PRESET_KEY)
    const valid: ThemePreset[] = ['default', 'terra', 'github', 'nord', 'catppuccin', 'tokyo-night', 'custom']
    return valid.includes(raw as ThemePreset) ? (raw as ThemePreset) : 'default'
  } catch {
    return 'default'
  }
}

export function writeThemePresetToStorage(preset: ThemePreset): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(THEME_PRESET_KEY, preset)
  } catch { /* ignore */ }
}

export function readCustomThemeIdFromStorage(): string | null {
  if (!canUseLocalStorage()) return null
  try {
    return localStorage.getItem(CUSTOM_THEME_ID_KEY) || null
  } catch {
    return null
  }
}

export function writeCustomThemeIdToStorage(id: string | null): void {
  if (!canUseLocalStorage()) return
  try {
    if (id) {
      localStorage.setItem(CUSTOM_THEME_ID_KEY, id)
    } else {
      localStorage.removeItem(CUSTOM_THEME_ID_KEY)
    }
  } catch { /* ignore */ }
}

export function readCustomThemesFromStorage(): Record<string, ThemeDefinition> {
  if (!canUseLocalStorage()) return {}
  try {
    const raw = localStorage.getItem(CUSTOM_THEMES_KEY)
    if (!raw) return {}
    return JSON.parse(raw) as Record<string, ThemeDefinition>
  } catch {
    return {}
  }
}

export function writeCustomThemesToStorage(themes: Record<string, ThemeDefinition>): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(CUSTOM_THEMES_KEY, JSON.stringify(themes))
  } catch { /* ignore */ }
}

export function readCustomBodyFontFromStorage(): string | null {
  if (!canUseLocalStorage()) return null
  try {
    return localStorage.getItem(CUSTOM_BODY_FONT_KEY) || null
  } catch { return null }
}

export function writeCustomBodyFontToStorage(font: string | null): void {
  if (!canUseLocalStorage()) return
  try {
    if (font) {
      localStorage.setItem(CUSTOM_BODY_FONT_KEY, font)
    } else {
      localStorage.removeItem(CUSTOM_BODY_FONT_KEY)
    }
  } catch { /* ignore */ }
}
