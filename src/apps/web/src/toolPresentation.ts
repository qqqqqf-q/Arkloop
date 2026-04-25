import type { FileOpRef } from './storage'

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
  description: string
  subject?: string
  detail?: string
  stats?: Record<string, unknown>
}

export type ExploreGroupRef = {
  id: string
  label: string
  status: 'running' | 'success' | 'failed'
  items: FileOpRef[]
  seq?: number
}

const EXPLORE_TOOL_NAMES = new Set(['read_file', 'grep', 'glob', 'load_tools', 'load_skill', 'lsp'])
const MUTATING_LSP_OPERATIONS = new Set(['rename'])

function basename(path: string): string {
  const normalized = path.replace(/\\/g, '/')
  return normalized.split('/').filter(Boolean).pop() ?? path
}

function truncate(value: string, max: number): string {
  return value.length > max ? `${value.slice(0, max)}…` : value
}

function stringArg(args: Record<string, unknown>, key: string): string {
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
  let description = fallback || toolName
  let subject: string | undefined
  let detail: string | undefined

  switch (toolName) {
    case 'read_file': {
      kind = 'read'
      const path = readPath(args)
      subject = path ? basename(path) : undefined
      detail = path || undefined
      description = subject ? `Read ${truncate(subject, 48)}` : 'Read file'
      break
    }
    case 'grep': {
      kind = 'grep'
      const pattern = stringArg(args, 'pattern')
      const path = stringArg(args, 'path')
      subject = pattern ? truncate(pattern, 48) : undefined
      detail = path || undefined
      description = pattern ? `Searched ${truncate(pattern, 48)}` : 'Searched code'
      break
    }
    case 'glob': {
      kind = 'glob'
      const pattern = stringArg(args, 'pattern')
      const path = stringArg(args, 'path')
      subject = pattern ? truncate(pattern, 48) : undefined
      detail = path || undefined
      description = pattern ? `Listed ${truncate(pattern, 48)}` : 'Listed files'
      break
    }
    case 'lsp': {
      const operation = stringArg(args, 'operation')
      kind = MUTATING_LSP_OPERATIONS.has(operation) ? 'edit' : 'lsp'
      subject = operation || undefined
      detail = stringArg(args, 'file_path') || stringArg(args, 'query') || undefined
      description = lspDescription(args)
      break
    }
    case 'edit':
    case 'edit_file':
    case 'write_file': {
      kind = 'edit'
      const path = stringArg(args, 'file_path')
      subject = path ? basename(path) : undefined
      detail = path || undefined
      description = subject ? `${toolName === 'write_file' ? 'Wrote' : 'Edited'} ${truncate(subject, 48)}` : (toolName === 'write_file' ? 'Wrote file' : 'Edited file')
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
      description = fallback || (command ? truncate(command.split('\n')[0].trim(), 72) : 'Run command')
      break
    }
    case 'load_tools':
    case 'load_skill': {
      kind = 'explore'
      description = toolName === 'load_skill' ? 'Loaded skill' : 'Loaded tools'
      break
    }
    default:
      break
  }

  return {
    kind: explicit?.kind ?? kind,
    description: explicit?.description ?? description,
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
  if (status === 'running') return 'Exploring code'
  const reads = new Set(items.filter((item) => normalizeToolName(item.toolName) === 'read_file').map((item) => item.filePath || item.label))
  const hasSearch = items.some((item) => ['grep', 'lsp'].includes(normalizeToolName(item.toolName)))
  const hasGlob = items.some((item) => normalizeToolName(item.toolName) === 'glob')
  const parts: string[] = []
  if (hasSearch) parts.push('Searched code')
  if (hasGlob) parts.push('listed files')
  if (reads.size > 0) parts.push(`read ${reads.size === 1 ? 'a file' : `${reads.size} files`}`)
  return parts.length > 0 ? parts.join(', ') : 'Explored code'
}
