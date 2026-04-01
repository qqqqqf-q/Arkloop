import { copSegmentCalls, type AssistantTurnSegment, type AssistantTurnUi } from './assistantTurnSegments'
import type {
  CodeExecutionRef,
  FileOpRef,
  MessageSearchStepRef,
  SubAgentRef,
  WebFetchRef,
  WebSource,
} from './storage'
import type { WebSearchPhaseStep } from './components/CopTimeline'
import { isWebFetchToolName } from './runEventProcessing'
import { isWebSearchToolName } from './webSearchTimelineFromRunEvent'

type CopSegment = Extract<AssistantTurnSegment, { type: 'cop' }>
export type GenericToolCallRef = {
  id: string
  toolName: string
  label: string
  output?: string
  status: 'running' | 'success' | 'failed'
  errorMessage?: string
  seq?: number
}

const CODE_EXECUTION_TOOL_NAMES = new Set(['python_execute', 'exec_command', 'write_stdin'])
const SUB_AGENT_TOOL_NAMES = new Set([
  'spawn_agent', 'acp_agent', 'spawn_acp',
  'send_input', 'wait_agent', 'resume_agent', 'close_agent', 'interrupt_agent',
  'send_acp', 'wait_acp', 'close_acp', 'interrupt_acp',
])
const FILE_OP_TOOL_NAMES = new Set([
  'grep', 'glob', 'read_file', 'read', 'write_file', 'edit', 'edit_file',
  'load_tools', 'memory_write', 'memory_search', 'memory_read', 'memory_forget',
])
const AUXILIARY_RENDERED_TOOL_NAMES = new Set([
  'show_widget',
  'create_artifact',
  'document_write',
  'browser',
])

function sortBySeq<T extends { seq?: number }>(items: T[]): T[] {
  return [...items].sort((a, b) => (a.seq ?? 0) - (b.seq ?? 0))
}

function isKnownTimelineTool(toolName: string): boolean {
  if (toolName === 'read' || toolName.startsWith('read.')) return true
  return (
    CODE_EXECUTION_TOOL_NAMES.has(toolName) ||
    SUB_AGENT_TOOL_NAMES.has(toolName) ||
    FILE_OP_TOOL_NAMES.has(toolName) ||
    AUXILIARY_RENDERED_TOOL_NAMES.has(toolName) ||
    isWebSearchToolName(toolName) ||
    isWebFetchToolName(toolName)
  )
}

type WebSearchPhaseStepLike = Pick<MessageSearchStepRef, 'id' | 'kind' | 'label' | 'status' | 'queries' | 'seq' | 'resultSeq' | 'sources'>

/**
 * 仅返回 CopTimeline 已支持的数据子集（代码 / 子代理 / 文件 / 抓取 / 搜索阶段步骤）。
 * segment 内有 toolCallId 但池子尚未匹配时返回 { steps:[], sources:[] }，避免外层把整条 COP 拆掉。
 */
