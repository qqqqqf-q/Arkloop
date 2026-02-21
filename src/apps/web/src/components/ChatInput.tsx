import { useRef, useEffect, useCallback, useState } from 'react'
import { Plus, ChevronDown, ArrowUp, Square, Paperclip } from 'lucide-react'
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
  const [proExpanded, setProExpanded] = useState(false)
  const [proHovered, setProHovered] = useState(false)

  const adjustHeight = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`
  }, [])

  useEffect(() => {
    adjustHeight()
  }, [value, adjustHeight])

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
    e.target.value = ''
    setMenuOpen(false)
  }

  const borderColor = variant === 'welcome' ? 'var(--c-border)' : 'var(--c-border-mid)'

  return (
    <div
      className="w-full max-w-[756px] rounded-2xl bg-[var(--c-bg-input)]"
      style={{
        border: `0.5px solid ${borderColor}`,
        borderRadius: '16px',
        padding: '20px 24px',
        boxShadow: '0 2px 8px rgba(0, 0, 0, 0.08)',
      }}
    >
      <form onSubmit={onSubmit}>
        <textarea
          ref={textareaRef}
          rows={1}
          className="w-full resize-none bg-transparent outline-none placeholder:text-[var(--c-text-muted)] disabled:cursor-not-allowed"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          disabled={disabled || isStreaming}
          style={{
            fontFamily: 'inherit',
            fontSize: '16px',
            color: 'var(--c-text-tertiary)',
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
              className="relative top-px flex h-8 w-8 items-center justify-center rounded-lg text-[var(--c-text-secondary)] opacity-70 transition-[opacity,background] duration-150 hover:bg-[var(--c-bg-deep)] hover:opacity-100"
            >
              <Plus size={20} />
            </button>

            {menuOpen && (
              <div
                ref={menuRef}
                className="absolute left-0 z-50 overflow-hidden rounded-xl"
                style={{
                  top: 'calc(100% + 8px)',
                  background: 'var(--c-bg-menu)',
                  border: '0.5px solid var(--c-border-subtle)',
                  boxShadow: '0 8px 24px rgba(0,0,0,0.15)',
                  minWidth: '200px',
                }}
              >
                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  className="flex w-full items-center gap-3 px-4 py-3 text-sm transition-colors duration-100 hover:bg-[var(--c-bg-deep)]"
                  style={{ color: 'var(--c-text-secondary)' }}
                >
                  <Paperclip size={15} style={{ color: 'var(--c-text-icon)' }} />
                  从本地文件添加
                </button>
              </div>
            )}
          </div>

          <button
            type="button"
            onClick={() => setProExpanded((v) => !v)}
            onMouseEnter={() => setProHovered(true)}
            onMouseLeave={() => setProHovered(false)}
            className="relative top-px -ml-1 h-8 rounded-lg font-semibold"
            style={{
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'flex-start',
              paddingLeft: '12px',
              background: proExpanded ? 'var(--c-pro-bg)' : proHovered ? 'var(--c-bg-deep)' : 'transparent',
              color: proExpanded ? '#4691F6' : 'var(--c-text-secondary)',
              opacity: proExpanded ? 1 : proHovered ? 1 : 0.7,
              fontSize: '15px',
              width: proExpanded ? '48px' : '32px',
              overflow: 'hidden',
              flexShrink: 0,
              whiteSpace: 'nowrap',
              transition: 'width 0.22s ease, background-color 0.15s ease, color 0.2s ease, opacity 0.15s ease',
            }}
          >
            P<span
              style={{
                display: 'inline-block',
                overflow: 'hidden',
                maxWidth: proExpanded ? '20px' : '0px',
                opacity: proExpanded ? 1 : 0,
                transition: proExpanded
                  ? 'max-width 0.22s ease, opacity 0.15s ease 0.07s'
                  : 'opacity 0.07s ease, max-width 0.18s ease 0.04s',
              }}
            >ro</span>
          </button>

          <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '6px' }}>
            <div
              style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer', color: 'var(--c-text-secondary)', fontSize: '14px', padding: '4px 8px', borderRadius: '6px' }}
            >
              <span>Sonnet 4.5</span>
              <ChevronDown size={14} />
            </div>

            {isStreaming && canCancel ? (
              <button
                type="button"
                onClick={onCancel}
                disabled={cancelSubmitting}
                className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--c-accent-send)] text-[var(--c-accent-send-text)] transition-colors duration-150 hover:bg-[var(--c-accent-send-hover)] disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Square size={14} fill="currentColor" />
              </button>
            ) : (
              <button
                type="submit"
                disabled={disabled || isStreaming || (!value.trim() && attachments.length === 0)}
                className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--c-accent-send)] text-[var(--c-accent-send-text)] transition-colors duration-150 hover:bg-[var(--c-accent-send-hover)] active:scale-[0.96] disabled:cursor-not-allowed disabled:opacity-50"
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
