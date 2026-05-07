import { copSegmentCalls, type AssistantTurnSegment, type AssistantTurnUi } from './assistantTurnSegments'
import type { CopBlockItem } from './assistantTurnSegments'
import type {
  CodeExecutionRef,
  FileOpRef,
  MessageSearchStepRef,
  SubAgentRef,
  WebFetchRef,
  WebSource,
} from './storage'
import type { WebSearchPhaseStep } from './components/CopTimeline'
import { isWebFetchToolName } from './agentEventProcessing'
import { exploreGroupLabel, isExploreFileOp, type ExploreGroupRef } from './toolPresentation'
import {
  DEFAULT_SEARCHING_LABEL,
  COMPLETED_SEARCHING_LABEL,
  isWebSearchToolName,
  webSearchQueriesFromArguments,
  webSearchSourcesFromResult,
} from './webSearchTimelineFromAgentEvent'

type CopSegment = Extract<AssistantTurnSegment, { type: 'cop' }>
export type GenericToolCallRef = {
  id: string
  toolName: string
  label: string
  displayDescription?: string
  output?: string
  emptyLabel?: string
  status: 'running' | 'success' | 'failed'
  errorMessage?: string
  seq?: number
}

export type TodoItemRef = {
  id: string
  content: string
  activeForm?: string
  status: 'pending' | 'in_progress' | 'completed' | 'cancelled'
}

export type TodoChangeRef = {
  type: 'created' | 'updated' | 'removed'
  id: string
  content: string
  status?: TodoItemRef['status']
  previousStatus?: TodoItemRef['status']
  index?: number
  previousIndex?: number
  oldContent?: string
  activeForm?: string
  oldActiveForm?: string
}

export type TodoWriteRef = {
  id: string
  toolName: 'todo_write'
  todos: TodoItemRef[]
  oldTodos?: TodoItemRef[]
  changes?: TodoChangeRef[]
  completedCount?: number
  totalCount?: number
  status: 'running' | 'success' | 'failed'
  errorMessage?: string
  seq?: number
}

const CODE_EXECUTION_TOOL_NAMES = new Set(['python_execute', 'exec_command', 'continue_process', 'terminate_process'])
const TODO_TOOL_NAMES = new Set(['todo_write'])
const SUB_AGENT_TOOL_NAMES = new Set([
  'spawn_agent',
  'send_input', 'wait_agent', 'resume_agent', 'close_agent', 'interrupt_agent',
])
const FILE_OP_TOOL_NAMES = new Set([
  'grep', 'glob', 'read_file', 'read', 'write_file', 'edit', 'edit_file',
  'load_tools', 'memory_write', 'memory_edit', 'memory_search', 'memory_read', 'memory_forget',
  'notebook_write', 'notebook_read', 'notebook_edit', 'notebook_forget',
])
const AUXILIARY_RENDERED_TOOL_NAMES = new Set([
  'show_widget',
  'create_artifact',
  'document_write',
  'browser',
])
const IMAGE_GENERATE_TOOL_NAME = 'image_generate'

function sortBySeq<T extends { seq?: number }>(items: T[]): T[] {
  return [...items].sort((a, b) => (a.seq ?? 0) - (b.seq ?? 0))
}

export function isTopLevelCopToolName(toolName: string): boolean {
  return CODE_EXECUTION_TOOL_NAMES.has(toolName) || TODO_TOOL_NAMES.has(toolName)
}

export type SplitCopItemEntry =
  | { kind: 'timeline'; id: string; seq: number; items: CopBlockItem[] }
  | { kind: 'tool'; id: string; seq: number; item: Extract<CopBlockItem, { kind: 'call' }> }

export function splitCopItemsByTopLevelTools(items: CopBlockItem[]): SplitCopItemEntry[] {
  const entries: SplitCopItemEntry[] = []
  let current: CopBlockItem[] = []
  let timelineIndex = 0

  const flushCurrent = () => {
    if (current.length === 0) return
    entries.push({
      kind: 'timeline',
      id: `timeline-${timelineIndex}`,
      seq: current[0]?.seq ?? 0,
      items: current,
    })
    timelineIndex += 1
    current = []
  }

  for (const item of items) {
    if (item.kind === 'call' && isTopLevelCopToolName(item.call.toolName)) {
      flushCurrent()
      entries.push({
        kind: 'tool',
        id: item.call.toolCallId,
        seq: item.seq,
        item,
      })
      continue
    }
    current.push(item)
  }

  flushCurrent()
  return entries
}

