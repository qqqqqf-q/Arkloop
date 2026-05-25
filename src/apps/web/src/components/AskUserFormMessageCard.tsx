import { useCallback, useState } from 'react'
import { ChevronDown, ChevronUp } from 'lucide-react'
import type { AgentAskUserFormContent } from '../agent-ui'
import type { FieldValue } from '../userInputTypes'
import UserInputCard from './UserInputCard'

interface Props {
  content: AgentAskUserFormContent
  activeRunId: string | null
  onSubmit: (requestId: string, answers: Record<string, FieldValue>) => Promise<void>
  onDismiss: (requestId: string) => Promise<void>
}

function formatFieldValue(value: unknown): string {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'boolean') return value ? 'Yes' : 'No'
  if (Array.isArray(value)) return value.map(String).join(', ')
  return String(value)
}

function SubmittedAnswersView({ content }: { content: AgentAskUserFormContent }) {
  const [expanded, setExpanded] = useState(false)
  const answers = content.answers ?? {}
  const keys = content.schema._fieldOrder ?? Object.keys(answers)
  const statusLabel = content.status === 'submitted' ? 'Submitted' : content.status === 'dismissed' ? 'Dismissed' : 'Expired'
  const statusColor = content.status === 'submitted' ? 'var(--c-status-success)' : 'var(--c-text-muted)'

  return (
    <div
      className="flex flex-col w-full"
      style={{
        background: 'var(--c-bg-input)',
        borderWidth: '0.5px',
        borderStyle: 'solid',
        borderColor: 'var(--c-border-subtle)',
        borderRadius: '20px',
        padding: '18px 22px 16px',
        opacity: 0.85,
      }}
    >
      <div className="flex items-center justify-between gap-3 mb-2">
        <h2 className="text-[15px] font-normal leading-snug m-0 flex-1" style={{ color: 'var(--c-text-secondary)' }}>
          {content.message}
        </h2>
        <span className="text-[12px] font-medium flex-shrink-0 px-2 py-0.5 rounded-md" style={{ color: statusColor, background: 'var(--c-bg-deep)' }}>
          {statusLabel}
        </span>
      </div>

      {keys.length > 0 && (
        <button
          type="button"
          onClick={() => setExpanded(!expanded)}
          className="flex items-center gap-1.5 text-[12px] font-medium border-none bg-transparent cursor-pointer mt-1 mb-2"
          style={{ color: 'var(--c-text-muted)' }}
        >
          {expanded ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
          {expanded ? 'Hide answers' : `Show ${keys.length} answer${keys.length > 1 ? 's' : ''}`}
        </button>
      )}

      {expanded && (
        <div className="flex flex-col gap-2 mt-1">
          {keys.map((key: string) => {
            const fieldSchema = content.schema.properties[key]
            const title = (fieldSchema && typeof fieldSchema === 'object' && 'title' in fieldSchema)
              ? (fieldSchema.title as string) ?? key
              : key
            const value = answers[key]
            return (
              <div key={key} className="flex items-start gap-3 px-2 py-1.5 rounded-lg" style={{ background: 'var(--c-bg-deep)' }}>
                <span className="text-[13px] font-medium flex-shrink-0" style={{ color: 'var(--c-text-secondary)', minWidth: '80px' }}>
                  {title}
                </span>
                <span className="text-[13px] font-light flex-1" style={{ color: 'var(--c-text-primary)' }}>
                  {formatFieldValue(value)}
                </span>
              </div>
            )
          })}
          {content.submittedAt && (
            <div className="text-[11px] mt-1" style={{ color: 'var(--c-text-muted)' }}>
              {new Date(content.submittedAt).toLocaleString()}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export default function AskUserFormMessageCard({ content, activeRunId, onSubmit, onDismiss }: Props) {
  const isPending = content.status === 'pending'
  const isEditable = isPending && activeRunId === content.runId

  const handleSubmit = useCallback(async (response: { type: 'user_input_response'; request_id: string; answers: Record<string, FieldValue> }) => {
    await onSubmit(response.request_id, response.answers)
  }, [onSubmit])

  const handleDismiss = useCallback(async () => {
    await onDismiss(content.requestId)
  }, [onDismiss, content.requestId])

  if (isEditable) {
    return (
      <UserInputCard
        request={{
          request_id: content.requestId,
          message: content.message,
          requestedSchema: {
            properties: content.schema.properties as Record<string, import('../userInputTypes').FieldSchema>,
            required: content.schema.required,
            _fieldOrder: content.schema._fieldOrder,
          },
        }}
        onSubmit={handleSubmit}
        onDismiss={handleDismiss}
        disabled={!activeRunId}
      />
    )
  }

  return <SubmittedAnswersView content={content} />
}
