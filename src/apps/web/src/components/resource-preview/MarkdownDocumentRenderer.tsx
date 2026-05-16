import { useMemo } from 'react'
import { MarkdownRenderer } from '../MarkdownRenderer'
import { TodoListCard } from '../TodoListCard'
import type { ArtifactRef } from '../../storage'
import { isPlanTodoCompleted, parsePlanMarkdown, type PlanMetadata, type PlanTodo } from '../../planMetadata'
import type { TodoItemRef, TodoWriteRef } from '../../copSegmentTimeline'

type Props = {
  content: string
  accessToken?: string
  artifacts?: ArtifactRef[]
  runId?: string
}

export function MarkdownDocumentRenderer({ content, accessToken = '', artifacts = [], runId }: Props) {
  const plan = useMemo(() => parsePlanMarkdown(content), [content])
  if (plan) {
    return (
      <PlanDocumentRenderer
        plan={plan}
        accessToken={accessToken}
        artifacts={artifacts}
        runId={runId}
      />
    )
  }

  return (
    <div data-preview-renderer="markdown" style={{ padding: '20px 28px' }}>
      <MarkdownRenderer
        content={content}
        artifacts={artifacts}
        accessToken={accessToken}
        runId={runId}
        compact
        allowHtml
      />
    </div>
  )
}

function planTodoStatus(status: string | undefined): TodoItemRef['status'] {
  if (isPlanTodoCompleted(status)) return 'completed'
  if (status === 'in_progress' || status === 'cancelled') return status
  return 'pending'
}

function planTodosAsTodoWrite(todos: PlanTodo[]): TodoWriteRef | null {
  const items = todos
    .filter((todo) => todo.content.trim() !== '')
    .map((todo, index) => ({
      id: todo.id || `plan-todo-${index + 1}`,
      content: todo.content,
      status: planTodoStatus(todo.status),
    }))
  if (items.length === 0) return null

  return {
    id: 'plan-todos',
    toolName: 'todo_write',
    todos: items,
    completedCount: items.filter((item) => item.status === 'completed').length,
    totalCount: items.length,
    status: 'success',
  }
}

function PlanTodoList({ todos }: { todos: PlanTodo[] }) {
  const todoWrite = planTodosAsTodoWrite(todos)
  if (!todoWrite) return null

  return (
    <section style={{ marginTop: 44, paddingTop: 20, borderTop: '0.5px solid var(--c-border-subtle)' }}>
      <TodoListCard todo={todoWrite} />
    </section>
  )
}

function PlanDocumentRenderer({
  plan,
  accessToken,
  artifacts,
  runId,
}: {
  plan: PlanMetadata
  accessToken: string
  artifacts: ArtifactRef[]
  runId?: string
}) {
  const body = plan.body.trim()

  return (
    <article
      data-preview-renderer="plan"
      style={{
        maxWidth: 840,
        margin: '0 auto',
        padding: '54px 56px 64px',
      }}
    >
      {plan.name ? (
        <h1 style={{ margin: '0 0 12px', color: 'var(--c-text-primary)', fontSize: 30, lineHeight: '38px', fontWeight: 'var(--c-fw-500)' }}>
          {plan.name}
        </h1>
      ) : null}
      {plan.overview ? (
        <p style={{ margin: '0 0 30px', color: 'var(--c-text-secondary)', fontSize: 16, lineHeight: '26px' }}>
          {plan.overview}
        </p>
      ) : null}
      {body ? (
        <div style={{ marginTop: plan.name || plan.overview ? 8 : 0 }}>
          <MarkdownRenderer
            content={body}
            artifacts={artifacts}
            accessToken={accessToken}
            runId={runId}
            compact
            allowHtml
          />
        </div>
      ) : null}
      <PlanTodoList todos={plan.todos} />
    </article>
  )
}
