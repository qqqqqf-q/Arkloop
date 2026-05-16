const PLAN_FRONT_MATTER_RE = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/
const PLAN_NAME_RE = /^name:\s*(.+?)\s*$/m

export type PlanTodo = {
  id: string
  content: string
  status: string
}

export type PlanMetadata = {
  name?: string
  overview?: string
  todos: PlanTodo[]
  body: string
}

export type PlanBuildState = 'ready' | 'building' | 'built'

export const PLAN_TODOS_UPDATED_EVENT = 'arkloop:plan-todos-updated'

function unquoteYamlScalar(value: string): string {
  const trimmed = value.trim()
  if (trimmed.length >= 2) {
    const first = trimmed[0]
    const last = trimmed[trimmed.length - 1]
    if ((first === '"' && last === '"') || (first === "'" && last === "'")) {
      return trimmed.slice(1, -1)
    }
  }
  return trimmed
}

export function isPlanMarkdownPath(path: string | undefined): boolean {
  if (!path) return false
  return path.replace(/\\/g, '/').split('/').pop()?.toLowerCase().endsWith('.plan.md') ?? false
}

export function parsePlanMarkdown(content: string | undefined): PlanMetadata | null {
  if (!content) return null
  const match = /^---\r?\n([\s\S]*?)\r?\n---[ \t]*(?:\r?\n|$)([\s\S]*)$/.exec(content)
  if (!match) return null
  const header = match[1] ?? ''
  const lines = header.split(/\r?\n/)
  const plan: PlanMetadata = { todos: [], body: match[2] ?? '' }
  let currentTodo: PlanTodo | null = null
  let inTodos = false

  for (const line of lines) {
    const topLevel = /^([A-Za-z][A-Za-z0-9_-]*):\s*(.*)$/.exec(line)
    if (topLevel) {
      const [, key, rawValue] = topLevel
      inTodos = key === 'todos'
      if (key === 'name') plan.name = unquoteYamlScalar(rawValue ?? '')
      if (key === 'overview') plan.overview = unquoteYamlScalar(rawValue ?? '')
      continue
    }

    if (!inTodos) continue
    const todoStart = /^\s*-\s+id:\s*(.*)$/.exec(line)
    if (todoStart) {
      currentTodo = { id: unquoteYamlScalar(todoStart[1] ?? ''), content: '', status: 'pending' }
      plan.todos.push(currentTodo)
      continue
    }
    const todoField = /^\s+([A-Za-z][A-Za-z0-9_-]*):\s*(.*)$/.exec(line)
    if (!currentTodo || !todoField) continue
    const [, key, rawValue] = todoField
    if (key === 'content') currentTodo.content = unquoteYamlScalar(rawValue ?? '')
    if (key === 'status') currentTodo.status = unquoteYamlScalar(rawValue ?? '')
  }

  if (!plan.name && !plan.overview && plan.todos.length === 0) return null
  return plan
}

export function isPlanMarkdown(path: string | undefined, content: string | undefined): boolean {
  return isPlanMarkdownPath(path) && parsePlanMarkdown(content) !== null
}

export function isPlanTodoCompleted(status: string | undefined): boolean {
  const normalized = status?.trim().toLowerCase()
  return normalized === 'completed' || normalized === 'complete' || normalized === 'done'
}

export function resolvePlanBuildState(plan: PlanMetadata, buildRequested: boolean): PlanBuildState {
  const todos = plan.todos.filter((todo) => todo.content.trim() !== '')
  if (todos.length > 0 && todos.every((todo) => isPlanTodoCompleted(todo.status))) return 'built'
  return buildRequested ? 'building' : 'ready'
}

export function extractPlanNameFromMarkdown(content: string | undefined): string | null {
  const plan = parsePlanMarkdown(content)
  if (plan?.name) return plan.name
  const raw = PLAN_NAME_RE.exec(PLAN_FRONT_MATTER_RE.exec(content ?? '')?.[1] ?? '')?.[1]?.trim()
  if (!raw) return null
  const unquoted = raw.replace(/^["']|["']$/g, '').trim()
  return unquoted || null
}

export function planDisplayNameFromArgs(args: Record<string, unknown>): string | null {
  const path = typeof args.file_path === 'string' ? args.file_path : ''
  if (!isPlanMarkdownPath(path)) return null
  const content = typeof args.content === 'string' ? args.content : ''
  return extractPlanNameFromMarkdown(content)
}

export function planDisplayNameFromResult(result: unknown): string | null {
  if (!result || typeof result !== 'object' || Array.isArray(result)) return null
  const record = result as Record<string, unknown>
  const plan = typeof record.plan === 'string' ? record.plan : ''
  return extractPlanNameFromMarkdown(plan)
}
