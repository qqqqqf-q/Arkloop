import { useCallback, useEffect, useState } from 'react'
import { ChevronLeft, ChevronRight, ArrowRight, X } from 'lucide-react'
import { useLocale } from '../contexts/LocaleContext'
import type {
  UserInputAnswer,
  UserInputRequest,
  UserInputResponse,
} from '../userInputTypes'

function buildInitialAnswers(questions: UserInputRequest['questions']): Record<string, UserInputAnswer> {
  const initial: Record<string, UserInputAnswer> = {}
  for (const question of questions) {
    const recommended = question.options.find((option) => option.recommended)
    if (recommended) {
      initial[question.id] = { type: 'option', value: recommended.value }
    }
  }
  return initial
}

function isAnswerComplete(answer?: UserInputAnswer): boolean {
  if (!answer) {
    return false
  }
  return answer.type === 'option' || !!answer.value.trim()
}

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
  const [answers, setAnswers] = useState<Record<string, UserInputAnswer>>(() => buildInitialAnswers(request.questions))
  const [otherTexts, setOtherTexts] = useState<Record<string, string>>({})
  const [submitting, setSubmitting] = useState(false)
  const [cardHovered, setCardHovered] = useState(false)
  const [hoveredOptIdx, setHoveredOptIdx] = useState<number | null>(null)

  const currentQ = request.questions[activeIdx]
  const isLastQuestion = activeIdx === request.questions.length - 1
  const currentAnswer = answers[currentQ.id]
  const currentAnswered = isAnswerComplete(currentAnswer)
  const allAnswered = request.questions.every((question) => isAnswerComplete(answers[question.id]))

  const doSubmit = useCallback((latestAnswers: Record<string, UserInputAnswer>) => {
    setSubmitting(true)
    onSubmit({ type: 'user_input_response', request_id: request.request_id, answers: latestAnswers })
  }, [onSubmit, request.request_id])

  const handleOptionClick = useCallback((questionId: string, value: string) => {
    const updated: Record<string, UserInputAnswer> = { ...answers, [questionId]: { type: 'option', value } }
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
    if (ready) doSubmit(updated)
  }, [answers, isMulti, isLastQuestion, request.questions, doSubmit])

  const handleDismiss = useCallback(() => {
    if (submitting || disabled) return
    onDismiss()
  }, [submitting, disabled, onDismiss])

  const primaryEnabled = !submitting && !disabled && (allAnswered || (isMulti && !isLastQuestion && currentAnswered))

  const handlePrimaryAction = useCallback(() => {
    if (!primaryEnabled) {
      return
    }
    if (isMulti && !isLastQuestion) {
      setActiveIdx((index) => index + 1)
      return
    }
    doSubmit(answers)
  }, [answers, doSubmit, isLastQuestion, isMulti, primaryEnabled])

  const handleSelectOther = useCallback(() => {
    setAnswers((prev) => ({
      ...prev,
      [currentQ.id]: { type: 'other', value: otherTexts[currentQ.id] ?? '' },
    }))
  }, [currentQ.id, otherTexts])

  const handleUpdateOther = useCallback((text: string) => {
    setOtherTexts((prev) => ({ ...prev, [currentQ.id]: text }))
    setAnswers((prev) => ({
      ...prev,
      [currentQ.id]: { type: 'other', value: text },
    }))
  }, [currentQ.id])

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.preventDefault(); handleDismiss() }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [handleDismiss])

  const otherSelected = answers[currentQ.id]?.type === 'other'
  const otherText = otherTexts[currentQ.id] ?? ''

  // 卡片水平 padding，用于选项容器负 margin 延伸到边缘
  const CARD_H_PAD = 22

  return (
    <div
      className="flex flex-col w-full"
      style={{
        background: 'var(--c-bg-input)',
        borderWidth: '0.5px',
        borderStyle: 'solid',
        borderColor: cardHovered ? 'var(--c-input-border-color-hover)' : 'var(--c-input-border-color)',
        borderRadius: '20px',
        boxShadow: cardHovered ? 'var(--c-input-shadow-hover)' : 'var(--c-input-shadow)',
        transition: 'border-color 0.2s ease, box-shadow 0.2s ease',
        padding: `18px ${CARD_H_PAD}px 16px`,
      }}
      onMouseEnter={() => setCardHovered(true)}
      onMouseLeave={() => setCardHovered(false)}
    >
      {/* Header */}
      <div className="flex items-start justify-between gap-3 mb-4">
        <div className="flex flex-col gap-0.5 flex-1">
          {currentQ.header && (
            <span
              className="text-[10px] font-normal"
              style={{ color: 'var(--c-text-tertiary)', letterSpacing: '0.06em', textTransform: 'uppercase' }}
            >
              {currentQ.header}
            </span>
          )}
          <h2 className="text-[17px] font-normal leading-snug m-0" style={{ color: 'var(--c-text-secondary)' }}>
            {currentQ.question}
          </h2>
        </div>

        <div className="flex items-center gap-1 flex-shrink-0 mt-0.5">
          {isMulti && (
            <>
              <button
                type="button"
                onClick={() => activeIdx > 0 && setActiveIdx((i) => i - 1)}
                disabled={activeIdx === 0}
                className="flex h-6 w-6 items-center justify-center rounded-md border-none bg-transparent cursor-pointer disabled:opacity-25 transition-[background-color] duration-[60ms] hover:bg-[var(--c-bg-deep)]"
                style={{ color: 'var(--c-text-secondary)' }}
              >
                <ChevronLeft size={14} />
              </button>
              <span className="text-xs tabular-nums px-0.5" style={{ color: 'var(--c-text-tertiary)' }}>
                {activeIdx + 1}/{request.questions.length}
              </span>
              <button
                type="button"
                onClick={() => !isLastQuestion && setActiveIdx((i) => i + 1)}
                disabled={isLastQuestion}
                className="flex h-6 w-6 items-center justify-center rounded-md border-none bg-transparent cursor-pointer disabled:opacity-25 transition-[background-color] duration-[60ms] hover:bg-[var(--c-bg-deep)]"
                style={{ color: 'var(--c-text-secondary)' }}
              >
                <ChevronRight size={14} />
              </button>
              <div style={{ width: '1px', height: '14px', background: 'var(--c-border-subtle)', margin: '0 2px' }} />
            </>
          )}
          <button
            type="button"
            onClick={handleDismiss}
            disabled={submitting || !!disabled}
            aria-label={t.userInput.dismiss}
            className="flex h-6 w-6 items-center justify-center rounded-md border-none bg-transparent cursor-pointer disabled:opacity-30 transition-[background-color] duration-[60ms] hover:bg-[var(--c-bg-deep)]"
            style={{ color: 'var(--c-text-muted)' }}
          >
            <X size={13} />
          </button>
        </div>
      </div>

      {/*
        Options list
        负 margin = 卡片水平 padding，让选项行的 hover 背景可以延伸到卡片内边缘
        分割线 marginLeft/Right = 0，相对于此容器就是全宽，与卡片边缘对齐
        行内再用 paddingLeft/Right 补回内缩距离 + 额外 8px 留给 hover 的呼吸感
      */}
      <div
        className="flex flex-col"
        style={{ marginLeft: 0, marginRight: 0 }}
      >
        {currentQ.options.map((opt, idx) => {
          const selected = answers[currentQ.id]?.type === 'option' && answers[currentQ.id]?.value === opt.value
          const isHovered = hoveredOptIdx === idx
          const showDivider = idx < currentQ.options.length - 1
          const dividerVisible = hoveredOptIdx !== idx && hoveredOptIdx !== idx + 1
          return (
            <div key={opt.value}>
              <OptionRow
                index={idx}
                option={opt}
                selected={selected}
                disabled={submitting || !!disabled}
                isHovered={isHovered}
                onHover={() => setHoveredOptIdx(idx)}
                onHoverEnd={() => setHoveredOptIdx(null)}
                onClick={() => handleOptionClick(currentQ.id, opt.value)}
                t={t}
              />
              {showDivider && (
                <div
                  style={{
                    height: '0.5px',
                    background: 'var(--c-border-subtle)',
                    // marginLeft/Right = 0：分割线与容器等宽，即与卡片边缘对齐
                    opacity: dividerVisible ? 1 : 0,
                    transition: 'opacity 60ms ease',
                  }}
                />
              )}
            </div>
          )
        })}
      </div>

      {/*
        Footer
        margin-top = 0：让最后一个选项的 paddingBottom (13px) 自然充当与分割线的间距
        这样底部间距 ≈ 顶部标题 mb-4 (16px)，视觉上均匀
      */}
      <div
        className="flex items-center justify-between pt-3"
        style={{ borderTop: '0.5px solid var(--c-border-subtle)', marginTop: 0 }}
      >
        {currentQ.allow_other ? (
          <OtherInput
            selected={otherSelected}
            text={otherText}
            disabled={submitting || !!disabled}
            onSelect={handleSelectOther}
            onUpdateText={handleUpdateOther}
            onSubmit={handlePrimaryAction}
            t={t}
          />
        ) : (
          <span />
        )}

        <div className="flex items-center gap-1.5 flex-shrink-0">
          <button
            type="button"
            onClick={handlePrimaryAction}
            aria-label={t.userInput.submit}
            data-testid="user-input-submit"
            disabled={!primaryEnabled}
            className="flex h-7 w-7 items-center justify-center rounded-lg border-none cursor-pointer transition-[background-color,color] duration-[60ms] disabled:opacity-30"
            style={{
              background: primaryEnabled ? 'var(--c-text-primary)' : 'var(--c-bg-deep)',
              color: primaryEnabled ? 'var(--c-bg-page)' : 'var(--c-text-muted)',
            }}
          >
            <ArrowRight size={13} />
          </button>
          <button
            type="button"
            onClick={handleDismiss}
            disabled={submitting || !!disabled}
            className="rounded-lg px-3 py-1.5 text-[13px] border-none bg-transparent cursor-pointer transition-[background-color] duration-[60ms] disabled:opacity-40 hover:bg-[var(--c-bg-deep)]"
            style={{ color: 'var(--c-text-secondary)' }}
          >
            {t.userInput.dismiss}
          </button>
        </div>
      </div>
    </div>
  )
}