function pickCodeExecutionMode(args: Record<string, unknown>): CodeExecutionRef['mode'] | undefined {
  const mode = args.mode
  return mode === 'buffered' || mode === 'follow' || mode === 'stdin' || mode === 'pty'
    ? mode
    : undefined
}

function fallbackCodeExecutionFromCall(call: ReturnType<typeof copSegmentCalls>[number], seq: number): CodeExecutionRef | null {
  if (!CODE_EXECUTION_TOOL_NAMES.has(call.toolName)) return null
  const code = typeof call.arguments.command === 'string' ? call.arguments.command
    : typeof call.arguments.code === 'string' ? call.arguments.code
      : typeof call.arguments.cmd === 'string' ? call.arguments.cmd
        : undefined
  return {
    id: call.toolCallId,
    language: call.toolName === 'python_execute' ? 'python' : 'shell',
    mode: call.toolName === 'exec_command' ? pickCodeExecutionMode(call.arguments) : undefined,
    code,
    displayDescription: call.displayDescription,
    status: call.errorClass ? 'failed' : call.result === undefined ? 'running' : 'completed',
    errorClass: call.errorClass,
    seq,
  }
}

function resolveGroupStatus(items: FileOpRef[]): ExploreGroupRef['status'] {
  if (items.some((item) => item.status === 'running')) return 'running'
  if (items.some((item) => item.status === 'failed')) return 'failed'
  return 'success'
}

function groupConsecutiveExploreFileOps(calls: ReturnType<typeof copSegmentCalls>, fileOps: FileOpRef[]): ExploreGroupRef[] {
  if (fileOps.length === 0) return []

  const fileOpById = new Map(fileOps.map((item) => [item.id, item] as const))
  const groups: ExploreGroupRef[] = []
  let currentItems: FileOpRef[] = []

  const flushCurrent = () => {
    if (currentItems.length === 0) return
    const status = resolveGroupStatus(currentItems)
    groups.push({
      id: `explore:${currentItems.map((item) => item.id).join(':')}`,
      label: exploreGroupLabel(currentItems, status),
      status,
      items: currentItems,
      seq: currentItems[0]?.seq,
    })
    currentItems = []
  }

  for (const call of calls) {
    const op = fileOpById.get(call.toolCallId)
    if (op && isExploreFileOp(op)) {
      currentItems.push(op)
    } else {
      flushCurrent()
    }
  }
  flushCurrent()

  return groups
}

function isKnownTimelineTool(toolName: string): boolean {
  if (toolName === 'read' || toolName.startsWith('read.')) return true
  return (
    CODE_EXECUTION_TOOL_NAMES.has(toolName) ||
    TODO_TOOL_NAMES.has(toolName) ||
    SUB_AGENT_TOOL_NAMES.has(toolName) ||
    FILE_OP_TOOL_NAMES.has(toolName) ||
    AUXILIARY_RENDERED_TOOL_NAMES.has(toolName) ||
    isWebSearchToolName(toolName) ||
    isWebFetchToolName(toolName)
  )
}

function pluralize(count: number, singular: string, plural = `${singular}s`): string {
  return `${count} ${count === 1 ? singular : plural}`
}

function summarizeGenericResult(result: unknown): { output?: string; emptyLabel?: string } {
  if (result == null) return { emptyLabel: 'Completed; no displayable output returned' }
  if (typeof result === 'string') {
    const trimmed = result.trim()
    return trimmed
      ? { output: `returned text · ${pluralize(Array.from(trimmed).length, 'char')}` }
      : { emptyLabel: 'Returned empty text' }
  }
  if (Array.isArray(result)) return { output: `returned array · ${pluralize(result.length, 'item')}` }
  if (typeof result === 'object') return { output: `returned object · ${pluralize(Object.keys(result as Record<string, unknown>).length, 'key')}` }
  if (typeof result === 'boolean') return { output: `returned boolean · ${result ? 'true' : 'false'}` }
  if (typeof result === 'number') return { output: `returned number · ${result}` }
  return { output: 'returned value' }
}

function genericImageGenerateLabel(status: GenericToolCallRef['status']): string {
  switch (status) {
    case 'running': return 'Generating image'
    case 'success': return 'Generated image'
    case 'failed': return 'Image generation failed'
  }
}

