import { MarkdownRenderer } from '../MarkdownRenderer'
import type { ArtifactRef } from '../../storage'

type Props = {
  content: string
  accessToken?: string
  artifacts?: ArtifactRef[]
  runId?: string
}

type PlanTodo = {
  id: string
  content: string
  status: string
}

type PlanFrontMatter = {
  name?: string
  overview?: string
  todos: PlanTodo[]
  body: string
}

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

function parsePlanFrontMatter(content: string): PlanFrontMatter | null {
  const match = /^---\r?\n([\s\S]*?)\r?\n---[ \t]*(?:\r?\n|$)([\s\S]*)$/.exec(content)
  if (!match) return null
  const header = match[1] ?? ''
  const lines = header.split(/\r?\n/)
  const plan: PlanFrontMatter = { todos: [], body: match[2] ?? '' }
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

function renderPlanFrontMatterAsMarkdown(content: string): string {
  const plan = parsePlanFrontMatter(content)
  if (!plan) return content

  const lines: string[] = []
  if (plan.name) lines.push(`# ${plan.name}`)
  if (plan.overview) {
    if (lines.length > 0) lines.push('')
    lines.push(plan.overview)
  }
  const todos = plan.todos.filter((todo) => todo.content.trim() !== '')
  if (todos.length > 0) {
    if (lines.length > 0) lines.push('')
    lines.push('## Todos')
    for (const todo of todos) {
      const checked = todo.status === 'completed' ? 'x' : ' '
      const suffix = todo.status && todo.status !== 'pending' && todo.status !== 'completed'
        ? ` (${todo.status})`
        : ''
      lines.push(`- [${checked}] ${todo.content}${suffix}`)
    }
  }

  const body = plan.body.trim()
  if (body) {
    if (lines.length > 0) lines.push('')
    lines.push(body)
  }
  return lines.join('\n')
}

export function MarkdownDocumentRenderer({ content, accessToken = '', artifacts = [], runId }: Props) {
  const renderContent = renderPlanFrontMatterAsMarkdown(content)
  return (
    <div data-preview-renderer="markdown" style={{ padding: '20px 28px' }}>
      <MarkdownRenderer
        content={renderContent}
        artifacts={artifacts}
        accessToken={accessToken}
        runId={runId}
        compact
        allowHtml
      />
    </div>
  )
}