// --- OptionRow ---

interface OptionRowProps {
  index: number
  option: { value: string; label: string; description?: string; recommended?: boolean }
  selected: boolean
  disabled: boolean
  isHovered: boolean

  onHover: () => void
  onHoverEnd: () => void
  onClick: () => void
  t: ReturnType<typeof useLocale>['t']
}

function OptionRow({ index, option, selected, disabled, isHovered, onHover, onHoverEnd, onClick, t }: OptionRowProps) {
  const [showTooltip, setShowTooltip] = useState(false)

  // badge 颜色策略（深色/浅色模式通用）：
  //   已选中  → --c-text-primary（最亮），字色 --c-bg-page（最深）→ 最强对比
  //   hover   → --c-border-subtle（比 --c-bg-deep 深一档），字色 --c-text-secondary
  //   默认    → --c-bg-deep，字色 --c-text-muted（最灰），与选中态拉开最大距离
  const badgeBg = selected
    ? 'var(--c-text-primary)'
    : isHovered && !disabled
      ? 'var(--c-border-subtle)'
      : 'var(--c-bg-deep)'

  const badgeColor = selected
    ? 'var(--c-bg-page)'
    : isHovered && !disabled
      ? 'var(--c-text-secondary)'
      : 'var(--c-text-muted)'

  // 行的水平 padding：不再抵消负 margin，直接使用 8px 呼吸感
  const rowPx = 8;

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={() => !disabled && onClick()}
      onKeyDown={(e) => {
        if (disabled) return
        if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onClick() }
      }}
      onMouseEnter={onHover}
      onMouseLeave={onHoverEnd}
      className="flex items-center gap-3 cursor-pointer"
      style={{
        background: isHovered && !disabled ? 'var(--c-bg-deep)' : 'transparent',
        borderRadius: '10px',
        opacity: disabled ? 0.5 : 1,
        transition: 'background 60ms ease',
        paddingTop: '13px',
        paddingBottom: '13px',
        paddingLeft: `${rowPx}px`,
        paddingRight: `${rowPx}px`,
      }}
    >
      <div
        className="flex-shrink-0 flex items-center justify-center rounded-md text-[12px] font-medium"
        style={{
          width: '26px',
          height: '26px',
          background: badgeBg,
          color: badgeColor,
          transition: 'background 60ms ease, color 60ms ease',
          flexShrink: 0,
        }}
      >
        {index + 1}
      </div>

      <span className="flex-1 text-[14.5px] font-light" style={{ color: 'var(--c-text-primary)' }}>
        {option.label}
        {option.recommended && (
          <span className="ml-1.5 opacity-60 text-[13px]">
            {t.userInput.recommended}
          </span>
        )}
      </span>



      {option.description && (
        <span
          className="relative flex-shrink-0"
          onMouseEnter={() => setShowTooltip(true)}
          onMouseLeave={() => setShowTooltip(false)}
        >
          <span
            className="inline-flex h-5 w-5 items-center justify-center rounded-full text-[10px] cursor-help"
            style={{ border: '0.5px solid var(--c-border-subtle)', color: 'var(--c-text-muted)' }}
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

      <ArrowRight
        size={13}
        style={{
          flexShrink: 0,
          color: 'var(--c-text-tertiary)',
          opacity: isHovered && !disabled ? 1 : 0,
          transition: 'opacity 80ms ease',
        }}
      />
    </div>
  )
}