function genericImageGenerateInput(args: Record<string, unknown>, fallback: string): string {
  const prompt = typeof args.prompt === 'string' ? args.prompt.trim() : ''
  const inputImages = Array.isArray(args.input_images)
    ? args.input_images.filter((item): item is string => typeof item === 'string' && item.trim() !== '').map((item) => item.trim())
    : []
  const options = [
    typeof args.size === 'string' && args.size.trim() ? `size=${args.size.trim()}` : '',
    typeof args.quality === 'string' && args.quality.trim() ? `quality=${args.quality.trim()}` : '',
    typeof args.background === 'string' && args.background.trim() ? `background=${args.background.trim()}` : '',
    typeof args.output_format === 'string' && args.output_format.trim() ? `format=${args.output_format.trim()}` : '',
    inputImages.length > 0 ? `input_images=${inputImages.join(', ')}` : '',
  ].filter(Boolean)
  const lines = [
    prompt ? `prompt: ${prompt}` : '',
    ...options,
  ].filter(Boolean)
  return lines.length > 0 ? lines.join('\n') : fallback
}

function summarizeImageGenerateResult(result: unknown): { output?: string; emptyLabel?: string } {
  if (!result || typeof result !== 'object' || Array.isArray(result)) return { emptyLabel: 'No image metadata returned' }
  const r = result as Record<string, unknown>
  const artifacts = Array.isArray(r.artifacts) ? r.artifacts : []
  const parts = [
    typeof r.provider === 'string' && r.provider.trim() ? `provider: ${r.provider.trim()}` : '',
    typeof r.model === 'string' && r.model.trim() ? `model: ${r.model.trim()}` : '',
    typeof r.mime_type === 'string' && r.mime_type.trim() ? `type: ${r.mime_type.trim()}` : '',
    typeof r.bytes === 'number' ? `bytes: ${r.bytes}` : '',
    artifacts.length > 0 ? `artifacts: ${artifacts.length}` : '',
    typeof r.revised_prompt === 'string' && r.revised_prompt.trim() ? `revised_prompt: ${r.revised_prompt.trim()}` : '',
  ].filter(Boolean)
  return parts.length > 0 ? { output: parts.join('\n') } : { emptyLabel: 'No image metadata returned' }
}

function parseTodoItems(value: unknown): TodoItemRef[] {
  if (!Array.isArray(value)) return []
  return value.flatMap((entry) => {
    if (!entry || typeof entry !== 'object') return []
    const item = entry as Record<string, unknown>
    const id = typeof item.id === 'string' ? item.id.trim() : ''
    const content = typeof item.content === 'string' ? item.content.trim() : ''
    const activeFormRaw = typeof item.active_form === 'string' ? item.active_form
      : typeof item.activeForm === 'string' ? item.activeForm
        : ''
    const activeForm = activeFormRaw.trim()
    const status = item.status
    if (!id || !content) return []
    if (status !== 'pending' && status !== 'in_progress' && status !== 'completed' && status !== 'cancelled') return []
    return [{ id, content, ...(activeForm ? { activeForm } : {}), status }]
  })
}

function parseTodoStatus(value: unknown): TodoItemRef['status'] | undefined {
  return value === 'pending' || value === 'in_progress' || value === 'completed' || value === 'cancelled'
    ? value
    : undefined
}

function parseTodoChanges(value: unknown): TodoChangeRef[] {
  if (!Array.isArray(value)) return []
  return value.flatMap((entry) => {
    if (!entry || typeof entry !== 'object') return []
    const item = entry as Record<string, unknown>
    const type = item.type
    const id = typeof item.id === 'string' ? item.id.trim() : ''
    const content = typeof item.content === 'string' ? item.content.trim() : ''
    if ((type !== 'created' && type !== 'updated' && type !== 'removed') || !id || !content) return []
    const status = parseTodoStatus(item.status)
    const previousStatus = parseTodoStatus(item.previous_status ?? item.previousStatus)
    const index = typeof item.index === 'number' ? item.index : undefined
    const previousIndexRaw = item.previous_index ?? item.previousIndex
    const previousIndex = typeof previousIndexRaw === 'number' ? previousIndexRaw : undefined
    const oldContentRaw = item.old_content ?? item.oldContent
    const activeFormRaw = item.active_form ?? item.activeForm
    const oldActiveFormRaw = item.old_active_form ?? item.oldActiveForm
    const oldContent = typeof oldContentRaw === 'string' && oldContentRaw.trim() ? oldContentRaw.trim() : undefined
    const activeForm = typeof activeFormRaw === 'string' && activeFormRaw.trim() ? activeFormRaw.trim() : undefined
    const oldActiveForm = typeof oldActiveFormRaw === 'string' && oldActiveFormRaw.trim() ? oldActiveFormRaw.trim() : undefined
    return [{
      type,
      id,
      content,
      ...(status ? { status } : {}),
      ...(previousStatus ? { previousStatus } : {}),
      ...(typeof index === 'number' ? { index } : {}),
      ...(typeof previousIndex === 'number' ? { previousIndex } : {}),
      ...(oldContent ? { oldContent } : {}),
      ...(activeForm ? { activeForm } : {}),
      ...(oldActiveForm ? { oldActiveForm } : {}),
    }]
  })
}

