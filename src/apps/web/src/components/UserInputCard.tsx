import { useCallback, useEffect, useMemo, useState } from 'react'
import { ChevronUp, ChevronDown } from 'lucide-react'
import { useLocale } from '../contexts/LocaleContext'
import type {
  UserInputAnswer,
  UserInputQuestion,
  UserInputRequest,
  UserInputResponse,
} from '../userInputTypes'

interface Props {
  request: UserInputRequest
  onSubmit: (response: UserInputResponse) => void
  onDismiss: () => void
  disabled?: boolean
}

export default function UserInputCard({ request, onSubmit, onDismiss, disabled }: Props) {
  const { t } = useLocale()
  const isMulti = request.questions.length > 1
  const [activeIdx, setActiveIdx] = useState(0)
  const [answers, setAnswers] = useState<Record<string, UserInputAnswer>>({})
  const [otherTexts, setOtherTexts] = useState<Record<string, string>>({})
  const [optionOrders, setOptionOrders] = useState<Record<string, string[]>>({})
  const [submitting, setSubmitting] = useState(false)
  const [hovered, setHovered] = useState(false)

  useEffect(() => {
    const initial: Record<string, UserInputAnswer> = {}
    const orders: Record<string, string[]> = {}
    for (const q of request.questions) {
      orders[q.id] = q.options.map((o) => o.value)
      const rec = q.options.find((o) => o.recommended)
      if (rec) {
        initial[q.id] = { type: 'option', value: rec.value }
      }
    }
    setAnswers(initial)
    setOptionOrders(orders)
    setActiveIdx(0)
  }, [request.questions])

  const selectOption = useCallback((questionId: string, value: string) => {
    setAnswers((prev) => ({ ...prev, [questionId]: { type: 'option', value } }))
  }, [])

  const selectOther = useCallback((questionId: string) => {
    setAnswers((prev) => ({
      ...prev,
      [questionId]: { type: 'other', value: otherTexts[questionId] ?? '' },
    }))
  }, [otherTexts])

  const updateOtherText = useCallback((questionId: string, text: string) => {
    setOtherTexts((prev) => ({ ...prev, [questionId]: text }))
    setAnswers((prev) => {
      if (prev[questionId]?.type === 'other') {
        return { ...prev, [questionId]: { type: 'other', value: text } }
      }
      return prev
    })
  }, [])

  const moveOption = useCallback((questionId: string, value: string, direction: -1 | 1) => {
    setOptionOrders((prev) => {
      const order = [...(prev[questionId] ?? [])]
      const idx = order.indexOf(value)
      if (idx < 0) return prev
      const target = idx + direction
      if (target < 0 || target >= order.length) return prev
      ;[order[idx], order[target]] = [order[target], order[idx]]
      return { ...prev, [questionId]: order }
    })
  }, [])

  const currentQ = request.questions[activeIdx]
  const isLastQuestion = activeIdx === request.questions.length - 1

  const currentAnswered = useMemo(() => {
    if (!currentQ) return false
    const a = answers[currentQ.id]
    if (!a) return false
    if (a.type === 'other' && !a.value.trim()) return false
    return true
  }, [answers, currentQ])

  const allAnswered = useMemo(() => {
    if (submitting || disabled) return false
    for (const q of request.questions) {
      const a = answers[q.id]
      if (!a) return false
      if (a.type === 'other' && !a.value.trim()) return false
    }
    return true
  }, [answers, request.questions, submitting, disabled])

  const handleNext = useCallback(() => {
    if (!currentAnswered || isLastQuestion) return
    setActiveIdx((i) => i + 1)
  }, [currentAnswered, isLastQuestion])

  const handleSubmit = useCallback(() => {
    if (!allAnswered) return
    setSubmitting(true)
    onSubmit({
      type: 'user_input_response',
      request_id: request.request_id,
      answers,
    })
  }, [allAnswered, onSubmit, request.request_id, answers])

  const handleOptionConfirm = useCallback((questionId: string, value: string) => {
    const updated: Record<string, UserInputAnswer> = {
      ...answers,
      [questionId]: { type: 'option', value },
    }
    setAnswers(updated)
    if (isMulti && !isLastQuestion) {
      setActiveIdx((i) => i + 1)
      return
    }
    let ready = true
    for (const q of request.questions) {
      const a = updated[q.id]
      if (!a || (a.type === 'other' && !a.value.trim())) { ready = false; break }
    }
    if (ready) {
      setSubmitting(true)
      onSubmit({ type: 'user_input_response', request_id: request.request_id, answers: updated })
    }
  }, [answers, isMulti, isLastQuestion, request, onSubmit])

  const handleDismiss = useCallback(() => {
    if (submitting || disabled) return
    onDismiss()
  }, [submitting, disabled, onDismiss])

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        handleDismiss()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [handleDismiss])

  const getOrderedOptions = (q: UserInputQuestion) => {
    const order = optionOrders[q.id]
    if (!order) return q.options
    const byValue = new Map(q.options.map((o) => [o.value, o]))
    return order.map((v) => byValue.get(v)).filter(Boolean) as typeof q.options
  }

  // 多题模式：只渲染当前题目；单题模式：渲染全部
  const visibleQuestions = isMulti ? [currentQ] : request.questions

  return (
    <div
      className="flex flex-col gap-3 w-full"
      style={{
        background: 'var(--c-bg-input)',
        borderWidth: '0.5px',
        borderStyle: 'solid',
        borderColor: hovered
          ? 'var(--c-input-border-color-hover)'
          : 'var(--c-input-border-color)',
        borderRadius: '20px',
        boxShadow: hovered
          ? 'var(--c-input-shadow-hover)'
          : 'var(--c-input-shadow)',
        transition: 'border-color 0.2s ease, box-shadow 0.2s ease',
        padding: '16px 20px 12px',
      }}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      {isMulti && (
        <div className="flex items-center gap-2 px-1">
          {request.questions.map((_, i) => (
            <div
              key={i}
              className="h-1 flex-1 rounded-full transition-colors duration-200"
              style={{
                background: i <= activeIdx
                  ? 'var(--c-text-secondary)'
                  : 'var(--c-border-subtle)',
              }}
            />
          ))}
        </div>
      )}

      {visibleQuestions.map((q) => (
        <QuestionBlock
          key={q.id}
          question={q}
          orderedOptions={getOrderedOptions(q)}
          answer={answers[q.id]}
          otherText={otherTexts[q.id] ?? ''}
          onSelectOption={(v) => selectOption(q.id, v)}
          onConfirmOption={(v) => handleOptionConfirm(q.id, v)}
          onSelectOther={() => selectOther(q.id)}
          onUpdateOther={(text) => updateOtherText(q.id, text)}
          onMoveOption={(v, d) => moveOption(q.id, v, d)}
          disabled={submitting || !!disabled}
          t={t}
        />
      ))}

      <div className="flex items-center justify-end gap-1.5 pt-1">
        <button
          type="button"
          onClick={handleDismiss}
          disabled={submitting || disabled}
          className="flex items-center gap-1 rounded-full px-2 py-1 text-[11px] transition-all cursor-pointer border-none bg-transparent"
          style={{
            color: 'var(--c-text-tertiary)',
            opacity: submitting || disabled ? 0.4 : 1,
          }}
          onMouseEnter={(e) => { e.currentTarget.style.color = 'var(--c-text-secondary)' }}
          onMouseLeave={(e) => { e.currentTarget.style.color = 'var(--c-text-tertiary)' }}
        >
          {t.userInput.dismiss}
          <kbd
            className="inline-flex items-center justify-center rounded-full px-1 text-[10px] font-mono leading-[18px]"
            style={{
              background: 'var(--c-bg-deep)',
              color: 'var(--c-text-muted)',
              border: '0.5px solid var(--c-border-subtle)',
              minWidth: '28px',
              height: '18px',
            }}
          >
            ESC
          </kbd>
        </button>

        {isMulti && !isLastQuestion ? (
          <button
            type="button"
            onClick={handleNext}
            disabled={!currentAnswered || submitting || disabled}
            className="rounded-full px-3 py-1 text-[11px] font-medium transition-opacity disabled:opacity-40 cursor-pointer border-none"
            style={{ background: 'var(--c-text-secondary)', color: 'var(--c-bg-page)' }}
          >
            {t.userInput.next}
          </button>
        ) : (
          <button
            type="button"
            onClick={handleSubmit}
            disabled={!allAnswered || submitting}
            className="rounded-full px-3 py-1 text-[11px] font-medium transition-opacity disabled:opacity-40 cursor-pointer border-none"
            style={{ background: 'var(--c-brand, #3b82f6)', color: '#fff' }}
          >
            {submitting ? t.userInput.submitting : t.userInput.submit}
          </button>
        )}
      </div>
    </div>
  )
}