// --- OtherInput ---

interface OtherInputProps {
  selected: boolean
  text: string
  disabled: boolean
  onSelect: () => void
  onUpdateText: (text: string) => void
  onSubmit: () => void
  t: ReturnType<typeof useLocale>['t']
}

function OtherInput({ selected, text, disabled, onSelect, onUpdateText, onSubmit, t }: OtherInputProps) {
  return (
    <div className="flex items-center gap-2 flex-1 mr-3">
      <div
        className="flex-shrink-0 flex items-center justify-center rounded-md text-[12px] font-medium"
        style={{
          width: '22px',
          height: '22px',
          background: selected ? 'var(--c-text-primary)' : 'var(--c-bg-deep)',
          color: selected ? 'var(--c-bg-page)' : 'var(--c-text-muted)',
          transition: 'background 60ms ease, color 60ms ease',
          flexShrink: 0,
        }}
      >
        *
      </div>
      <input
        type="text"
        value={text}
        onChange={(e) => { onUpdateText(e.target.value); onSelect() }}
        onClick={onSelect}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && text.trim()) { e.preventDefault(); onSubmit() }
        }}
        disabled={disabled}
        placeholder={t.userInput.otherPlaceholder}
        className="flex-1 bg-transparent border-none outline-none text-[13px] font-light"
        style={{ color: 'var(--c-text-primary)', caretColor: 'var(--c-text-primary)' }}
      />
    </div>
  )
}
