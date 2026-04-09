import { debugBus } from '@arkloop/shared'

const DEBUG_PANEL_KEY = 'arkloop:web:developer_show_debug_panel'

function isStreamDebugEnabled(): boolean {
  if (!import.meta.env.DEV || typeof window === 'undefined') return false
  try {
    return window.localStorage.getItem(DEBUG_PANEL_KEY) === 'true'
  } catch {
    return false
  }
}

export function emitStreamDebug(type: string, data: unknown, source = 'show-widget') {
  if (!isStreamDebugEnabled()) return
  debugBus.emit({
    ts: Date.now(),
    type,
    source,
    data,
  })
}

type ShowWidgetDebugState = {
  runId: string
  toolCallId: string
  toolCallIndex: number
  title: string | null
  firstDeltaAt: number
  firstDeltaSeq: number | null
  lastSeq: number | null
  lastContentLength: number
  firstVisibleAt?: number
  firstVisibleContentLength?: number
  shellReadyAt?: number
  firstDomAt?: number
  scriptsDoneAt?: number
  completedAt?: number
}

const showWidgetStates = new Map<string, ShowWidgetDebugState>()

function showWidgetStateKey(runId: string, toolCallId: string | undefined, toolCallIndex: number): string {
  return `${runId}::${toolCallId || `idx:${toolCallIndex}`}`
}

function getShowWidgetState(input: {
  runId: string
  toolCallId?: string | null
  toolCallIndex: number
  title?: string | null
  seq?: number | null
  contentLength?: number
}): ShowWidgetDebugState {
  const normalizedToolCallId = input.toolCallId ?? ''
  const key = showWidgetStateKey(input.runId, normalizedToolCallId, input.toolCallIndex)
  const now = Date.now()
  let state = showWidgetStates.get(key)
  if (!state) {
    state = {
      runId: input.runId,
      toolCallId: normalizedToolCallId,
      toolCallIndex: input.toolCallIndex,
      title: input.title ?? null,
      firstDeltaAt: now,
      firstDeltaSeq: input.seq ?? null,
      lastSeq: input.seq ?? null,
      lastContentLength: input.contentLength ?? 0,
    }
    showWidgetStates.set(key, state)
    return state
  }
  if (input.title != null) state.title = input.title
  if (input.seq != null) state.lastSeq = input.seq
  if (input.contentLength != null) state.lastContentLength = input.contentLength
  return state
}

export function noteShowWidgetDelta(input: {
  runId: string
  toolCallId?: string | null
  toolCallIndex: number
  title?: string | null
  seq?: number | null
  contentLength: number
}) {
  if (!isStreamDebugEnabled()) return
  getShowWidgetState(input)
}

export function noteShowWidgetStatus(input: {
  runId: string
  toolCallId?: string | null
  toolCallIndex: number
  title?: string | null
  seq?: number | null
  contentLength: number
  visible: boolean
  complete: boolean
}) {
  if (!isStreamDebugEnabled()) return
  const state = getShowWidgetState(input)
  const now = Date.now()

  if (input.visible && state.firstVisibleAt == null) {
    state.firstVisibleAt = now
    state.firstVisibleContentLength = input.contentLength
    emitStreamDebug('show-widget:first-visible', {
      runId: state.runId,
      toolCallId: state.toolCallId,
      toolCallIndex: state.toolCallIndex,
      title: state.title,
      firstDeltaSeq: state.firstDeltaSeq,
      firstVisibleSeq: state.lastSeq,
      firstVisibleContentLength: state.firstVisibleContentLength,
      timeToFirstVisibleMs: now - state.firstDeltaAt,
    })
  }

  if (input.complete && state.completedAt == null) {
    state.completedAt = now
    emitStreamDebug('show-widget:completed', {
      runId: state.runId,
      toolCallId: state.toolCallId,
      toolCallIndex: state.toolCallIndex,
      title: state.title,
      firstDeltaSeq: state.firstDeltaSeq,
      completedSeq: state.lastSeq,
      firstVisibleContentLength: state.firstVisibleContentLength ?? null,
      completedContentLength: input.contentLength,
      timeToFirstVisibleMs: state.firstVisibleAt != null ? state.firstVisibleAt - state.firstDeltaAt : null,
      timeToCompleteMs: now - state.firstDeltaAt,
      timeVisibleToCompleteMs: state.firstVisibleAt != null ? now - state.firstVisibleAt : null,
    })
    showWidgetStates.delete(showWidgetStateKey(state.runId, state.toolCallId, state.toolCallIndex))
  }
}

export function noteShowWidgetPhase(input: {
  runId: string
  toolCallId?: string | null
  toolCallIndex: number
  title?: string | null
  seq?: number | null
  contentLength: number
  phase: 'shell-ready' | 'first-dom' | 'scripts-done'
}) {
  if (!isStreamDebugEnabled()) return
  const state = getShowWidgetState(input)
  const now = Date.now()

  if (input.phase === 'shell-ready' && state.shellReadyAt == null) {
    state.shellReadyAt = now
    emitStreamDebug('show-widget:shell-ready', {
      runId: state.runId,
      toolCallId: state.toolCallId,
      toolCallIndex: state.toolCallIndex,
      title: state.title,
      contentLength: input.contentLength,
      timeToShellReadyMs: now - state.firstDeltaAt,
    })
    return
  }

  if (input.phase === 'first-dom' && state.firstDomAt == null) {
    state.firstDomAt = now
    emitStreamDebug('show-widget:first-dom', {
      runId: state.runId,
      toolCallId: state.toolCallId,
      toolCallIndex: state.toolCallIndex,
      title: state.title,
      contentLength: input.contentLength,
      timeToFirstDomMs: now - state.firstDeltaAt,
      timeShellReadyToFirstDomMs: state.shellReadyAt != null ? now - state.shellReadyAt : null,
    })
    return
  }

  if (input.phase === 'scripts-done' && state.scriptsDoneAt == null) {
    state.scriptsDoneAt = now
    emitStreamDebug('show-widget:scripts-done', {
      runId: state.runId,
      toolCallId: state.toolCallId,
      toolCallIndex: state.toolCallIndex,
      title: state.title,
      contentLength: input.contentLength,
      timeToScriptsDoneMs: now - state.firstDeltaAt,
      timeFirstDomToScriptsDoneMs: state.firstDomAt != null ? now - state.firstDomAt : null,
    })
  }
}
