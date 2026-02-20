import { useRef, useEffect, useCallback, useState } from 'react'
import { Plus, Clock, ChevronDown, ArrowUp, Square, Paperclip } from 'lucide-react'
import type { FormEvent, KeyboardEvent } from 'react'

export type Attachment = {
  id: string
  name: string
  size: number
  content: string
  encoding: 'text' | 'base64'
}

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
  attachments?: Attachment[]
  onAttachFiles?: (files: File[]) => void
}

export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
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
  attachments = [],
  onAttachFiles,
}: Props) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const plusBtnRef = useRef<HTMLButtonElement>(null)
  const [menuOpen, setMenuOpen] = useState(false)

  const adjustHeight = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`
  }, [])

  useEffect(() => {
    adjustHeight()
  }, [value, adjustHeight])

  // 点击外部关闭菜单
  useEffect(() => {
    if (!menuOpen) return
    const handleClick = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node) ||
        plusBtnRef.current?.contains(e.target as Node)
      ) return
      setMenuOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [menuOpen])

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      if (!disabled && !isStreaming && value.trim()) {
        e.currentTarget.form?.requestSubmit()
      }
    }
  }

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? [])
    if (files.length > 0) onAttachFiles?.(files)
    // 重置 input 以允许重复选择同一文件
    e.target.value = ''
    setMenuOpen(false)
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
          {/* + 按钮及菜单 */}
          <div className="relative">
            <button
              ref={plusBtnRef}
              type="button"
              onClick={() => setMenuOpen((v) => !v)}
              className="relative top-px flex h-8 w-8 items-center justify-center rounded-lg text-[#c2c0b6] opacity-70 transition-[opacity,background] duration-150 hover:bg-[#141413] hover:opacity-100"
            >
              <Plus size={20} />
            </button>

            {menuOpen && (
              <div
                ref={menuRef}
                className="absolute left-0 z-50 overflow-hidden rounded-xl"
                style={{
                  top: 'calc(100% + 8px)',
                  background: '#2a2a28',
                  border: '0.5px solid #3a3a38',
                  boxShadow: '0 8px 24px rgba(0,0,0,0.4)',
                  minWidth: '200px',
                }}
              >
                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  className="flex w-full items-center gap-3 px-4 py-3 text-sm transition-colors duration-100 hover:bg-[#333330]"
                  style={{ color: '#c2c0b6' }}
                >
                  <Paperclip size={15} style={{ color: '#7b7970' }} />
                  从本地文件添加
                </button>
              </div>
            )}
          </div>

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
                disabled={disabled || isStreaming || (!value.trim() && attachments.length === 0)}
                className="flex h-8 w-8 items-center justify-center rounded-lg bg-[#7b4937] text-[#e8e8e3] transition-colors duration-150 hover:bg-[#8d5541] active:scale-[0.96] disabled:cursor-not-allowed disabled:opacity-50"
              >
                <ArrowUp size={16} />
              </button>
            )}
          </div>
        </div>
      </form>

      {/* 隐藏的 file input */}
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={handleFileChange}
      />
    </div>
  )
}
