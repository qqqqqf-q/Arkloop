import {
  canUseStorage,
} from '@arkloop/shared/storage'
import type { Theme } from '@arkloop/shared/contexts/theme'
import type { FontFamily, CodeFontFamily, FontSize, ThemePreset, ThemeDefinition } from './themes/types'
import type { AssistantTurnSegment, AssistantTurnUi, CopBlockItem, TurnToolCallRef } from './assistantTurnSegments'

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
const SELECTED_THINKING_KEY = 'arkloop:web:selected_thinking'
const FONT_SETTINGS_KEY = 'arkloop:web:font-settings'
const THEME_PRESET_KEY = 'arkloop:web:theme-preset'
const CUSTOM_THEME_ID_KEY = 'arkloop:web:custom-theme-id'
const CUSTOM_THEMES_KEY = 'arkloop:web:custom-themes'
const CUSTOM_BODY_FONT_KEY = 'arkloop:web:custom-body-font'

export const DEFAULT_PERSONA_KEY = 'normal'
export const SEARCH_PERSONA_KEY = 'extended-search'
export const LEARNING_PERSONA_KEY = 'stem-tutor'

export type AppMode = 'chat' | 'work'

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
    if (raw === 'chat' || raw === 'work') return raw
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

export function readSelectedThinkingEnabled(): boolean {
  if (!canUseLocalStorage()) return false
  return localStorage.getItem(SELECTED_THINKING_KEY) === 'true'
}

export function writeSelectedThinkingEnabled(enabled: boolean): void {
  if (!canUseLocalStorage()) return
  try {
    if (enabled) {
      localStorage.setItem(SELECTED_THINKING_KEY, 'true')
    } else {
      localStorage.removeItem(SELECTED_THINKING_KEY)
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

export type WidgetRef = {
  id: string       // tool_call_id
  title: string
  html: string     // widget_code
}

function messageWidgetsKey(messageId: string): string {
  return `arkloop:web:msg_widgets:${messageId}`
}

export function readMessageWidgets(messageId: string): WidgetRef[] | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageWidgetsKey(messageId))
    return raw ? (JSON.parse(raw) as WidgetRef[]) : null
  } catch { return null }
}