export function deriveTodoChanges(oldTodos: TodoItemRef[], newTodos: TodoItemRef[]): TodoChangeRef[] {
  if (oldTodos.length === 0) return []
  const oldByID = new Map(oldTodos.map((item, index) => [item.id, { item, index }] as const))
  const newIDs = new Set<string>()
  const changes: TodoChangeRef[] = []

  newTodos.forEach((item, index) => {
    newIDs.add(item.id)
    const old = oldByID.get(item.id)
    if (!old) {
      changes.push({
        type: 'created',
        id: item.id,
        content: item.content,
        status: item.status,
        index,
        ...(item.activeForm ? { activeForm: item.activeForm } : {}),
      })
      return
    }
    if (old.item.content === item.content && old.item.status === item.status && old.item.activeForm === item.activeForm) {
      return
    }
    changes.push({
      type: 'updated',
      id: item.id,
      content: item.content,
      status: item.status,
      previousStatus: old.item.status,
      index,
      oldContent: old.item.content,
      ...(item.activeForm ? { activeForm: item.activeForm } : {}),
      ...(old.item.activeForm ? { oldActiveForm: old.item.activeForm } : {}),
    })
  })

  oldTodos.forEach((item, previousIndex) => {
    if (newIDs.has(item.id)) return
    changes.push({
      type: 'removed',
      id: item.id,
      content: item.content,
      previousStatus: item.status,
      previousIndex,
      ...(item.activeForm ? { oldActiveForm: item.activeForm } : {}),
    })
  })
  return changes
}

function resultArrayField(result: Record<string, unknown>, snakeKey: string, camelKey: string): unknown {
  return result[snakeKey] ?? result[camelKey]
}

function resultNumberField(result: Record<string, unknown>, snakeKey: string, camelKey: string): number | undefined {
  const value = result[snakeKey] ?? result[camelKey]
  return typeof value === 'number' ? value : undefined
}

function todoWriteFromCall(item: Extract<CopSegment['items'][number], { kind: 'call' }>): TodoWriteRef | null {
  const call = item.call
  if (!TODO_TOOL_NAMES.has(call.toolName)) return null
  const result = call.result && typeof call.result === 'object'
    ? call.result as Record<string, unknown>
    : null
  const resultTodos = result ? parseTodoItems(result.todos) : []
  const argumentTodos = parseTodoItems(call.arguments.todos)
  const hasError = typeof call.errorClass === 'string' && call.errorClass.trim() !== ''
  const oldTodos = result ? parseTodoItems(resultArrayField(result, 'old_todos', 'oldTodos')) : []
  const parsedChanges = result ? parseTodoChanges(result.changes) : []
  const changes = parsedChanges.length > 0 ? parsedChanges : deriveTodoChanges(oldTodos, resultTodos)
  const completedCount = result ? resultNumberField(result, 'completed_count', 'completedCount') : undefined
  const totalCount = result ? resultNumberField(result, 'total_count', 'totalCount') : undefined
  return {
    id: call.toolCallId,
    toolName: 'todo_write',
    todos: resultTodos.length > 0 ? resultTodos : argumentTodos,
    ...(oldTodos.length > 0 ? { oldTodos } : {}),
    ...(changes.length > 0 ? { changes } : {}),
    ...(typeof completedCount === 'number' ? { completedCount } : {}),
    ...(typeof totalCount === 'number' ? { totalCount } : {}),
    status: hasError ? 'failed' : call.result === undefined ? 'running' : 'success',
    ...(hasError ? { errorMessage: call.errorClass } : {}),
    seq: item.seq,
  }
}

