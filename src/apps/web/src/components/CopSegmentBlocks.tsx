import { memo } from 'react'
import type { AssistantTurnSegment } from '../assistantTurnSegments'
import type { CodeExecution } from './CodeExecutionCard'
import type { CodeExecutionRef, FileOpRef, SubAgentRef, WebFetchRef, WebSource } from '../storage'
import type { WebSearchPhaseStep } from './cop-timeline/CopTimeline'
import { CopTimeline } from './cop-timeline/CopTimeline'
import { buildResolvedPool, buildSubSegments, buildThinkingOnlyFromItems, segmentLiveTitle } from '../copSubSegment'
import { basename, presentationForTool, stringArg } from '../toolPresentation'
import { contentText } from '../timelineText'
import {
  copTimelinePayloadForSegment,
  deriveTodoChanges,
  splitCopItemsByTopLevelTools,
  type GenericToolCallRef,
  type TodoWriteRef,
} from '../copSegmentTimeline'
import { TopLevelCopToolBlock, type TopLevelCopToolEntry } from './TopLevelCopToolBlock'

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
  accessToken?: string
  baseUrl?: string
  typography?: 'default' | 'work'
  todoWritesForFinalDisplay?: TodoWriteRef[] | null
}

function topLevelEntryForTool(
  entry: Extract<ReturnType<typeof splitCopItemsByTopLevelTools>[number], { kind: 'tool' }>,
  payload: ReturnType<typeof copTimelinePayloadForSegment>,
  todoWritesForFinalDisplay?: TodoWriteRef[] | null,
): TopLevelCopToolEntry | null {
  const id = entry.item.call.toolCallId
  const codeExecution = payload.codeExecutions?.find((item) => item.id === id)
  if (codeExecution) {
    return { kind: 'code', id, seq: entry.seq, item: codeExecution }
  }
  const todo = payload.todoWrites?.find((item) => item.id === id)
  if (todo) {
    return { kind: 'todo', id, seq: entry.seq, item: todoForFinalDisplay(todo, todoWritesForFinalDisplay ?? payload.todoWrites ?? []) }
  }
  const fileOp = payload.fileOps?.find((item) => item.id === id)
  if (fileOp) {
    return { kind: 'file', id, seq: entry.seq, item: fileOp }
  }
  const generic = payload.genericTools?.find((item) => item.id === id) ?? genericRootToolFromCall(entry.item)
  if (generic) {
    return { kind: 'generic', id, seq: entry.seq, item: generic }
  }
  return null
}

function previewFromArgs(toolName: string, args: Record<string, unknown>): string | undefined {
  switch (toolName) {
    case 'write_file':
    case 'document_write':
    case 'create_artifact':
      return stringArg(args, 'content') || undefined
    case 'show_widget':
      return stringArg(args, 'widget_code') || undefined
    case 'edit':
    case 'edit_file':
      return stringArg(args, 'new_string') || stringArg(args, 'replacement') || stringArg(args, 'content') || undefined
    case 'image_generate':
      return stringArg(args, 'prompt') || undefined
    default:
      return undefined
  }
}

