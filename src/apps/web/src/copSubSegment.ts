import type { CopBlockItem } from './assistantTurnSegments'
import { normalizeToolName } from './toolPresentation'

export type CopSegmentCategory = 'explore' | 'exec' | 'edit' | 'agent' | 'fetch' | 'generic'

export type CopSubSegment = {
  id: string
  category: CopSegmentCategory
  status: 'open' | 'closed'
  items: CopBlockItem[]
  seq: number
  title: string
}

const EXPLORE_NAMES = new Set(['read_file', 'grep', 'glob', 'load_tools', 'load_skill', 'lsp'])
const EXEC_NAMES = new Set(['exec_command', 'python_execute', 'continue_process', 'terminate_process'])
const EDIT_NAMES = new Set(['edit', 'edit_file', 'write_file'])
const AGENT_NAMES = new Set([
  'spawn_agent', 'acp_agent', 'spawn_acp',
  'send_input', 'wait_agent', 'resume_agent', 'close_agent', 'interrupt_agent',
  'send_acp', 'wait_acp', 'close_acp', 'interrupt_acp',
])
const FETCH_NAMES = new Set(['web_fetch'])
const MUTATING_LSP = new Set(['rename'])

export function categoryForTool(toolName: string): CopSegmentCategory {
  const n = normalizeToolName(toolName)
  if (EXPLORE_NAMES.has(n)) {
    if (n === 'lsp' && MUTATING_LSP.has(toolName)) return 'edit'
    return 'explore'
  }
  if (EXEC_NAMES.has(n)) return 'exec'
  if (EDIT_NAMES.has(n)) return 'edit'
  if (AGENT_NAMES.has(n)) return 'agent'
  if (FETCH_NAMES.has(n)) return 'fetch'
  return 'generic'
}

export function segmentLiveTitle(cat: CopSegmentCategory): string {
  switch (cat) {
    case 'explore': return 'Exploring code...'
    case 'exec': return 'Running...'
    case 'edit': return 'Editing...'
    case 'agent': return 'Agent running...'
    case 'fetch': return 'Fetching...'
    case 'generic': return 'Working...'
  }
}

function basename(path: string): string {
  const normalized = path.replace(/\\/g, '/')
  return normalized.split('/').filter(Boolean).pop() ?? path
}

export function segmentCompletedTitle(seg: CopSubSegment): string {
  const calls = seg.items
    .filter((i): i is Extract<CopBlockItem, { kind: 'call' }> => i.kind === 'call')
    .map((i) => i.call)
  if (calls.length === 0) return 'Completed'

  switch (seg.category) {
    case 'explore': {
      const readPaths = new Set(calls.filter((c) => normalizeToolName(c.toolName) === 'read_file').map((c) => c.arguments?.file_path as string | undefined ?? ''))
      const searchCount = calls.filter((c) => {
        const n = normalizeToolName(c.toolName)
        return n === 'grep' || n === 'lsp'
      }).length
      const globCount = calls.filter((c) => normalizeToolName(c.toolName) === 'glob').length
      const parts: string[] = []
      if (readPaths.size > 0) parts.push(`Read ${readPaths.size} file${readPaths.size === 1 ? '' : 's'}`)
      if (searchCount > 0) parts.push(`${searchCount} search${searchCount === 1 ? '' : 'es'}`)
      if (globCount > 0) parts.push(`Listed ${globCount} file${globCount === 1 ? '' : 's'}`)
      return parts.length > 0 ? parts.join(', ') : 'Explored code'
    }
    case 'exec': {
      const n = calls.length
      return `${n} step${n === 1 ? '' : 's'} completed`
    }
    case 'edit': {
      const editCall = calls[0]
      const filePath = (editCall?.arguments?.file_path as string | undefined) ?? ''
      return filePath ? `Edited ${basename(filePath)}` : 'Edit completed'
    }
    case 'agent': {
      const n = calls.length
      return n === 1 ? 'Agent completed' : `${n} agent tasks completed`
    }
    case 'fetch': return 'Fetch completed'
    case 'generic': {
      if (calls.length === 1) {
        const t = calls[0]!.toolName
        // Map known generic tool names to readable labels
        const label: Record<string, string> = {
          todo_write: 'Updated todos',
          todo_read: 'Read todos',
        }
        return label[t] ?? t
      }
      return `${calls.length} steps completed`
    }
  }
}

export function buildSubSegments(items: CopBlockItem[]): CopSubSegment[] {
  const segments: CopSubSegment[] = []
  let currentItems: CopBlockItem[] = []
  let currentCat: CopSegmentCategory | null = null
  let pendingLead: CopBlockItem[] = [] // think/text before any tool call

  const closeCurrent = () => {
    if (currentItems.length === 0 && pendingLead.length === 0) return
    if (currentCat == null) return
    const allItems = [...pendingLead, ...currentItems]
    if (allItems.length === 0) return
    const seg: CopSubSegment = {
      id: `seg-${segments.length}`,
      category: currentCat,
      status: 'closed',
      items: allItems,
      seq: allItems[0]?.seq ?? 0,
      title: segmentLiveTitle(currentCat),
    }
    seg.title = segmentCompletedTitle(seg)
    segments.push(seg)
    currentItems = []
    currentCat = null
    pendingLead = []
  }

  for (const item of items) {
    if (item.kind === 'call') {
      const cat = categoryForTool(item.call.toolName)
      if (currentCat !== null && cat !== currentCat) {
        closeCurrent()
      }
      if (currentCat === null) {
        currentCat = cat
        currentItems = []
      }
      currentItems.push(item)
    } else {
      // thinking or assistant_text
      if (currentCat !== null) {
        currentItems.push(item)
      } else {
        pendingLead.push(item)
      }
    }
  }

  closeCurrent()

  return segments
}

