import type { FileOpRef } from './storage'
import { planDisplayNameFromArgs } from './planMetadata'
import { contentText, renderTimelineText, type TimelineText } from './timelineText'

export type ToolDisplayKind =
  | 'explore'
  | 'read'
  | 'grep'
  | 'glob'
  | 'lsp'
  | 'command'
  | 'edit'
  | 'agent'
  | 'memory'
  | 'generic'

export type ToolPresentation = {
  kind: ToolDisplayKind
  text: TimelineText
  description: string
  subject?: string
  detail?: string
  stats?: Record<string, unknown>
}

export type ExploreGroupRef = {
  id: string
  label: string
  text?: TimelineText
  status: 'running' | 'success' | 'failed'
  items: FileOpRef[]
  seq?: number
}

export const EXPLORE_TOOL_NAMES = new Set(['read_file', 'grep', 'glob', 'load_tools', 'load_skill', 'lsp'])
export const LOAD_TOOL_NAMES = new Set(['load_tools', 'load_skill'])
export const LSP_MUTATING_OPERATIONS = new Set(['rename'])

export function basename(path: string): string {
  const normalized = path.replace(/\\/g, '/')
  return normalized.split('/').filter(Boolean).pop() ?? path
}

// 把命令行中疑似路径的 token 简化为 basename，避免长绝对路径塞满 UI
export function compactCommandLine(line: string): string {
  if (!line) return line
  return line.split(' ').map((token) => {
    if (!token) return token
    if (token.startsWith('-')) return token
    if (token.includes('://')) return token
    if (!token.includes('/')) return token
    const segments = token.split('/').filter(Boolean)
    if (segments.length < 3) return token
    return basename(token)
  }).join(' ')
}

export function truncate(value: string, max: number): string {
  return value.length > max ? `${value.slice(0, max)}…` : value
}

export function stringArg(args: Record<string, unknown>, key: string): string {
  const value = args[key]
  return typeof value === 'string' ? value : ''
}

function readPath(args: Record<string, unknown>): string {
  const direct = stringArg(args, 'file_path')
  if (direct) return direct
  const source = args.source
  if (!source || typeof source !== 'object' || Array.isArray(source)) return ''
  const nested = (source as { file_path?: unknown }).file_path
  return typeof nested === 'string' ? nested : ''
}

function displayFromArgs(args: Record<string, unknown>): Partial<ToolPresentation> | null {
  const raw = args.display
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null
  const display = raw as Record<string, unknown>
  const description = typeof display.description === 'string' ? display.description.trim() : ''
  const kind = typeof display.kind === 'string' ? display.kind.trim() as ToolDisplayKind : undefined
  const subject = typeof display.subject === 'string' ? display.subject.trim() : undefined
  const detail = typeof display.detail === 'string' ? display.detail.trim() : undefined
  const stats = display.stats && typeof display.stats === 'object' && !Array.isArray(display.stats)
    ? display.stats as Record<string, unknown>
    : undefined
  return { ...(kind ? { kind } : {}), ...(description ? { description } : {}), ...(subject ? { subject } : {}), ...(detail ? { detail } : {}), ...(stats ? { stats } : {}) }
}

function lspDescription(args: Record<string, unknown>): string {
  const operation = stringArg(args, 'operation')
  const query = stringArg(args, 'query')
  const filePath = stringArg(args, 'file_path')
  const subject = query || (filePath ? basename(filePath) : '')
  switch (operation) {
    case 'definition': return subject ? `Found definition in ${subject}` : 'Found definition'
    case 'references': return subject ? `Found references in ${subject}` : 'Found references'
    case 'hover': return subject ? `Inspected symbol in ${subject}` : 'Inspected symbol'
    case 'document_symbols': return subject ? `Listed symbols in ${subject}` : 'Listed document symbols'
    case 'workspace_symbols': return query ? `Searched symbols for ${truncate(query, 36)}` : 'Searched symbols'
    case 'type_definition': return subject ? `Found type definition in ${subject}` : 'Found type definition'
    case 'implementation': return subject ? `Found implementations in ${subject}` : 'Found implementations'
    case 'diagnostics': return subject ? `Checked diagnostics in ${subject}` : 'Checked diagnostics'
    case 'prepare_call_hierarchy': return 'Prepared call hierarchy'
    case 'incoming_calls': return 'Found incoming calls'
    case 'outgoing_calls': return 'Found outgoing calls'
    case 'rename': return subject ? `Renamed symbol in ${subject}` : 'Renamed symbol'
    default: return operation ? `Ran LSP ${operation}` : 'Ran LSP'
  }
}

export function normalizeToolName(toolName: string): string {
  if (toolName === 'read' || toolName.startsWith('read.')) return 'read_file'
  return toolName
}