function genericRootToolFromCall(item: Extract<Extract<AssistantTurnSegment, { type: 'cop' }>['items'][number], { kind: 'call' }>): GenericToolCallRef | null {
  const call = item.call
  const hasError = typeof call.errorClass === 'string' && call.errorClass.trim() !== ''
  const status: GenericToolCallRef['status'] = hasError ? 'failed' : call.result === undefined ? 'running' : 'success'
  const args = call.arguments ?? {}
  const presentation = presentationForTool(call.toolName, args)
  const filename = stringArg(args, 'filename') || stringArg(args, 'file_path')
  const title = stringArg(args, 'title') || stringArg(args, 'name')
  const label = call.displayDescription || title || (filename ? basename(filename) : presentation.description || call.toolName)
  const displayText = call.displayDescription
    ? contentText(call.displayDescription)
    : title
      ? contentText(title)
      : filename
        ? contentText(basename(filename))
        : presentation.text
  const preview = status === 'running' ? previewFromArgs(call.toolName, args) : undefined
  return {
    id: call.toolCallId,
    toolName: call.toolName,
    label,
    ...(call.displayDescription || presentation.description !== call.toolName ? { displayDescription: call.displayDescription || presentation.description } : {}),
    displayText,
    ...(preview ? { output: preview } : {}),
    status,
    errorMessage: hasError ? call.errorMessage ?? call.errorClass : undefined,
    seq: item.seq,
  }
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
  accessToken,
  baseUrl,
  typography = 'default',
  todoWritesForFinalDisplay,
}: Props) {
  const splitEntries = splitCopItemsByTopLevelTools(segment.items, { segmentTitle: segment.title })
  if (splitEntries.length === 0) return null

  const pools = { codeExecutions, fileOps, webFetches, subAgents, searchSteps, sources }
  const fullPayload = copTimelinePayloadForSegment(segment, pools)
  const timelineEntryCount = splitEntries.filter((entry) => entry.kind === 'timeline').length
  const effectiveHeaderOverride = headerOverride ?? segment.title?.trim() ?? undefined

  return (
    <>
      {splitEntries.map((entry, index) => {
        const entryLive = !!live && index === splitEntries.length - 1
        const entryComplete = isComplete || !entryLive

        if (entry.kind === 'tool') {
          const toolEntry = topLevelEntryForTool(entry, fullPayload, todoWritesForFinalDisplay)
          if (!toolEntry) return null
          return (
            <TopLevelCopToolBlock
              key={`${keyPrefix}-tool-${entry.id}`}
              entry={toolEntry}
              live={entryLive}
              onOpenCodeExecution={onOpenCodeExecution}
              activeCodeExecutionId={activeCodeExecutionId}
            />
          )
        }

        // Single-call entry without process context → render directly
        if (entry.items.length === 1 && entry.items[0]?.kind === 'call') {
          const singleCall = entry.items[0]!
          // Construct a tool entry for topLevelEntryForTool
          const toolEntry: Extract<ReturnType<typeof splitCopItemsByTopLevelTools>[number], { kind: 'tool' }> = {
            kind: 'tool',
            id: singleCall.call.toolCallId,
            seq: singleCall.seq,
            item: singleCall,
          }
          const toolResult = topLevelEntryForTool(toolEntry, fullPayload, todoWritesForFinalDisplay)
          if (toolResult) {
            return (
              <TopLevelCopToolBlock
                key={`${keyPrefix}-tool-${entry.id}`}
                entry={toolResult}
                live={entryLive}
                onOpenCodeExecution={onOpenCodeExecution}
                activeCodeExecutionId={activeCodeExecutionId}
              />
            )
          }
          return null
        }

        const timelineSegment: CopSegment = { ...segment, items: entry.items }
        const payload = copTimelinePayloadForSegment(timelineSegment, pools)
        const pool = buildResolvedPool(payload)
        const subSegments = buildSubSegments(entry.items)
        if (subSegments.length > 0 && entryLive) {
          const lastSeg = subSegments[subSegments.length - 1]!
          lastSeg.status = 'open'
          lastSeg.title = segmentLiveTitle(lastSeg.category)
        }

        const thinkingOnlyData = subSegments.length === 0 &&
          !payload.codeExecutions?.length &&
          !payload.subAgents?.length &&
          !payload.fileOps?.length &&
          !payload.webFetches?.length &&
          !payload.genericTools?.length &&
          !payload.todoWrites?.length
          ? buildThinkingOnlyFromItems(entry.items)
          : null

        const hasTimelineBody =
          subSegments.length > 0 ||
          thinkingOnlyData != null ||
          payload.steps.length > 0 ||
          payload.sources.length > 0 ||
          !!payload.fileOps?.length ||
          !!payload.webFetches?.length ||
          !!payload.genericTools?.length ||
          !!payload.subAgents?.length ||
          !!(payload.exploreGroups && payload.exploreGroups.length > 0)

        if (!hasTimelineBody) return null

        return (
          <CopTimeline
            key={`${keyPrefix}-timeline-${entry.id}`}
            segments={subSegments}
            pool={pool}
            thinkingOnly={thinkingOnlyData}
            thinkingHint={thinkingHint}
            headerOverride={timelineEntryCount === 1 ? effectiveHeaderOverride : undefined}
            isComplete={entryComplete}
            live={entryLive}
            shimmer={entryLive && !!shimmer}
            compactNarrativeEnd={compactNarrativeEnd}
            onOpenCodeExecution={onOpenCodeExecution}
            onOpenSubAgent={onOpenSubAgent}
            activeCodeExecutionId={activeCodeExecutionId}
            accessToken={accessToken}
            baseUrl={baseUrl}
            typography={typography}
          />
        )
      })}
    </>
  )
})