export function writeMessageWidgets(messageId: string, widgets: WidgetRef[]): void {
  if (!canUseLocalStorage() || !messageId || widgets.length === 0) return
  try {
    localStorage.setItem(messageWidgetsKey(messageId), JSON.stringify(widgets))
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
  mode?: 'buffered' | 'follow' | 'stdin' | 'pty'
  code?: string
  output?: string
  exitCode?: number
  processRef?: string
  cursor?: string
  nextCursor?: string
  processStatus?: 'running' | 'exited' | 'terminated' | 'timed_out' | 'cancelled'
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
  if (item.mode != null && item.mode !== 'buffered' && item.mode !== 'follow' && item.mode !== 'stdin' && item.mode !== 'pty') return false
  if (!isCodeExecutionStatus(item.status)) return false
  if (item.code != null && typeof item.code !== 'string') return false
  if (item.output != null && typeof item.output !== 'string') return false
  if (item.exitCode != null && typeof item.exitCode !== 'number') return false
  if (item.processRef != null && typeof item.processRef !== 'string') return false
  if (item.cursor != null && typeof item.cursor !== 'string') return false
  if (item.nextCursor != null && typeof item.nextCursor !== 'string') return false
  if (item.processStatus != null && item.processStatus !== 'running' && item.processStatus !== 'exited' && item.processStatus !== 'terminated' && item.processStatus !== 'timed_out' && item.processStatus !== 'cancelled') return false
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
  seq?: number
  resultSeq?: number
  sources?: WebSource[]
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
        const seq = typeof item.seq === 'number' ? item.seq : undefined
        const resultSeq = typeof item.resultSeq === 'number' ? item.resultSeq : undefined
        const queries = Array.isArray(item.queries)
          ? item.queries.filter((q): q is string => typeof q === 'string')
          : undefined
        const sources = Array.isArray(item.sources)
          ? item.sources
            .filter((source): source is Record<string, unknown> => source != null && typeof source === 'object')
            .map((source): WebSource | null => {
              const title = typeof source.title === 'string' ? source.title : ''
              const url = typeof source.url === 'string' ? source.url : ''
              if (!url) return null
              const snippet = typeof source.snippet === 'string' ? source.snippet : undefined
              return { title, url, snippet }
            })
            .filter((source): source is WebSource => source != null)
          : undefined
        if (!id) return null
        if (kind !== 'planning' && kind !== 'searching' && kind !== 'reviewing' && kind !== 'finished') return null
        if (status !== 'active' && status !== 'done') return null
        return { id, kind, label, status, queries, seq, ...(resultSeq != null ? { resultSeq } : {}), ...(sources && sources.length > 0 ? { sources } : {}) }
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
  toolName:
    | 'memory_write'
    | 'memory_edit'
    | 'memory_search'
    | 'memory_read'
    | 'memory_forget'
    | 'notebook_write'
    | 'notebook_read'
    | 'notebook_edit'
    | 'notebook_forget'
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
        if (
          toolName !== 'memory_write'
          && toolName !== 'memory_edit'
          && toolName !== 'memory_search'
          && toolName !== 'memory_read'
          && toolName !== 'memory_forget'
          && toolName !== 'notebook_write'
          && toolName !== 'notebook_read'
          && toolName !== 'notebook_edit'
          && toolName !== 'notebook_forget'
        ) return null
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
  narratives?: Array<{ id: string; text: string; seq: number }>
  codeExecutions?: CodeExecutionRef[]
  subAgents?: SubAgentRef[]
  fileOps?: FileOpRef[]
  webFetches?: WebFetchRef[]
}

export type MessageCopBlocksRef = {
  blocks: CopBlockRef[]
  preText?: string
  finalContent?: string
  bridgeTexts?: string[]
}

function messageCopBlocksKey(messageId: string): string {
  return `arkloop:web:msg_cop_blocks:${messageId}`
}

function parseStepRef(s: Record<string, unknown>): MessageSearchStepRef | null {
  const id = typeof s.id === 'string' ? s.id : ''
  const kind = s.kind
  const label = typeof s.label === 'string' ? s.label : ''
  const status = s.status
  const seq = typeof s.seq === 'number' ? s.seq : undefined
  const resultSeq = typeof s.resultSeq === 'number' ? s.resultSeq : undefined
  const queries = Array.isArray(s.queries)
    ? (s.queries as unknown[]).filter((q): q is string => typeof q === 'string')
    : undefined
  if (!id) return null
  if (kind !== 'planning' && kind !== 'searching' && kind !== 'reviewing' && kind !== 'finished') return null
  if (status !== 'active' && status !== 'done') return null
  return { id, kind, label, status, queries, seq, ...(resultSeq != null ? { resultSeq } : {}) }
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
        const narratives = Array.isArray(b.narratives)
          ? (b.narratives as unknown[])
              .filter((n): n is Record<string, unknown> => n != null && typeof n === 'object')
              .flatMap((n) => {
                const narrativeId = typeof n.id === 'string' ? n.id : ''
                const text = typeof n.text === 'string' ? n.text : ''
                const seq = typeof n.seq === 'number' ? n.seq : undefined
                if (!narrativeId || !text || seq == null) return []
                return [{ id: narrativeId, text, seq }]
              })
          : []
        const codeExecutions: CodeExecutionRef[] = Array.isArray(b.codeExecutions)
          ? (b.codeExecutions as unknown[]).filter(isCodeExecutionRef)
          : []
        const subAgents: SubAgentRef[] = Array.isArray(b.subAgents)
          ? (b.subAgents as unknown[]).filter(isSubAgentRef)
          : []
        const fileOps: FileOpRef[] = Array.isArray(b.fileOps)
          ? (b.fileOps as unknown[]).filter(isFileOpRef)
          : []
        const webFetches: WebFetchRef[] = Array.isArray(b.webFetches)
          ? (b.webFetches as unknown[]).filter(isWebFetchRef)
          : []
        return {
          id,
          title,
          steps,
          sources,
          narratives: narratives.length > 0 ? narratives : undefined,
          codeExecutions: codeExecutions.length > 0 ? codeExecutions : undefined,
          subAgents: subAgents.length > 0 ? subAgents : undefined,
          fileOps: fileOps.length > 0 ? fileOps : undefined,
          webFetches: webFetches.length > 0 ? webFetches : undefined,
        }
      })
      .filter((b): b is CopBlockRef => b != null)
    if (blocks.length === 0) return null
    const bridgeTexts = Array.isArray(obj.bridgeTexts)
      ? (obj.bridgeTexts as unknown[]).map(t => typeof t === 'string' ? t : '')
      : []
    const preText = typeof obj.preText === 'string' && obj.preText ? obj.preText : undefined
    const finalContent = typeof obj.finalContent === 'string' ? obj.finalContent : undefined
    return { blocks, preText, finalContent, bridgeTexts: bridgeTexts.length > 0 ? bridgeTexts : undefined }
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