// --- QuestionBlock ---

interface QuestionBlockProps {
  question: UserInputQuestion
  orderedOptions: UserInputQuestion['options']
  answer?: UserInputAnswer
  otherText: string
  onSelectOption: (value: string) => void
  onConfirmOption: (value: string) => void
  onSelectOther: () => void
  onUpdateOther: (text: string) => void
  onMoveOption: (value: string, direction: -1 | 1) => void
  disabled: boolean
  t: ReturnType<typeof useLocale>['t']
}

function QuestionBlock({
  question,
  orderedOptions,
  answer,
  otherText,
  onSelectOption,
  onConfirmOption,
  onSelectOther,
  onUpdateOther,
  onMoveOption,
  disabled,
  t,
}: QuestionBlockProps) {
  return (
    <div className="flex flex-col gap-2">
      {question.header && (
        <div
          className="text-sm font-semibold px-1"
          style={{ color: 'var(--c-text-heading)' }}
        >
          {question.header}
        </div>
      )}
      <div
        className="text-xs px-1"
        style={{ color: 'var(--c-text-primary)' }}
      >
        {question.question}
      </div>
      <div className="flex flex-col gap-0.5">
        {orderedOptions.map((opt, idx) => {
          const selected = answer?.type === 'option' && answer.value === opt.value
          return (
            <OptionRow
              key={opt.value}
              index={idx}
              option={opt}
              selected={selected}
              disabled={disabled}
              isFirst={idx === 0}
              isLast={idx === orderedOptions.length - 1}
              onSelect={() => onSelectOption(opt.value)}
              onConfirm={() => onConfirmOption(opt.value)}
              onMoveUp={() => onMoveOption(opt.value, -1)}
              onMoveDown={() => onMoveOption(opt.value, 1)}
              t={t}
            />
          )
        })}
        {question.allow_other && (
          <OtherRow
            selected={answer?.type === 'other'}
            text={otherText}
            disabled={disabled}
            onSelect={onSelectOther}
            onUpdateText={onUpdateOther}
            t={t}
          />
        )}
      </div>
    </div>
  )
}