type WebSearchPhaseStepLike = Pick<MessageSearchStepRef, 'id' | 'kind' | 'label' | 'status' | 'queries' | 'seq' | 'resultSeq' | 'sources'>

function fallbackWebSearchStepsForSegment(
  segment: CopSegment,
  knownStepIds: Set<string>,
  globalSources: WebSource[],
): WebSearchPhaseStep[] {
  const fallbackSteps: WebSearchPhaseStep[] = []
  let lastSearchStepId: string | null = null
  let lastSearchStepSeq: number | undefined
  let hasScopedSources = false

  for (const item of segment.items) {
    if (item.kind !== 'call') continue
    const { call, seq } = item
    if (!isWebSearchToolName(call.toolName)) continue
    if (knownStepIds.has(call.toolCallId)) continue

    const resultSources = webSearchSourcesFromResult(call.result)
    const searchStatus: WebSearchPhaseStep['status'] =
      call.result !== undefined || call.errorClass != null ? 'done' : 'active'
    fallbackSteps.push({
      id: call.toolCallId,
      kind: 'searching',
      label: searchStatus === 'done' ? COMPLETED_SEARCHING_LABEL : DEFAULT_SEARCHING_LABEL,
      status: searchStatus,
      queries: webSearchQueriesFromArguments(call.arguments),
      seq,
      ...(resultSources ? { sources: resultSources } : {}),
    })
    lastSearchStepId = call.toolCallId
    lastSearchStepSeq = seq

    if (resultSources && resultSources.length > 0) {
      hasScopedSources = true
    }
  }

  if (!hasScopedSources && globalSources.length > 0 && lastSearchStepId) {
    fallbackSteps.push({
      id: `${lastSearchStepId}::reviewing`,
      kind: 'reviewing',
      label: 'Reviewing sources',
      status: 'done',
      sources: globalSources,
      seq: typeof lastSearchStepSeq === 'number' ? lastSearchStepSeq + 0.5 : undefined,
    })
  }

  return fallbackSteps
}

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
  exploreGroups?: ExploreGroupRef[]
  webFetches?: WebFetchRef[]
  subAgents?: SubAgentRef[]
  genericTools?: GenericToolCallRef[]
  todoWrites?: TodoWriteRef[]
} {
  const calls = copSegmentCalls(segment)
  const ids = new Set(calls.map((c) => c.toolCallId))

  const existingCodeExecutions = sortBySeq((pools.codeExecutions ?? []).filter((x) => ids.has(x.id)))
  const existingCodeExecutionIds = new Set(existingCodeExecutions.map((item) => item.id))
  const fallbackCodeExecutions = sortBySeq(
    segment.items
      .filter((item): item is Extract<CopSegment['items'][number], { kind: 'call' }> => item.kind === 'call')
      .filter((item) => !existingCodeExecutionIds.has(item.call.toolCallId))
      .map((item) => fallbackCodeExecutionFromCall(item.call, item.seq))
      .filter((item): item is CodeExecutionRef => item != null),
  )
  const codeExecutions = sortBySeq([...existingCodeExecutions, ...fallbackCodeExecutions])
  const allFileOps = sortBySeq((pools.fileOps ?? []).filter((x) => ids.has(x.id)))
  const exploreFileOps = allFileOps.filter(isExploreFileOp)
  const fileOps = allFileOps.filter((op) => !isExploreFileOp(op))
  const exploreGroups = groupConsecutiveExploreFileOps(calls, exploreFileOps)
  const webFetches = sortBySeq((pools.webFetches ?? []).filter((x) => ids.has(x.id)))
  const subAgents = sortBySeq((pools.subAgents ?? []).filter((x) => ids.has(x.id)))
  const todoWrites = sortBySeq(
    segment.items
      .filter((item): item is Extract<CopSegment['items'][number], { kind: 'call' }> => item.kind === 'call')
      .map(todoWriteFromCall)
      .filter((item): item is TodoWriteRef => item != null),
  )

  const mappedSteps: WebSearchPhaseStep[] = sortBySeq(
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
  const mappedSourcesById = new Map(
    (pools.searchSteps ?? [])
      .filter((s) => ids.has(s.id) && Array.isArray(s.sources) && s.sources.length > 0)
      .map((s) => [s.id, s.sources ?? []] as const),
  )
  const fallbackSteps = fallbackWebSearchStepsForSegment(segment, new Set(mappedSteps.map((step) => step.id)), pools.sources)
  const steps = sortBySeq([...mappedSteps, ...fallbackSteps])
  const sourcesById = new Map<string, WebSource[]>(mappedSourcesById)
  for (const step of fallbackSteps) {
    if (step.kind !== 'searching') continue
    if (!Array.isArray(step.sources) || step.sources.length === 0) continue
    sourcesById.set(step.id, step.sources)
  }

  const stepsWithScopedSources: WebSearchPhaseStep[] = steps.flatMap((step) => {
    if (step.kind === 'reviewing') return [step]
    if (step.kind !== 'searching') return [step]
    const scopedSources = sourcesById.get(step.id)
    if (!scopedSources || scopedSources.length === 0) return [step]
    const reviewingSeq = step.resultSeq ?? (typeof step.seq === 'number' ? step.seq + 0.5 : 0)
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
    ...allFileOps.map((item) => item.id),
    ...webFetches.map((item) => item.id),
    ...subAgents.map((item) => item.id),
    ...todoWrites.map((item) => item.id),
    ...steps.map((item) => item.id),
  ])
  const genericTools = sortBySeq(
    segment.items
      .filter((item): item is Extract<CopSegment['items'][number], { kind: 'call' }> => item.kind === 'call')
      .filter((item) => !renderedIds.has(item.call.toolCallId) && !isKnownTimelineTool(item.call.toolName))
      .map((item): GenericToolCallRef => {
        const call = item.call
        const hasError = typeof call.errorClass === 'string' && call.errorClass.trim() !== ''
        const status: GenericToolCallRef['status'] = hasError ? 'failed' : call.result === undefined ? 'running' : 'success'
        const isImageGenerate = call.toolName === IMAGE_GENERATE_TOOL_NAME
        const statusLabel = isImageGenerate ? genericImageGenerateLabel(status) : ''
        const resultSummary = isImageGenerate && status === 'success'
          ? summarizeImageGenerateResult(call.result)
          : summarizeGenericResult(call.result)
        const previewEntries = Object.entries(call.arguments).slice(0, 2)
        const preview = previewEntries.length > 0
          ? `${call.toolName} ${previewEntries.map(([key, value]) => `${key}=${typeof value === 'string' ? value : JSON.stringify(value)}`).join(' ')}`
          : call.toolName
        return {
          id: call.toolCallId,
          toolName: call.toolName,
          label: isImageGenerate ? genericImageGenerateInput(call.arguments, statusLabel) : call.displayDescription || preview,
          ...(isImageGenerate ? { displayDescription: statusLabel } : {}),
          output: resultSummary.output,
          emptyLabel: resultSummary.emptyLabel,
          status,
          errorMessage: hasError ? call.errorMessage ?? call.errorClass : undefined,
          seq: item.seq,
        }
      }),
  )

  const hasRich =
    stepsWithScopedSources.length > 0 ||
    codeExecutions.length > 0 ||
    fileOps.length > 0 ||
    exploreGroups.length > 0 ||
    webFetches.length > 0 ||
    subAgents.length > 0 ||
    todoWrites.length > 0 ||
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
    ...(exploreGroups.length > 0 ? { exploreGroups } : {}),
    ...(webFetches.length > 0 ? { webFetches } : {}),
    ...(subAgents.length > 0 ? { subAgents } : {}),
    ...(todoWrites.length > 0 ? { todoWrites } : {}),
    ...(genericTools.length > 0 ? { genericTools } : {}),
  }
}