// -- Assistant turn segments (text | cop) --

function messageAssistantTurnKey(messageId: string): string {
  return `arkloop:web:msg_assistant_turn:${messageId}`
}

function parseTurnToolCallRef(raw: unknown): TurnToolCallRef | null {
  if (!raw || typeof raw !== 'object') return null
  const o = raw as Record<string, unknown>
  if (typeof o.toolCallId !== 'string' || typeof o.toolName !== 'string') return null
  if (!o.arguments || typeof o.arguments !== 'object' || Array.isArray(o.arguments)) return null
  const rec: TurnToolCallRef = {
    toolCallId: o.toolCallId,
    toolName: o.toolName,
    arguments: { ...(o.arguments as Record<string, unknown>) },
  }
  rec.result = o.result
  if (typeof o.errorClass === 'string' && o.errorClass.trim() !== '') rec.errorClass = o.errorClass
  return rec
}

function parseCopBlockItem(raw: unknown): CopBlockItem | null {
  if (!raw || typeof raw !== 'object') return null
  const o = raw as Record<string, unknown>
  if (o.kind === 'thinking') {
    if (typeof o.content !== 'string') return null
    const seq = typeof o.seq === 'number' ? o.seq : 0
    const startedAtMs = typeof o.startedAtMs === 'number' ? o.startedAtMs : undefined
    const endedAtMs = typeof o.endedAtMs === 'number' ? o.endedAtMs : undefined
    return {
      kind: 'thinking',
      content: o.content,
      seq,
      ...(startedAtMs != null ? { startedAtMs } : {}),
      ...(endedAtMs != null ? { endedAtMs } : {}),
    }
  }
  if (o.kind === 'assistant_text') {
    if (typeof o.content !== 'string') return null
    const seq = typeof o.seq === 'number' ? o.seq : 0
    return { kind: 'assistant_text', content: o.content, seq }
  }
  if (o.kind === 'call') {
    const call = parseTurnToolCallRef(o.call)
    if (!call) return null
    const seq = typeof o.seq === 'number' ? o.seq : 0
    return { kind: 'call', call, seq }
  }
  return null
}

function parseAssistantTurnSegment(raw: unknown): AssistantTurnSegment | null {
  if (!raw || typeof raw !== 'object') return null
  const o = raw as Record<string, unknown>
  const ty = o.type
  if (ty === 'text') {
    if (typeof o.content !== 'string') return null
    return { type: 'text', content: o.content }
  }
  if (ty === 'thinking') {
    if (typeof o.content !== 'string') return null
    return {
      type: 'cop',
      title: null,
      items: [{ kind: 'thinking', content: o.content, seq: 0 }],
    }
  }
  if (ty !== 'cop') return null
  const title = o.title === null ? null : typeof o.title === 'string' ? o.title : null
  if (Array.isArray(o.items)) {
    const items = (o.items as unknown[]).map(parseCopBlockItem).filter((x): x is CopBlockItem => x != null)
    if (items.length > 0) return { type: 'cop', title, items }
  }
  if (Array.isArray(o.calls)) {
    const calls = o.calls.map(parseTurnToolCallRef).filter((c): c is TurnToolCallRef => c != null)
    const items: CopBlockItem[] = calls.map((c, i) => ({ kind: 'call', call: c, seq: i }))
    return { type: 'cop', title, items }
  }
  return null
}

function parseAssistantTurnData(raw: unknown): AssistantTurnUi | null {
  if (!raw || typeof raw !== 'object') return null
  const obj = raw as Record<string, unknown>
  if (!Array.isArray(obj.segments)) return null
  const segments = (obj.segments as unknown[])
    .map(parseAssistantTurnSegment)
    .filter((s): s is AssistantTurnSegment => s != null)
  if (segments.length === 0) return null
  return { segments }
}