// --- OptionRow ---

interface OptionRowProps {
  index: number
  option: UserInputQuestion['options'][number]
  selected: boolean
  disabled: boolean
  isFirst: boolean
  isLast: boolean
  onSelect: () => void
  onConfirm: () => void
  onMoveUp: () => void
  onMoveDown: () => void
  t: ReturnType<typeof useLocale>['t']
}

function OptionRow({
  index,
  option,
  selected,
  disabled,
  isFirst,
  isLast,
  onSelect,
  onConfirm,
  onMoveUp,
  onMoveDown,
  t,
}: OptionRowProps) {
  const [showTooltip, setShowTooltip] = useState(false)
  const [rowHovered, setRowHovered] = useState(false)

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={() => !disabled && onSelect()}
      onDoubleClick={() => !disabled && onConfirm()}
      onKeyDown={(e) => {
        if (disabled) return
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onSelect()
        }
      }}
      onMouseEnter={() => setRowHovered(true)}
      onMouseLeave={() => setRowHovered(false)}
      className="flex items-center gap-1.5 pl-1 pr-2 py-1 text-xs cursor-pointer"
      style={{
        background: selected
          ? 'var(--c-bg-card-hover)'
          : rowHovered && !disabled
            ? 'var(--c-bg-deep)'
            : 'transparent',
        border: selected
          ? '0.5px solid var(--c-border-mid)'
          : '0.5px solid transparent',
        borderRadius: '10px',
        color: 'var(--c-text-primary)',
        opacity: disabled ? 0.5 : 1,
        transition: 'background 0.15s ease, border-color 0.15s ease',
      }}
    >
      <span
        className="flex-shrink-0 text-[11px] font-mono w-4 text-right"
        style={{ color: 'var(--c-text-muted)' }}
      >
        {index + 1}.
      </span>

      <span className="flex-1">{option.label}</span>

      {option.recommended && (
        <span
          className="flex-shrink-0 rounded-full px-2 py-0.5 text-[10px] font-medium"
          style={{
            background: selected ? 'var(--c-border-mid)' : 'var(--c-bg-deep)',
            color: 'var(--c-text-secondary)',
          }}
        >
          {t.userInput.recommended}
        </span>
      )}

      {option.description && (
        <span
          className="relative flex-shrink-0"
          onMouseEnter={() => setShowTooltip(true)}
          onMouseLeave={() => setShowTooltip(false)}
        >
          <span
            className="inline-flex h-4 w-4 items-center justify-center rounded-full text-[10px] cursor-help"
            style={{
              border: '0.5px solid var(--c-border-subtle)',
              color: 'var(--c-text-muted)',
            }}
          >
            i
          </span>
          {showTooltip && (
            <div
              className="absolute bottom-full right-0 z-10 mb-1 max-w-[200px] rounded-xl px-2.5 py-1.5 text-xs"
              style={{
                background: 'var(--c-bg-menu)',
                border: '0.5px solid var(--c-border-subtle)',
                color: 'var(--c-text-secondary)',
                boxShadow: '0 2px 8px rgba(0,0,0,0.15)',
              }}
            >
              {option.description}
            </div>
          )}
        </span>
      )}

      <div
        className="flex flex-shrink-0 flex-col"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.stopPropagation()}
      >
        <button
          type="button"
          tabIndex={-1}
          disabled={disabled || isFirst}
          onClick={(e) => { e.stopPropagation(); onMoveUp() }}
          className="flex h-4 w-5 items-center justify-center border-none bg-transparent cursor-pointer disabled:opacity-20 transition-opacity"
          style={{ color: 'var(--c-text-muted)' }}
          aria-label="Move up"
        >
          <ChevronUp size={12} />
        </button>
        <button
          type="button"
          tabIndex={-1}
          disabled={disabled || isLast}
          onClick={(e) => { e.stopPropagation(); onMoveDown() }}
          className="flex h-4 w-5 items-center justify-center border-none bg-transparent cursor-pointer disabled:opacity-20 transition-opacity"
          style={{ color: 'var(--c-text-muted)' }}
          aria-label="Move down"
        >
          <ChevronDown size={12} />
        </button>
      </div>
    </div>
  )
}