export type CopTimelinePayload = ReturnType<typeof copTimelinePayloadForSegment>

export type CopTimelineBodySeqInput = {
  payload: CopTimelinePayload
  bodyFileOps?: FileOpRef[] | null
  thinkingRows?: Array<{ id: string; seq: number }> | null
  copInlineTextRows?: Array<{ id: string; seq: number }> | null
}

export type CopTimelineBodySlice = {
  id: string
  seq: number
  steps: WebSearchPhaseStep[]
  sources: WebSource[]
  codeExecutions?: CodeExecutionRef[]
  fileOps?: FileOpRef[]
  webFetches?: WebFetchRef[]
  genericTools?: GenericToolCallRef[]
  subAgents?: SubAgentRef[]
  todoWrites?: TodoWriteRef[]
  thinkingRows?: Array<{ id: string; seq: number }>
  copInlineTextRows?: Array<{ id: string; seq: number }>
}

export type PromotedCopTimelineEntry =
  | { kind: 'timeline'; id: string; seq: number; slice: CopTimelineBodySlice }
  | { kind: 'explore'; id: string; seq: number; attachedSlice?: CopTimelineBodySlice }
  | { kind: 'edit'; id: string; seq: number; attachedSlice?: CopTimelineBodySlice }

function itemSeq(item: { seq?: number }): number {
  return typeof item.seq === 'number' ? item.seq : Number.MAX_SAFE_INTEGER
}