export function readMessageAssistantTurn(messageId: string): AssistantTurnUi | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const item = localStorage.getItem(messageAssistantTurnKey(messageId))
    if (!item) return null
    return parseAssistantTurnData(JSON.parse(item) as unknown)
  } catch {
    return null
  }
}

export function writeMessageAssistantTurn(messageId: string, data: AssistantTurnUi): void {
  if (!canUseLocalStorage() || !messageId || data.segments.length === 0) return
  try {
    localStorage.setItem(messageAssistantTurnKey(messageId), JSON.stringify(data))
  } catch { /* ignore */ }
}

export function clearMessageAssistantTurn(messageId: string): void {
  if (!canUseLocalStorage() || !messageId) return
  try {
    localStorage.removeItem(messageAssistantTurnKey(messageId))
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
  /** host acp_agent vs spawn_agent；仅影响标签文案 */
  sourceTool?: 'acp_agent'
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

// -- Terminal handoff status --

export type MessageTerminalStatusRef = 'completed' | 'cancelled' | 'interrupted' | 'failed'

function messageTerminalStatusKey(messageId: string): string {
  return `arkloop:web:msg_terminal_status:${messageId}`
}

export function readMessageTerminalStatus(messageId: string): MessageTerminalStatusRef | null {
  if (!canUseLocalStorage() || !messageId) return null
  try {
    const raw = localStorage.getItem(messageTerminalStatusKey(messageId))
    return raw === 'completed' || raw === 'cancelled' || raw === 'interrupted' || raw === 'failed' ? raw : null
  } catch {
    return null
  }
}

export function writeMessageTerminalStatus(messageId: string, status: MessageTerminalStatusRef): void {
  if (!canUseLocalStorage() || !messageId) return
  try {
    localStorage.setItem(messageTerminalStatusKey(messageId), status)
  } catch { /* ignore */ }
}

// -- Thread run handoff --

export type ThreadRunHandoffRef = {
  runId: string
  status: Exclude<MessageTerminalStatusRef, 'completed'>
  assistantTurn?: AssistantTurnUi | null
  sources: WebSource[]
  artifacts: ArtifactRef[]
  widgets: WidgetRef[]
  codeExecutions: CodeExecutionRef[]
  browserActions: BrowserActionRef[]
  subAgents: SubAgentRef[]
  fileOps: FileOpRef[]
  webFetches: WebFetchRef[]
  searchSteps: MessageSearchStepRef[]
}

function threadRunHandoffKey(threadId: string): string {
  return `arkloop:web:thread_run_handoff:${threadId}`
}

function isWebSource(value: unknown): value is WebSource {
  if (!value || typeof value !== 'object') return false
  const item = value as Record<string, unknown>
  if (typeof item.title !== 'string') return false
  if (typeof item.url !== 'string' || item.url.trim() === '') return false
  if (item.snippet != null && typeof item.snippet !== 'string') return false
  return true
}

function isArtifactRef(value: unknown): value is ArtifactRef {
  if (!value || typeof value !== 'object') return false
  const item = value as Record<string, unknown>
  if (typeof item.key !== 'string' || item.key.trim() === '') return false
  if (typeof item.filename !== 'string') return false
  if (typeof item.size !== 'number') return false
  if (typeof item.mime_type !== 'string') return false
  if (item.title != null && typeof item.title !== 'string') return false
  if (item.display != null && item.display !== 'inline' && item.display !== 'panel') return false
  return true
}

function isWidgetRef(value: unknown): value is WidgetRef {
  if (!value || typeof value !== 'object') return false
  const item = value as Record<string, unknown>
  return typeof item.id === 'string' &&
    item.id.trim() !== '' &&
    typeof item.title === 'string' &&
    typeof item.html === 'string'
}

function isBrowserActionRef(value: unknown): value is BrowserActionRef {
  if (!value || typeof value !== 'object') return false
  const item = value as Record<string, unknown>
  if (typeof item.id !== 'string' || item.id.trim() === '') return false
  if (typeof item.command !== 'string') return false
  if (item.output != null && typeof item.output !== 'string') return false
  if (item.url != null && typeof item.url !== 'string') return false
  if (item.exitCode != null && typeof item.exitCode !== 'number') return false
  if (item.screenshotArtifact != null && !isArtifactRef(item.screenshotArtifact)) return false
  return true
}

function isMessageSearchStepRef(value: unknown): value is MessageSearchStepRef {
  if (!value || typeof value !== 'object') return false
  const item = value as Record<string, unknown>
  if (typeof item.id !== 'string' || item.id.trim() === '') return false
  if (item.kind !== 'planning' && item.kind !== 'searching' && item.kind !== 'reviewing' && item.kind !== 'finished') return false
  if (item.status !== 'active' && item.status !== 'done') return false
  if (typeof item.label !== 'string') return false
  if (item.seq != null && typeof item.seq !== 'number') return false
  if (item.resultSeq != null && typeof item.resultSeq !== 'number') return false
  if (item.queries != null && (!Array.isArray(item.queries) || !item.queries.every((query) => typeof query === 'string'))) return false
  if (item.sources != null && (!Array.isArray(item.sources) || !item.sources.every(isWebSource))) return false
  return true
}

export function readThreadRunHandoff(threadId: string): ThreadRunHandoffRef | null {
  if (!canUseLocalStorage() || !threadId) return null
  try {
    const raw = localStorage.getItem(threadRunHandoffKey(threadId))
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    if (!parsed || typeof parsed !== 'object') return null
    const item = parsed as Record<string, unknown>
    const runId = typeof item.runId === 'string' ? item.runId.trim() : ''
    const status = item.status
    if (!runId) return null
    if (status !== 'cancelled' && status !== 'interrupted' && status !== 'failed') return null
    const assistantTurn = item.assistantTurn == null ? null : parseAssistantTurnData(item.assistantTurn)
    const sources = Array.isArray(item.sources) ? item.sources.filter(isWebSource) : []
    const artifacts = Array.isArray(item.artifacts) ? item.artifacts.filter(isArtifactRef) : []
    const widgets = Array.isArray(item.widgets) ? item.widgets.filter(isWidgetRef) : []
    const codeExecutions = Array.isArray(item.codeExecutions) ? item.codeExecutions.filter(isCodeExecutionRef) : []
    const browserActions = Array.isArray(item.browserActions) ? item.browserActions.filter(isBrowserActionRef) : []
    const subAgents = Array.isArray(item.subAgents) ? item.subAgents.filter(isSubAgentRef) : []
    const fileOps = Array.isArray(item.fileOps) ? item.fileOps.filter(isFileOpRef) : []
    const webFetches = Array.isArray(item.webFetches) ? item.webFetches.filter(isWebFetchRef) : []
    const searchSteps = Array.isArray(item.searchSteps) ? item.searchSteps.filter(isMessageSearchStepRef) : []
    return {
      runId,
      status,
      assistantTurn,
      sources,
      artifacts,
      widgets,
      codeExecutions,
      browserActions,
      subAgents,
      fileOps,
      webFetches,
      searchSteps,
    }
  } catch {
    return null
  }
}

export function writeThreadRunHandoff(threadId: string, data: ThreadRunHandoffRef): void {
  if (!canUseLocalStorage() || !threadId || !data.runId) return
  try {
    localStorage.setItem(threadRunHandoffKey(threadId), JSON.stringify(data))
  } catch { /* ignore */ }
}

export function clearThreadRunHandoff(threadId: string): void {
  if (!canUseLocalStorage() || !threadId) return
  try {
    localStorage.removeItem(threadRunHandoffKey(threadId))
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

const DEVELOPER_MODE_KEY = 'arkloop:web:developer_mode'

export function readDeveloperMode(): boolean {
  if (!canUseLocalStorage()) return false
  try {
    return localStorage.getItem(DEVELOPER_MODE_KEY) === 'true'
  } catch { return false }
}

export function writeDeveloperMode(value: boolean): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(DEVELOPER_MODE_KEY, value ? 'true' : 'false')
    window.dispatchEvent(new CustomEvent('arkloop:developer_mode', { detail: value }))
  } catch { /* ignore */ }
}

const DEVELOPER_SHOW_DEBUG_PANEL_KEY = 'arkloop:web:developer_show_debug_panel'

export function readDeveloperShowDebugPanel(): boolean {
  if (!canUseLocalStorage()) return false
  try {
    return localStorage.getItem(DEVELOPER_SHOW_DEBUG_PANEL_KEY) === 'true'
  } catch { return false }
}

export function writeDeveloperShowDebugPanel(value: boolean): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(DEVELOPER_SHOW_DEBUG_PANEL_KEY, value ? 'true' : 'false')
    window.dispatchEvent(new CustomEvent('arkloop:developer_show_debug_panel', { detail: value }))
  } catch { /* ignore */ }
}

