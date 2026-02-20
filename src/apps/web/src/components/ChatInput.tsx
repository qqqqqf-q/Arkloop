import { useRef, useEffect, useCallback } from 'react'
import { Plus, Clock, ChevronDown, ArrowUp, Square } from 'lucide-react'
import type { FormEvent, KeyboardEvent } from 'react'

type Props = {
  value: string
  onChange: (val: string) => void
  onSubmit: (e: FormEvent<HTMLFormElement>) => void
  onCancel?: () => void
  placeholder?: string
  disabled?: boolean
  isStreaming?: boolean
  canCancel?: boolean
  cancelSubmitting?: boolean
  variant?: 'welcome' | 'chat'
}

export function ChatInput({
  value,
  onChange,
  onSubmit,
  onCancel,
  placeholder = '输入消息...',
  disabled = false,
  isStreaming = false,
  canCancel = false,
  cancelSubmitting = false,
  variant = 'chat',
}: Props) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const adjustHeight = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`
  }, [])

  useEffect(() => {
    adjustHeight()
  }, [value, adjustHeight])

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      if (!disabled && !isStreaming && value.trim()) {
        e.currentTarget.form?.requestSubmit()
      }
    }
  }

  const borderColor = variant === 'welcome' ? '#40403d' : '#5e5e5c'

  return (
    <div
      className="w-full max-w-[756px] rounded-2xl bg-[#30302e]"
      style={{
        border: `0.5px solid ${borderColor}`,
        borderRadius: '16px',
        padding: '20px 24px',
        boxShadow: '0 2px 8px rgba(0, 0, 0, 0.15)',
      }}
    >
      <form onSubmit={onSubmit}>
        <textarea
          ref={textareaRef}
          rows={1}
          className="w-full resize-none bg-transparent outline-none placeholder:text-[#6b6b68] disabled:cursor-not-allowed"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          disabled={disabled || isStreaming}
          style={{
            fontFamily: 'inherit',
            fontSize: '16px',
            color: '#9c9a92',
            marginBottom: '16px',
            letterSpacing: '-0.16px',
            overflow: 'hidden',
          }}
        />

        <div className="flex items-center" style={{ gap: '12px' }}>
          <button
            type="button"
            className="relative top-px flex h-8 w-8 items-center justify-center rounded-lg text-[#c2c0b6] opacity-70 transition-[opacity,background] duration-150 hover:bg-[#141413] hover:opacity-100"
          >
            <Plus size={20} />
          </button>
          <button
            type="button"
            className="relative top-px -ml-1 flex h-8 w-8 items-center justify-center rounded-lg text-[#c2c0b6] opacity-70 transition-[opacity,background] duration-150 hover:bg-[#141413] hover:opacity-100"
          >
            <Clock size={18} />
          </button>

          <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '6px' }}>
            <div
              style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer', color: '#c2c0b6', fontSize: '14px', padding: '4px 8px', borderRadius: '6px' }}
            >
              <span>Sonnet 4.5</span>
              <ChevronDown size={14} />
            </div>

            {isStreaming && canCancel ? (
              <button
                type="button"
                onClick={onCancel}
                disabled={cancelSubmitting}
                className="flex h-8 w-8 items-center justify-center rounded-lg bg-[#7b4937] text-[#e8e8e3] transition-colors duration-150 hover:bg-[#8d5541] disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Square size={14} fill="currentColor" />
              </button>
            ) : (
              <button
                type="submit"
                disabled={disabled || isStreaming || !value.trim()}
                className="flex h-8 w-8 items-center justify-center rounded-lg bg-[#7b4937] text-[#e8e8e3] transition-colors duration-150 hover:bg-[#8d5541] active:scale-[0.96] disabled:cursor-not-allowed disabled:opacity-50"
              >
                <ArrowUp size={16} />
              </button>
            )}
          </div>
        </div>
      </form>
    </div>
  )
}
