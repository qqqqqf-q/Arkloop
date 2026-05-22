import { memo } from 'react'
import type { AssistantTurnSegment } from '../assistantTurnSegments'
import type { CodeExecution } from './CodeExecutionCard'
import type { ArtifactRef, CodeExecutionRef, FileOpRef, SubAgentRef, WebFetchRef, WebSource } from '../storage'
import type { WebSearchPhaseStep } from './cop-timeline/CopTimeline'
import { CopTimeline } from './cop-timeline/CopTimeline'
import { buildResolvedPool, buildSubSegments, buildThinkingOnlyFromItems, segmentLiveTitle } from '../copSubSegment'
import {
  copTimelinePayloadForSegment,
  deriveTodoChanges,
  type TodoWriteRef,
} from '../copSegmentTimeline'

type CopSegment = Extract<AssistantTurnSegment, { type: 'cop' }>

type Props = {
  segment: CopSegment
  keyPrefix: string
  codeExecutions?: CodeExecutionRef[] | null
  fileOps?: FileOpRef[] | null
  webFetches?: WebFetchRef[] | null
  subAgents?: SubAgentRef[] | null
  searchSteps?: WebSearchPhaseStep[] | null
  sources: WebSource[]
  isComplete: boolean
  live?: boolean
  shimmer?: boolean
  thinkingHint?: string
  headerOverride?: string
  compactNarrativeEnd?: boolean
  onOpenCodeExecution?: (ce: CodeExecution) => void
  activeCodeExecutionId?: string
  onOpenSubAgent?: (agent: SubAgentRef) => void
  onOpenDocument?: (artifact: ArtifactRef, options?: { trigger?: HTMLElement | null; artifacts?: ArtifactRef[]; runId?: string }) => void
  artifacts?: ArtifactRef[] | null
  runId?: string
  accessToken?: string
  baseUrl?: string
  typography?: 'default' | 'work'
  todoWritesForFinalDisplay?: TodoWriteRef[] | null
}

function countCompletedTodos(todo: TodoWriteRef): number {
  return todo.completedCount ?? todo.todos.filter((item) => item.status === 'completed').length
}

function hydrateTodoFromPreviousWrite(todo: TodoWriteRef, allTodos: TodoWriteRef[]): TodoWriteRef {
  if ((todo.oldTodos?.length ?? 0) > 0 || todo.todos.length === 0) return todo
  const previous = allTodos
    .filter((item) => item.id !== todo.id && (item.seq ?? 0) < (todo.seq ?? 0) && item.todos.length > 0)
    .sort((left, right) => (right.seq ?? 0) - (left.seq ?? 0))[0]
  if (!previous) return todo
  const changes = deriveTodoChanges(previous.todos, todo.todos)
  if (changes.length === 0) return todo
  return { ...todo, oldTodos: previous.todos, changes }
}

function todoForFinalDisplay(todo: TodoWriteRef, allTodos: TodoWriteRef[]): TodoWriteRef {
  const hydrated = hydrateTodoFromPreviousWrite(todo, allTodos)
  const latest = allTodos
    .filter((item) => item.todos.length > 0)
    .sort((left, right) => (right.seq ?? 0) - (left.seq ?? 0))[0]
  if (!latest || latest.id === hydrated.id) return hydrated
  return {
    ...hydrated,
    todos: latest.todos,
    completedCount: countCompletedTodos(latest),
    totalCount: latest.totalCount ?? latest.todos.length,
  }
}

export const CopSegmentBlocks = memo(function CopSegmentBlocks({
  segment,
  keyPrefix,
  codeExecutions,
  fileOps,
  webFetches,
  subAgents,
  searchSteps,
  sources,
  isComplete,
  live,
  shimmer,
  thinkingHint,
  headerOverride,
  compactNarrativeEnd,
  onOpenCodeExecution,
  activeCodeExecutionId,
  onOpenSubAgent,
  onOpenDocument,
  artifacts,
  runId,
  accessToken,
  baseUrl,
  typography = 'default',
  todoWritesForFinalDisplay,
}: Props) {
  const pools = { codeExecutions, fileOps, webFetches, subAgents, searchSteps, sources }
  const payload = copTimelinePayloadForSegment(segment, pools)
  const effectiveHeaderOverride = headerOverride ?? segment.title?.trim() ?? undefined
  const todoWrites = payload.todoWrites && payload.todoWrites.length > 0
    ? payload.todoWrites.map((todo) => todoForFinalDisplay(todo, todoWritesForFinalDisplay ?? payload.todoWrites ?? []))
    : undefined
  const timelinePayload = todoWrites ? { ...payload, todoWrites } : payload
  const pool = buildResolvedPool(timelinePayload)
  for (const agent of subAgents ?? []) pool.subAgents.set(agent.id, agent)
  const subSegments = buildSubSegments(segment.items)
  if (subSegments.length > 0 && live) {
    const lastSeg = subSegments[subSegments.length - 1]!
    lastSeg.status = 'open'
    lastSeg.title = segmentLiveTitle(lastSeg.category)
  }

  const thinkingOnlyData = subSegments.length === 0 &&
    !timelinePayload.codeExecutions?.length &&
    !timelinePayload.subAgents?.length &&
    !timelinePayload.fileOps?.length &&
    !timelinePayload.webFetches?.length &&
    !timelinePayload.genericTools?.length &&
    !timelinePayload.todoWrites?.length
    ? buildThinkingOnlyFromItems(segment.items)
    : null

  const hasTimelineBody =
    subSegments.length > 0 ||
    thinkingOnlyData != null ||
    timelinePayload.steps.length > 0 ||
    timelinePayload.sources.length > 0 ||
    !!timelinePayload.fileOps?.length ||
    !!timelinePayload.webFetches?.length ||
    !!timelinePayload.genericTools?.length ||
    !!timelinePayload.subAgents?.length ||
    !!timelinePayload.todoWrites?.length ||
    !!(timelinePayload.exploreGroups && timelinePayload.exploreGroups.length > 0)

  if (!hasTimelineBody) return null

  return (
    <div className="cop-segment-block">
      <CopTimeline
        key={`${keyPrefix}-timeline`}
        segments={subSegments}
        pool={pool}
        thinkingOnly={thinkingOnlyData}
        thinkingHint={thinkingHint}
        headerOverride={effectiveHeaderOverride}
        isComplete={isComplete}
        live={live}
        shimmer={live && !!shimmer}
        compactNarrativeEnd={compactNarrativeEnd}
        onOpenCodeExecution={onOpenCodeExecution}
        onOpenSubAgent={onOpenSubAgent}
        onOpenDocument={onOpenDocument}
        artifacts={artifacts}
        runId={runId}
        activeCodeExecutionId={activeCodeExecutionId}
        accessToken={accessToken}
        baseUrl={baseUrl}
        typography={typography}
      />
    </div>
  )
})
