import type { AssistantTurnSegment, AssistantTurnUi } from './assistantTurnSegments'
import type {
  CodeExecutionRef,
  FileOpRef,
  MessageSearchStepRef,
  SubAgentRef,
  WebFetchRef,
  WebSource,
} from './storage'
import type { WebSearchPhaseStep } from './components/CopTimeline'

type CopSegment = Extract<AssistantTurnSegment, { type: 'cop' }>

function sortBySeq<T extends { seq?: number }>(items: T[]): T[] {
  return [...items].sort((a, b) => (a.seq ?? 0) - (b.seq ?? 0))
}

type WebSearchPhaseStepLike = Pick<MessageSearchStepRef, 'id' | 'kind' | 'label' | 'status' | 'queries' | 'seq'>

/**
 * 仅返回 CopTimeline 已支持的数据子集（代码 / 子代理 / 文件 / 抓取 / 搜索阶段步骤）。
 * 其余 tool call 不生成任何 UI。
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
} | null {
  const ids = new Set(segment.calls.map((c) => c.toolCallId))
  if (ids.size === 0) return null

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
        seq: s.seq,
      })),
  )

  const hasSearchish = steps.some((s) => s.kind === 'searching' || s.kind === 'reviewing')
  const sources = hasSearchish ? pools.sources : []

  const hasAny =
    steps.length > 0 ||
    codeExecutions.length > 0 ||
    fileOps.length > 0 ||
    webFetches.length > 0 ||
    subAgents.length > 0

  if (!hasAny) return null

  return {
    steps,
    sources,
    ...(codeExecutions.length > 0 ? { codeExecutions } : {}),
    ...(fileOps.length > 0 ? { fileOps } : {}),
    ...(webFetches.length > 0 ? { webFetches } : {}),
    ...(subAgents.length > 0 ? { subAgents } : {}),
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
    if (!payload) continue
    for (const s of payload.steps) ids.add(s.id)
    for (const c of payload.codeExecutions ?? []) ids.add(c.id)
    for (const f of payload.fileOps ?? []) ids.add(f.id)
    for (const w of payload.webFetches ?? []) ids.add(w.id)
    for (const a of payload.subAgents ?? []) ids.add(a.id)
  }
  return ids
}