function minSeq(items: Array<{ seq?: number } | undefined | null>): number | undefined {
  const values = items
    .map((item) => item?.seq)
    .filter((value): value is number => typeof value === 'number')
  return values.length > 0 ? Math.min(...values) : undefined
}

export function copTimelineBodySeq({ payload, bodyFileOps, thinkingRows, copInlineTextRows }: CopTimelineBodySeqInput): number {
  return minSeq([
    payload.steps[0],
    payload.codeExecutions?.[0],
    bodyFileOps?.[0],
    payload.webFetches?.[0],
    payload.genericTools?.[0],
    payload.subAgents?.[0],
    payload.todoWrites?.[0],
    thinkingRows?.[0],
    copInlineTextRows?.[0],
  ]) ?? Number.MAX_SAFE_INTEGER
}

type BodyBucket =
  | { kind: 'step'; item: WebSearchPhaseStep }
  | { kind: 'code'; item: CodeExecutionRef }
  | { kind: 'file'; item: FileOpRef }
  | { kind: 'fetch'; item: WebFetchRef }
  | { kind: 'generic'; item: GenericToolCallRef }
  | { kind: 'agent'; item: SubAgentRef }
  | { kind: 'todo'; item: TodoWriteRef }
  | { kind: 'thinking'; item: { id: string; seq: number } }
  | { kind: 'inline'; item: { id: string; seq: number } }

function makeBodySlice(id: string, buckets: BodyBucket[], sources: WebSource[]): CopTimelineBodySlice {
  const steps = buckets.filter((entry): entry is Extract<BodyBucket, { kind: 'step' }> => entry.kind === 'step').map((entry) => entry.item)
  const codeExecutions = buckets.filter((entry): entry is Extract<BodyBucket, { kind: 'code' }> => entry.kind === 'code').map((entry) => entry.item)
  const fileOps = buckets.filter((entry): entry is Extract<BodyBucket, { kind: 'file' }> => entry.kind === 'file').map((entry) => entry.item)
  const webFetches = buckets.filter((entry): entry is Extract<BodyBucket, { kind: 'fetch' }> => entry.kind === 'fetch').map((entry) => entry.item)
  const genericTools = buckets.filter((entry): entry is Extract<BodyBucket, { kind: 'generic' }> => entry.kind === 'generic').map((entry) => entry.item)
  const subAgents = buckets.filter((entry): entry is Extract<BodyBucket, { kind: 'agent' }> => entry.kind === 'agent').map((entry) => entry.item)
  const todoWrites = buckets.filter((entry): entry is Extract<BodyBucket, { kind: 'todo' }> => entry.kind === 'todo').map((entry) => entry.item)
  const thinkingRows = buckets.filter((entry): entry is Extract<BodyBucket, { kind: 'thinking' }> => entry.kind === 'thinking').map((entry) => entry.item)
  const copInlineTextRows = buckets.filter((entry): entry is Extract<BodyBucket, { kind: 'inline' }> => entry.kind === 'inline').map((entry) => entry.item)
  return {
    id,
    seq: buckets[0] ? itemSeq(buckets[0].item) : Number.MAX_SAFE_INTEGER,
    steps,
    sources: steps.length > 0 ? sources : [],
    ...(codeExecutions.length > 0 ? { codeExecutions } : {}),
    ...(fileOps.length > 0 ? { fileOps } : {}),
    ...(webFetches.length > 0 ? { webFetches } : {}),
    ...(genericTools.length > 0 ? { genericTools } : {}),
    ...(subAgents.length > 0 ? { subAgents } : {}),
    ...(todoWrites.length > 0 ? { todoWrites } : {}),
    ...(thinkingRows.length > 0 ? { thinkingRows } : {}),
    ...(copInlineTextRows.length > 0 ? { copInlineTextRows } : {}),
  }
}