const DEVELOPER_PIPELINE_TRACE_KEY = 'arkloop:web:developer_pipeline_trace_enabled'

export function readDeveloperPipelineTraceEnabled(): boolean {
  if (!canUseLocalStorage()) return false
  try {
    return localStorage.getItem(DEVELOPER_PIPELINE_TRACE_KEY) === 'true'
  } catch { return false }
}

export function writeDeveloperPipelineTraceEnabled(value: boolean): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(DEVELOPER_PIPELINE_TRACE_KEY, value ? 'true' : 'false')
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

export function readThreadModes(): Record<string, AppMode> {
  if (!canUseLocalStorage()) return {}
  try {
    const raw = localStorage.getItem(THREAD_MODES_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw) as Record<string, string>
    const next: Record<string, AppMode> = {}
    for (const [threadId, mode] of Object.entries(parsed)) {
      next[threadId] = mode === 'work' ? 'work' : 'chat'
    }
    return next
  } catch {
    return {}
  }
}

export function writeThreadMode(threadId: string, mode: AppMode): void {
  if (!canUseLocalStorage() || !threadId) return
  try {
    const map = readThreadModes()
    map[threadId] = mode
    localStorage.setItem(THREAD_MODES_KEY, JSON.stringify(map))
  } catch { /* ignore */ }
}

