import { useState } from 'react'
import { ChevronDown, ChevronRight, Circle, CircleCheck, CircleDotDashed, CircleX, ListTodo } from 'lucide-react'
import type { TodoChangeRef, TodoItemRef, TodoWriteRef } from '../copSegmentTimeline'
import { useLocale } from '../contexts/LocaleContext'
import type { LocaleStrings } from '../locales'

type Props = {
  todo: TodoWriteRef
}

function statusIcon(status: TodoItemRef['status']) {
  switch (status) {
    case 'completed':
      return <CircleCheck size={15} />
    case 'in_progress':
      return <CircleDotDashed size={15} />
    case 'cancelled':
      return <CircleX size={15} />
    case 'pending':
      return <Circle size={15} />
  }
}

function statusColor(status: TodoItemRef['status']): string {
  switch (status) {
    case 'completed':
      return 'var(--c-status-success-text)'
    case 'in_progress':
      return 'var(--c-text-primary)'
    case 'cancelled':
      return 'var(--c-status-error-text)'
    case 'pending':
      return 'var(--c-text-muted)'
  }
}

type TodoChangeSummary = {
  label: string
  content: string
  status: TodoItemRef['status']
}

function todoContentForChange(todo: TodoWriteRef, change: TodoChangeRef): string {
  const item = todo.todos.find((entry) => entry.id === change.id)
  if (change.status === 'in_progress') {
    return change.activeForm || item?.activeForm || change.content || item?.content || ''
  }
  return change.content || item?.content || ''
}

function todoChangePosition(todo: TodoWriteRef, change: TodoChangeRef): number {
  const total = todo.totalCount ?? todo.todos.length
  if (change.status === 'completed') {
    const completed = todo.completedCount ?? todo.todos.filter((item) => item.status === 'completed').length
    return Math.min(Math.max(completed, 1), Math.max(total, 1))
  }
  if (typeof change.index === 'number') {
    return Math.min(Math.max(change.index + 1, 1), Math.max(total, 1))
  }
  const itemIndex = todo.todos.findIndex((item) => item.id === change.id)
  return itemIndex >= 0 ? itemIndex + 1 : 1
}

function todoChangeLabel(t: LocaleStrings, status: TodoItemRef['status'], position: number, total: number): string {
  switch (status) {
    case 'completed':
      return t.todoChangeCompleted(position, total)
    case 'in_progress':
      return t.todoChangeStarted(position, total)
    case 'cancelled':
      return t.todoChangeCancelled(position, total)
    case 'pending':
      return t.todoChangeUpdated(position, total)
  }
}

function rankChangeStatus(status: TodoItemRef['status']): number {
  switch (status) {
    case 'completed':
      return 0
    case 'in_progress':
      return 1
    case 'cancelled':
      return 2
    case 'pending':
      return 3
  }
}

function singleTodoChangeSummary(todo: TodoWriteRef, t: LocaleStrings): TodoChangeSummary | null {
  if (todo.status === 'failed') return null
  const changes = todo.changes ?? []
  const statusChanges = changes
    .filter((change) => (
      change.type === 'updated' &&
      !!change.status &&
      !!change.previousStatus &&
      change.previousStatus !== change.status
    ))
    .sort((left, right) => rankChangeStatus(left.status!) - rankChangeStatus(right.status!))
  const change = statusChanges[0]
  if (!change?.status) return null

  const total = todo.totalCount ?? todo.todos.length
  if (total <= 0) return null
  const content = todoContentForChange(todo, change)
  if (!content) return null
  const position = todoChangePosition(todo, change)

  return {
    label: todoChangeLabel(t, change.status, position, total),
    content,
    status: change.status,
  }
}

function TodoItemsList({ todo }: { todo: TodoWriteRef }) {
  const failed = todo.status === 'failed'

  return (
    <div style={{ padding: '4px 8px' }}>
      {todo.todos.map((item, index) => {
        const muted = item.status === 'completed' || item.status === 'cancelled'
        return (
          <div
            key={item.id}
            className="todo-list-item-rise"
            style={{
              display: 'flex',
              alignItems: 'flex-start',
              gap: 8,
              padding: '5px 2px',
              color: muted ? 'var(--c-text-muted)' : 'var(--c-text-primary)',
              animationDelay: `${index * 18}ms`,
            }}
          >
            <span style={{ display: 'inline-flex', paddingTop: 2, color: statusColor(item.status), flexShrink: 0 }}>
              {statusIcon(item.status)}
            </span>
            <span
              style={{
                minWidth: 0,
                flex: 1,
                overflowWrap: 'anywhere',
                fontSize: 'var(--c-cop-row-font-size)',
                lineHeight: 'var(--c-cop-row-line-height)',
                textDecoration: muted ? 'line-through' : 'none',
                textDecorationColor: 'var(--c-text-muted)',
              }}
            >
              {item.status === 'in_progress' && item.activeForm ? item.activeForm : item.content}
            </span>
          </div>
        )
      })}
      {failed && todo.errorMessage && (
        <div
          style={{
            padding: '5px 2px',
            color: 'var(--c-status-error-text)',
            fontSize: 12,
            lineHeight: '18px',
          }}
        >
          {todo.errorMessage}
        </div>
      )}
    </div>
  )
}