export function promotedCopTimelineEntries(params: {
  payload: CopTimelinePayload
  hasTimelineBody: boolean
  bodyFileOps?: FileOpRef[] | null
  thinkingRows?: Array<{ id: string; seq: number }> | null
  copInlineTextRows?: Array<{ id: string; seq: number }> | null
}): PromotedCopTimelineEntry[] {
  const barriers = [
    ...(params.payload.exploreGroups ?? []).map((group) => ({ kind: 'explore' as const, id: group.id, seq: group.seq ?? Number.MAX_SAFE_INTEGER })),
    ...(params.payload.fileOps ?? [])
      .filter((op) => op.displayKind === 'edit')
      .map((op) => ({ kind: 'edit' as const, id: op.id, seq: op.seq ?? Number.MAX_SAFE_INTEGER })),
  ].sort((left, right) => left.seq - right.seq || left.kind.localeCompare(right.kind) || left.id.localeCompare(right.id))

  const bodyBuckets: BodyBucket[] = params.hasTimelineBody ? [
    ...params.payload.steps.map((item) => ({ kind: 'step' as const, item })),
    ...(params.payload.codeExecutions ?? []).map((item) => ({ kind: 'code' as const, item })),
    ...(params.bodyFileOps ?? []).map((item) => ({ kind: 'file' as const, item })),
    ...(params.payload.webFetches ?? []).map((item) => ({ kind: 'fetch' as const, item })),
    ...(params.payload.genericTools ?? []).map((item) => ({ kind: 'generic' as const, item })),
    ...(params.payload.subAgents ?? []).map((item) => ({ kind: 'agent' as const, item })),
    ...(params.payload.todoWrites ?? []).map((item) => ({ kind: 'todo' as const, item })),
    ...(params.thinkingRows ?? []).map((item) => ({ kind: 'thinking' as const, item })),
    ...(params.copInlineTextRows ?? []).map((item) => ({ kind: 'inline' as const, item })),
  ].sort((left, right) => itemSeq(left.item) - itemSeq(right.item) || left.kind.localeCompare(right.kind)) : []

  const entries: PromotedCopTimelineEntry[] = []
  let bucketIndex = 0
  let sliceIndex = 0
  const flushUntil = (maxSeq: number) => {
    const sliceBuckets: BodyBucket[] = []
    while (bucketIndex < bodyBuckets.length && itemSeq(bodyBuckets[bucketIndex]!.item) < maxSeq) {
      sliceBuckets.push(bodyBuckets[bucketIndex]!)
      bucketIndex += 1
    }
    if (sliceBuckets.length === 0) return
    const slice = makeBodySlice(`timeline-${sliceIndex}`, sliceBuckets, params.payload.sources)
    sliceIndex += 1
    entries.push({ kind: 'timeline', id: slice.id, seq: slice.seq, slice })
  }

  for (const barrier of barriers) {
    flushUntil(barrier.seq)
    const attachedBuckets: BodyBucket[] = []
    while (bucketIndex < bodyBuckets.length && bodyBuckets[bucketIndex]!.kind === 'thinking') {
      attachedBuckets.push(bodyBuckets[bucketIndex]!)
      bucketIndex += 1
    }
    const attachedSlice = attachedBuckets.length > 0
      ? makeBodySlice(`${barrier.kind}-${barrier.id}-attached`, attachedBuckets, params.payload.sources)
      : undefined
    entries.push(attachedSlice ? { ...barrier, attachedSlice } : barrier)
  }
  flushUntil(Number.POSITIVE_INFINITY)

  return entries.sort((left, right) => left.seq - right.seq || left.kind.localeCompare(right.kind) || left.id.localeCompare(right.id))
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
    for (const group of payload.exploreGroups ?? []) {
      for (const item of group.items) ids.add(item.id)
    }
    for (const w of payload.webFetches ?? []) ids.add(w.id)
    for (const a of payload.subAgents ?? []) ids.add(a.id)
    for (const g of payload.genericTools ?? []) ids.add(g.id)
    for (const t of payload.todoWrites ?? []) ids.add(t.id)
  }
  return ids
}