export function readThreadMode(threadId: string): AppMode {
  if (!canUseLocalStorage() || !threadId) return 'chat'
  try {
    const map = readThreadModes()
    const mode = map[threadId]
    return mode === 'work' ? 'work' : 'chat'
  } catch { return 'chat' }
}

// -- Work Folder --

const WORK_FOLDER_KEY = 'arkloop:web:work_folder'
const WORK_RECENT_FOLDERS_KEY = 'arkloop:web:work_recent_folders'

function threadWorkFolderKey(threadId: string): string {
  return `arkloop:web:work_folder:${threadId}`
}

function addToRecents(folder: string): void {
  try {
    const raw = localStorage.getItem(WORK_RECENT_FOLDERS_KEY)
    const recents: string[] = raw ? (JSON.parse(raw) as string[]) : []
    const next = [folder, ...recents.filter((f) => f !== folder)].slice(0, 8)
    localStorage.setItem(WORK_RECENT_FOLDERS_KEY, JSON.stringify(next))
  } catch { /* ignore */ }
}

// Global (pending) key — used before a thread is created
export function readWorkFolder(): string | null {
  if (!canUseLocalStorage()) return null
  try {
    return localStorage.getItem(WORK_FOLDER_KEY) || null
  } catch { return null }
}

export function writeWorkFolder(folder: string): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.setItem(WORK_FOLDER_KEY, folder)
    addToRecents(folder)
  } catch { /* ignore */ }
  window.dispatchEvent(new CustomEvent('arkloop:work-folder-changed'))
}

export function clearWorkFolder(): void {
  if (!canUseLocalStorage()) return
  try {
    localStorage.removeItem(WORK_FOLDER_KEY)
  } catch { /* ignore */ }
  window.dispatchEvent(new CustomEvent('arkloop:work-folder-changed'))
}

// Per-thread key — used once a thread exists
export function readThreadWorkFolder(threadId: string): string | null {
  if (!canUseLocalStorage() || !threadId) return null
  try {
    return localStorage.getItem(threadWorkFolderKey(threadId)) || null
  } catch { return null }
}

export function writeThreadWorkFolder(threadId: string, folder: string): void {
  if (!canUseLocalStorage() || !threadId) return
  try {
    localStorage.setItem(threadWorkFolderKey(threadId), folder)
    addToRecents(folder)
  } catch { /* ignore */ }
  window.dispatchEvent(new CustomEvent('arkloop:work-folder-changed'))
}