// --- OtherRow ---

interface OtherRowProps {
  selected: boolean
  text: string
  disabled: boolean
  onSelect: () => void
  onUpdateText: (text: string) => void
  t: ReturnType<typeof useLocale>['t']
}

function OtherRow({ selected, text, disabled, onSelect, onUpdateText, t }: OtherRowProps) {
  const [rowHovered, setRowHovered] = useState(false)

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={() => !disabled && onSelect()}
      onKeyDown={(e) => {
        if (disabled) return
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onSelect()
        }
      }}
      onMouseEnter={() => setRowHovered(true)}
      onMouseLeave={() => setRowHovered(false)}
      className="flex items-center gap-1.5 pl-1 pr-2 py-1 text-xs cursor-pointer"
      style={{
        background: selected
          ? 'var(--c-bg-card-hover)'
          : rowHovered && !disabled
            ? 'var(--c-bg-deep)'
            : 'transparent',
        border: selected
          ? '0.5px solid var(--c-border-mid)'
          : '0.5px solid transparent',
        borderRadius: '10px',
        color: 'var(--c-text-primary)',
        opacity: disabled ? 0.5 : 1,
        transition: 'background 0.15s ease, border-color 0.15s ease',
      }}
    >
      <span
        className="flex-shrink-0 text-[11px] font-mono w-4 text-right"
        style={{ color: 'var(--c-text-muted)' }}
      >
        *
      </span>
      <input
        type="text"
        value={text}
        onChange={(e) => { onUpdateText(e.target.value); onSelect() }}
        onClick={(e) => { e.stopPropagation(); onSelect() }}
        disabled={disabled}
        placeholder={t.userInput.otherPlaceholder}
        className="flex-1 bg-transparent border-none outline-none text-xs"
        style={{
          color: 'var(--c-text-primary)',
          caretColor: 'var(--c-text-primary)',
        }}
      />
    </div>
  )
}