function TodoChangeSummaryRow({ summary, todo }: { summary: TodoChangeSummary; todo: TodoWriteRef }) {
  const [expanded, setExpanded] = useState(false)
  return (
    <div className="todo-list-root" style={{ maxWidth: 'min(100%, 760px)', padding: '4px 0' }}>
      <button
        type="button"
        className="todo-summary-trigger"
        aria-expanded={expanded}
        data-testid="todo-change-summary"
        onClick={() => setExpanded((value) => !value)}
        style={{
          maxWidth: '100%',
          minWidth: 0,
          padding: '4px 0 2px',
          border: 'none',
          background: 'transparent',
          display: 'flex',
          alignItems: 'center',
          flexWrap: 'wrap',
          gap: 6,
          color: 'var(--c-cop-row-fg, var(--c-text-tertiary))',
          fontFamily: 'inherit',
          fontSize: 'var(--c-cop-row-font-size, 14px)',
          fontWeight: 400,
          lineHeight: 'var(--c-cop-row-line-height, 20px)',
          cursor: 'pointer',
          transition: 'color 0.15s ease',
        }}
      >
        <span style={{ whiteSpace: 'nowrap' }}>
          {summary.label}
        </span>
        <span style={{ display: 'inline-flex', color: 'currentColor', flexShrink: 0 }}>
          {statusIcon(summary.status)}
        </span>
        <span style={{ minWidth: 0, overflowWrap: 'anywhere', textAlign: 'left' }}>
          {summary.content}
        </span>
        {expanded
          ? <ChevronDown size={13} style={{ flexShrink: 0, color: 'currentColor' }} />
          : <ChevronRight size={13} style={{ flexShrink: 0, color: 'currentColor' }} />
        }
      </button>
      <div
        className="todo-summary-expand"
        data-testid="todo-summary-expand"
        aria-hidden={!expanded}
        style={{
          gridTemplateRows: expanded ? '1fr' : '0fr',
          opacity: expanded ? 1 : 0,
        }}
      >
        <div style={{ minHeight: 0, overflow: 'hidden' }}>
          <div
            style={{
              marginTop: 6,
              borderRadius: 8,
              background: 'var(--c-attachment-bg)',
              border: '0.5px solid var(--c-border-subtle)',
              overflow: 'hidden',
            }}
          >
            <TodoItemsList todo={todo} />
          </div>
        </div>
      </div>
    </div>
  )
}

export function TodoListCard({ todo }: Props) {
  const { t } = useLocale()
  const summary = singleTodoChangeSummary(todo, t)
  const [expanded, setExpanded] = useState(true)
  const completed = todo.completedCount ?? todo.todos.filter((item) => item.status === 'completed').length
  const total = todo.totalCount ?? todo.todos.length
  const failed = todo.status === 'failed'

  if (summary) {
    return <TodoChangeSummaryRow summary={summary} todo={todo} />
  }

  return (
    <div className="todo-list-root" style={{ maxWidth: 'min(100%, 760px)', padding: '4px 0' }}>
      <div
        style={{
          borderRadius: 8,
          background: 'var(--c-attachment-bg)',
          border: '0.5px solid var(--c-border-subtle)',
          overflow: 'hidden',
        }}
      >
        <button
          type="button"
          aria-expanded={expanded}
          onClick={() => setExpanded((value) => !value)}
          style={{
            width: '100%',
            minWidth: 0,
            border: 'none',
            background: 'transparent',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 10,
            padding: '8px 10px',
            color: 'var(--c-text-secondary)',
            fontSize: 'var(--c-cop-row-font-size)',
            lineHeight: 'var(--c-cop-row-line-height)',
          }}
        >
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 7, minWidth: 0 }}>
            <ListTodo size={15} style={{ flexShrink: 0, color: failed ? 'var(--c-status-error-text)' : 'var(--c-text-muted)' }} />
            <span style={{ color: 'var(--c-text-primary)', fontWeight: 460 }}>{t.todoListTitle}</span>
            {total > 0 && (
              <span style={{ color: failed ? 'var(--c-status-error-text)' : 'var(--c-text-muted)', whiteSpace: 'nowrap' }}>
                {t.todoListProgress(completed, total)}
              </span>
            )}
          </span>
          {expanded
            ? <ChevronDown size={14} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
            : <ChevronRight size={14} style={{ flexShrink: 0, color: 'var(--c-text-muted)' }} />
          }
        </button>
        <div
          style={{
            display: 'grid',
            gridTemplateRows: expanded ? '1fr' : '0fr',
            transition: 'grid-template-rows 0.24s cubic-bezier(0.4, 0, 0.2, 1)',
          }}
        >
          <div style={{ minHeight: 0, overflow: 'hidden' }}>
            <TodoItemsList todo={todo} />
          </div>
        </div>
      </div>
    </div>
  )
}