export function clearThreadWorkFolder(threadId: string): void {
  if (!canUseLocalStorage() || !threadId) return
  try {
    localStorage.removeItem(threadWorkFolderKey(threadId))
  } catch { /* ignore */ }
  window.dispatchEvent(new CustomEvent('arkloop:work-folder-changed'))
}

// Called when a new thread is created: move the pending global folder to the thread
export function transferGlobalWorkFolderToThread(threadId: string): void {
  if (!canUseLocalStorage() || !threadId) return
  try {
    const pending = localStorage.getItem(WORK_FOLDER_KEY)
    if (!pending) return
    localStorage.setItem(threadWorkFolderKey(threadId), pending)
    localStorage.removeItem(WORK_FOLDER_KEY)
  } catch { /* ignore */ }
  window.dispatchEvent(new CustomEvent('arkloop:work-folder-changed'))
}

export function transferGlobalThinkingToThread(threadId: string): void {
  if (!canUseLocalStorage() || !threadId) return
  try {
    if (localStorage.getItem(SELECTED_THINKING_KEY) !== 'true') return
    localStorage.setItem(`arkloop:thinking:${threadId}`, 'true')
  } catch { /* ignore */ }
}

export function readWorkRecentFolders(): string[] {
  if (!canUseLocalStorage()) return []
  try {
    const raw = localStorage.getItem(WORK_RECENT_FOLDERS_KEY)
    if (!raw) return []
    return JSON.parse(raw) as string[]
  } catch { return [] }
}

// -- Thread Thinking Toggle --

export function readThreadThinkingEnabled(threadId: string): boolean {
  if (!canUseLocalStorage() || !threadId) return false
  return localStorage.getItem(`arkloop:thinking:${threadId}`) === 'true'
}

export function writeThreadThinkingEnabled(threadId: string, enabled: boolean): void {
  if (!canUseLocalStorage() || !threadId) return
  try {
    if (enabled) {
      localStorage.setItem(`arkloop:thinking:${threadId}`, 'true')
    } else {
      localStorage.removeItem(`arkloop:thinking:${threadId}`)
    }
  } catch { /* ignore */ }
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
    const widgets = readMessageWidgets(old_id)
    if (widgets) writeMessageWidgets(new_id, widgets)
    const codeExec = readMessageCodeExecutions(old_id)
    if (codeExec) writeMessageCodeExecutions(new_id, codeExec)
    const thinking = readMessageThinking(old_id)
    if (thinking) writeMessageThinking(new_id, thinking)
    const searchSteps = readMessageSearchSteps(old_id)
    if (searchSteps) writeMessageSearchSteps(new_id, searchSteps)
    const copBlocks = readMessageCopBlocks(old_id)
    if (copBlocks) writeMessageCopBlocks(new_id, copBlocks)
    const assistantTurn = readMessageAssistantTurn(old_id)
    if (assistantTurn) writeMessageAssistantTurn(new_id, assistantTurn)
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
  if (!canUseLocalStorage()) return { fontFamily: 'default', codeFontFamily: 'jetbrains-mono', fontSize: 'normal' }
  try {
    const raw = localStorage.getItem(FONT_SETTINGS_KEY)
    if (!raw) return { fontFamily: 'default', codeFontFamily: 'jetbrains-mono', fontSize: 'normal' }
    const parsed = JSON.parse(raw) as Partial<FontSettings>
    return {
      fontFamily: (['default', 'inter', 'system', 'serif', 'noto-sans', 'source-sans', 'custom'] as FontFamily[]).includes(parsed.fontFamily as FontFamily) ? parsed.fontFamily as FontFamily : 'default',
      codeFontFamily: (['jetbrains-mono', 'fira-code', 'cascadia-code', 'source-code-pro'] as CodeFontFamily[]).includes(parsed.codeFontFamily as CodeFontFamily) ? parsed.codeFontFamily as CodeFontFamily : 'jetbrains-mono',
      fontSize: (['compact', 'normal', 'relaxed'] as FontSize[]).includes(parsed.fontSize as FontSize) ? parsed.fontSize as FontSize : 'normal',
    }
  } catch {
    return { fontFamily: 'default', codeFontFamily: 'jetbrains-mono', fontSize: 'normal' }
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