// -- Resolved pool builder --

import type { CopTimelinePayload } from './copSegmentTimeline'

function mapById<T extends { id: string }>(arr: T[]): Map<string, T> {
  const m = new Map<string, T>()
  for (const item of arr) m.set(item.id, item)
  return m
}

export type ResolvedPool = {
  codeExecutions: Map<string, import('./storage').CodeExecutionRef>
  fileOps: Map<string, import('./storage').FileOpRef>
  webFetches: Map<string, import('./storage').WebFetchRef>
  subAgents: Map<string, import('./storage').SubAgentRef>
  genericTools: Map<string, import('./copSegmentTimeline').GenericToolCallRef>
  steps: Map<string, import('./components/cop-timeline/types').WebSearchPhaseStep>
  sources: import('./storage').WebSource[]
}

export function buildResolvedPool(payload: CopTimelinePayload): ResolvedPool {
  const fileOps = new Map<string, import('./storage').FileOpRef>()
  for (const op of payload.fileOps ?? []) fileOps.set(op.id, op)
  for (const group of payload.exploreGroups ?? []) {
    for (const op of group.items) fileOps.set(op.id, op)
  }
  return {
    codeExecutions: mapById(payload.codeExecutions ?? []),
    fileOps,
    webFetches: mapById(payload.webFetches ?? []),
    subAgents: mapById(payload.subAgents ?? []),
    genericTools: mapById(payload.genericTools ?? []),
    steps: mapById(payload.steps),
    sources: payload.sources,
  }
}

function emptyMap<K extends string, V>(): Map<K, V> {
  return new Map<K, V>()
}

export const EMPTY_POOL: ResolvedPool = {
  codeExecutions: emptyMap(),
  fileOps: emptyMap(),
  webFetches: emptyMap(),
  subAgents: emptyMap(),
  genericTools: emptyMap(),
  steps: emptyMap(),
  sources: [],
}

export function buildFallbackSegments(tools: {
  codeExecutions?: Array<{ id: string; seq?: number }> | null
  subAgents?: Array<{ id: string; sourceTool?: string; seq?: number }> | null
  fileOps?: Array<{ id: string; toolName: string; seq?: number }> | null
  webFetches?: Array<{ id: string; seq?: number }> | null
}): CopSubSegment[] {
  const segs: CopSubSegment[] = []
  const allTools: Array<{ id: string; toolName: string; seq?: number }> = []
  if (tools.codeExecutions) {
    for (const c of tools.codeExecutions) allTools.push({ id: c.id, toolName: 'exec_command', seq: c.seq })
  }
  if (tools.subAgents) {
    for (const a of tools.subAgents) allTools.push({ id: a.id, toolName: a.sourceTool ?? 'spawn_agent', seq: a.seq })
  }
  if (tools.fileOps) {
    for (const f of tools.fileOps) allTools.push({ id: f.id, toolName: f.toolName, seq: f.seq })
  }
  if (tools.webFetches) {
    for (const w of tools.webFetches) allTools.push({ id: w.id, toolName: 'web_fetch', seq: w.seq })
  }
  allTools.sort((a, b) => (a.seq ?? 0) - (b.seq ?? 0))
  if (allTools.length === 0) return segs

  const items: CopBlockItem[] = allTools.map((t) => ({
    kind: 'call' as const,
    call: { toolCallId: t.id, toolName: t.toolName, arguments: {} as Record<string, unknown> },
    seq: t.seq ?? 0,
  }))

  segs.push({
    id: 'fallback-tools',
    category: 'generic',
    status: 'closed',
    items,
    seq: items[0]?.seq ?? 0,
    title: `${allTools.length} step${allTools.length === 1 ? '' : 's'} completed`,
  })
  return segs
}

export function buildThinkingOnlyFromItems(items: { kind: string; content?: string; seq: number; startedAtMs?: number; endedAtMs?: number }[]): { markdown: string; live?: boolean; durationSec: number; startedAtMs?: number } | null {
  let markdown = ''
  let live = false
  let startedAtMs: number | undefined
  for (const item of items) {
    if (item.kind === 'thinking' && item.content) {
      markdown += item.content
      if (item.endedAtMs == null) live = true
      if (startedAtMs == null && item.startedAtMs != null) startedAtMs = item.startedAtMs
    }
  }
  if (!markdown.trim()) return null
  return { markdown, live, durationSec: 0, startedAtMs }
}