export function copTimelinePayloadForSegment(
  segment: CopSegment,
  pools: {
    codeExecutions?: CodeExecutionRef[] | null
    fileOps?: FileOpRef[] | null
    webFetches?: WebFetchRef[] | null
    subAgents?: SubAgentRef[] | null
    searchSteps?: WebSearchPhaseStepLike[] | null
    sources: WebSource[]
  },
): {
  steps: WebSearchPhaseStep[]
  sources: WebSource[]
  codeExecutions?: CodeExecutionRef[]
  fileOps?: FileOpRef[]
  webFetches?: WebFetchRef[]
  subAgents?: SubAgentRef[]
  genericTools?: GenericToolCallRef[]
} {
  const calls = copSegmentCalls(segment)
  const ids = new Set(calls.map((c) => c.toolCallId))

  const codeExecutions = sortBySeq((pools.codeExecutions ?? []).filter((x) => ids.has(x.id)))
  const fileOps = sortBySeq((pools.fileOps ?? []).filter((x) => ids.has(x.id)))
  const webFetches = sortBySeq((pools.webFetches ?? []).filter((x) => ids.has(x.id)))
  const subAgents = sortBySeq((pools.subAgents ?? []).filter((x) => ids.has(x.id)))

  const steps: WebSearchPhaseStep[] = sortBySeq(
    (pools.searchSteps ?? [])
      .filter((s) => ids.has(s.id))
      .map((s) => ({
        id: s.id,
        kind: s.kind,
        label: s.label,
        status: s.status,
        queries: s.queries,
        resultSeq: s.resultSeq,
        seq: s.seq,
      })),
  )
  const sourcesById = new Map(
    (pools.searchSteps ?? [])
      .filter((s) => ids.has(s.id) && Array.isArray(s.sources) && s.sources.length > 0)
      .map((s) => [s.id, s.sources ?? []] as const),
  )
  const stepsWithScopedSources: WebSearchPhaseStep[] = steps.flatMap((step) => {
    if (step.kind !== 'searching') return [step]
    const scopedSources = sourcesById.get(step.id)
    if (!scopedSources || scopedSources.length === 0) return [step]
    const reviewingSeq = step.resultSeq ?? step.seq ?? 0
    return [
      step,
      {
        id: `${step.id}::reviewing`,
        kind: 'reviewing',
        label: 'Reviewing sources',
        status: step.status,
        sources: scopedSources,
        seq: reviewingSeq,
      },
    ]
  })
  // per-segment sources: 只收集当前 segment 的 search steps 自带的 sources
  const segmentSources = [...sourcesById.values()].flat()
  // 如果 segment 的 search steps 有自己的 sources 就用，否则回退到全局 pool（兼容无 per-step sources 的旧数据）
  const sources = segmentSources.length > 0 ? segmentSources : (steps.length > 0 ? pools.sources : [])
  const renderedIds = new Set<string>([
    ...codeExecutions.map((item) => item.id),
    ...fileOps.map((item) => item.id),
    ...webFetches.map((item) => item.id),
    ...subAgents.map((item) => item.id),
    ...steps.map((item) => item.id),
  ])
  const genericTools = sortBySeq(
    segment.items
      .filter((item): item is Extract<CopSegment['items'][number], { kind: 'call' }> => item.kind === 'call')
      .filter((item) => !renderedIds.has(item.call.toolCallId) && !isKnownTimelineTool(item.call.toolName))
      .map((item): GenericToolCallRef => {
        const call = item.call
        const hasError = typeof call.errorClass === 'string' && call.errorClass.trim() !== ''
        const output = call.result == null
          ? undefined
          : typeof call.result === 'string'
            ? call.result
            : JSON.stringify(call.result)
        const previewEntries = Object.entries(call.arguments).slice(0, 2)
        const preview = previewEntries.length > 0
          ? `${call.toolName} ${previewEntries.map(([key, value]) => `${key}=${typeof value === 'string' ? value : JSON.stringify(value)}`).join(' ')}`
          : call.toolName
        return {
          id: call.toolCallId,
          toolName: call.toolName,
          label: preview,
          output,
          status: hasError ? 'failed' : call.result === undefined ? 'running' : 'success',
          errorMessage: hasError ? call.errorClass : undefined,
          seq: item.seq,
        }
      }),
  )

  const hasRich =
    stepsWithScopedSources.length > 0 ||
    codeExecutions.length > 0 ||
    fileOps.length > 0 ||
    webFetches.length > 0 ||
    subAgents.length > 0 ||
    genericTools.length > 0

  // 仅有 thinking、无 call：仍返回壳子供 CopTimeline 挂 thinkingRows
  if (calls.length === 0) {
    return { steps: [], sources: [] }
  }

  // 有 toolCall 但池子尚未对齐时仍返回壳子，避免流式结束/刷新间隙整条 COP 被 ChatPage 直接 return null 拆掉
  if (!hasRich) {
    return { steps: [], sources: [] }
  }

  return {
    steps: stepsWithScopedSources,
    sources,
    ...(codeExecutions.length > 0 ? { codeExecutions } : {}),
    ...(fileOps.length > 0 ? { fileOps } : {}),
    ...(webFetches.length > 0 ? { webFetches } : {}),
    ...(subAgents.length > 0 ? { subAgents } : {}),
    ...(genericTools.length > 0 ? { genericTools } : {}),
  }
}

/** COP 段内已由 CopTimeline 渲染的条目 id（与 allStreamItems 互斥，避免双份工具 UI） */
export function toolCallIdsInCopTimelines(
  turn: AssistantTurnUi,
  pools: {
    codeExecutions?: CodeExecutionRef[] | null
    fileOps?: FileOpRef[] | null
    webFetches?: WebFetchRef[] | null
    subAgents?: SubAgentRef[] | null
    searchSteps?: WebSearchPhaseStepLike[] | null
    sources: WebSource[]
  },
): Set<string> {
  const ids = new Set<string>()
  for (const seg of turn.segments) {
    if (seg.type !== 'cop') continue
    const payload = copTimelinePayloadForSegment(seg, pools)
    for (const s of payload.steps) ids.add(s.id)
    for (const c of payload.codeExecutions ?? []) ids.add(c.id)
    for (const f of payload.fileOps ?? []) ids.add(f.id)
    for (const w of payload.webFetches ?? []) ids.add(w.id)
    for (const a of payload.subAgents ?? []) ids.add(a.id)
    for (const g of payload.genericTools ?? []) ids.add(g.id)
  }
  return ids
}