export function presentationForTool(toolNameInput: string, args: Record<string, unknown> = {}, label?: string): ToolPresentation {
  const toolName = normalizeToolName(toolNameInput)
  const explicit = displayFromArgs(args)
  const fallback = label?.trim()
  let kind: ToolDisplayKind = 'generic'
  let text: TimelineText = fallback ? contentText(fallback) : contentText(toolName)
  let subject: string | undefined
  let detail: string | undefined

  switch (toolName) {
    case 'read_file': {
      kind = 'read'
      const path = readPath(args)
      subject = path ? basename(path) : undefined
      detail = path || undefined
      text = subject ? { kind: 'tool_subject', action: 'read', subject: truncate(subject, 48) } : { kind: 'read_file' }
      break
    }
    case 'grep': {
      kind = 'grep'
      const pattern = stringArg(args, 'pattern')
      const path = stringArg(args, 'path')
      subject = pattern ? truncate(pattern, 48) : undefined
      detail = path || undefined
      text = pattern ? { kind: 'tool_subject', action: 'searched', subject: truncate(pattern, 48) } : { kind: 'searched_code' }
      break
    }
    case 'glob': {
      kind = 'glob'
      const pattern = stringArg(args, 'pattern')
      const path = stringArg(args, 'path')
      subject = pattern ? truncate(pattern, 48) : undefined
      detail = path || undefined
      text = pattern ? { kind: 'tool_subject', action: 'listed', subject: truncate(pattern, 48) } : { kind: 'listed_files' }
      break
    }
    case 'lsp': {
      const operation = stringArg(args, 'operation')
      kind = LSP_MUTATING_OPERATIONS.has(operation) ? 'edit' : 'lsp'
      subject = operation || undefined
      detail = stringArg(args, 'file_path') || stringArg(args, 'query') || undefined
      text = contentText(lspDescription(args))
      break
    }
    case 'edit':
    case 'edit_file':
    case 'write_file': {
      kind = 'edit'
      const path = stringArg(args, 'file_path')
      subject = planDisplayNameFromArgs(args) ?? (path ? basename(path) : undefined)
      detail = path || undefined
      text = subject
        ? { kind: 'tool_subject', action: toolName === 'write_file' ? 'wrote' : 'edited', subject: truncate(subject, 48) }
        : { kind: toolName === 'write_file' ? 'wrote_file' : 'edited_file' }
      break
    }
    case 'exec_command':
    case 'continue_process':
    case 'terminate_process':
    case 'python_execute': {
      kind = 'command'
      const command = stringArg(args, 'cmd') || stringArg(args, 'command') || stringArg(args, 'code')
      detail = command || undefined
      subject = command ? command.split(/\s+/)[0] : undefined
      const firstLine = command ? command.split('\n')[0]!.trim() : ''
      text = fallback
        ? contentText(fallback)
        : firstLine
          ? contentText(truncate(compactCommandLine(firstLine), 72))
          : { kind: 'run_command' }
      break
    }
    case 'load_tools':
    case 'load_skill': {
      kind = 'explore'
      text = { kind: 'loaded_resources', tense: 'done', tools: toolName === 'load_skill' ? 0 : 1, skills: toolName === 'load_skill' ? 1 : 0 }
      break
    }
    case 'enter_plan_mode': {
      text = { kind: 'plan_mode', action: 'enter' }
      break
    }
    case 'exit_plan_mode': {
      text = { kind: 'plan_mode', action: 'exit' }
      break
    }
    default:
      break
  }

  return {
    kind: explicit?.kind ?? kind,
    text: explicit?.description ? contentText(explicit.description) : text,
    description: explicit?.description ?? renderTimelineText(text, 'en'),
    ...(explicit?.subject ?? subject ? { subject: explicit?.subject ?? subject } : {}),
    ...(explicit?.detail ?? detail ? { detail: explicit?.detail ?? detail } : {}),
    ...(explicit?.stats ? { stats: explicit.stats } : {}),
  }
}

export function isExploreFileOp(op: FileOpRef): boolean {
  const toolName = normalizeToolName(op.toolName)
  if (!EXPLORE_TOOL_NAMES.has(toolName)) return false
  if (toolName !== 'lsp') return true
  return op.operation !== 'rename'
}

export function exploreGroupLabel(items: FileOpRef[], status: ExploreGroupRef['status']): string {
  return renderTimelineText(exploreGroupText(items, status), 'en')
}

export function exploreGroupText(items: FileOpRef[], status: ExploreGroupRef['status']): TimelineText {
  const onlyLoadTools = items.length > 0 && items.every((item) => LOAD_TOOL_NAMES.has(normalizeToolName(item.toolName)))
  const loadToolsCount = items.filter((item) => normalizeToolName(item.toolName) === 'load_tools').length
  const loadSkillCount = items.filter((item) => normalizeToolName(item.toolName) === 'load_skill').length

  if (status === 'running') {
    return onlyLoadTools
      ? { kind: 'loaded_resources', tense: 'live', tools: loadToolsCount, skills: loadSkillCount }
      : { kind: 'exploring_code' }
  }

  if (onlyLoadTools) return { kind: 'loaded_resources', tense: 'done', tools: loadToolsCount, skills: loadSkillCount }

  const reads = new Set(items.filter((item) => normalizeToolName(item.toolName) === 'read_file').map((item) => item.filePath || item.label))
  const hasSearch = items.some((item) => ['grep', 'lsp'].includes(normalizeToolName(item.toolName)))
  const hasGlob = items.some((item) => normalizeToolName(item.toolName) === 'glob')
  const parts: TimelineText[] = []
  if (hasSearch) parts.push({ kind: 'searched_code' })
  if (hasGlob) parts.push({ kind: 'listed_files' })
  if (reads.size > 0) parts.push({ kind: 'read_files', count: reads.size })
  return parts.length > 0 ? { kind: 'join', parts, separator: ', ' } : { kind: 'explored_code' }
}
