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
  const tierMenuRef = useRef<HTMLDivElement>(null)
  const chevronBtnRef = useRef<HTMLButtonElement>(null)
  const [menuOpen, setMenuOpen] = useState(false)
  const [tierMenuOpen, setTierMenuOpen] = useState(false)
  const [selectedTier, setSelectedTier] = useState<'Auto' | 'Lite' | 'Pro' | 'Ultra'>('Lite')
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

  useEffect(() => {
    if (!tierMenuOpen) return
    const handleClick = (e: MouseEvent) => {
      if (
        tierMenuRef.current?.contains(e.target as Node) ||
        chevronBtnRef.current?.contains(e.target as Node)
      ) return
      setTierMenuOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [tierMenuOpen])

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      if (!disabled && value.trim()) {
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

  const cycleTier = () => {
    setSelectedTier((prev) => prev === 'Lite' ? 'Pro' : prev === 'Pro' ? 'Ultra' : prev === 'Ultra' ? 'Auto' : 'Lite')
  }

  const handleTierSelect = (tier: 'Auto' | 'Lite' | 'Pro' | 'Ultra') => {
    setSelectedTier(tier)
    setTierMenuOpen(false)
  }

  const borderColor = variant === 'welcome' ? 'var(--c-border)' : 'var(--c-border-mid)'

  return (
    <div
      className="w-full max-w-[756px] rounded-2xl bg-[var(--c-bg-input)]"
      style={{
        border: `0.5px solid ${borderColor}`,
        borderRadius: '16px',
        padding: '20px 24px',
        boxShadow: '0 1px 4px rgba(0, 0, 0, 0.04)',
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
          disabled={disabled}
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
          <div className="relative -ml-1.5">
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
                className="absolute left-0 z-50"
                style={{
                  ...(variant === 'welcome'
                    ? { top: 'calc(100% + 8px)' }
                    : { bottom: 'calc(100% + 8px)' }),
                  border: '0.5px solid var(--c-border-subtle)',
                  borderRadius: '10px',
                  padding: '4px',
                  background: '#FFFFFF',
                  minWidth: '200px',
                }}
              >
                <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
                  <button
                    type="button"
                    onClick={() => fileInputRef.current?.click()}
                    className="flex w-full items-center gap-2 px-3 py-2 text-sm transition-colors duration-100"
                    style={{ color: '#000000', background: '#FFFFFF', borderRadius: '8px' }}
                    onMouseEnter={(e) => (e.currentTarget.style.background = '#F3F3F3')}
                    onMouseLeave={(e) => (e.currentTarget.style.background = '#FFFFFF')}
                  >
                    <Paperclip size={14} style={{ color: '#000000' }} />
                    从本地文件添加
                  </button>
                  <button
                    type="button"
                    className="flex w-full items-center gap-2 px-3 py-2 text-sm transition-colors duration-100"
                    style={{ color: '#000000', background: '#FFFFFF', borderRadius: '8px' }}
                    onMouseEnter={(e) => (e.currentTarget.style.background = '#F3F3F3')}
                    onMouseLeave={(e) => (e.currentTarget.style.background = '#FFFFFF')}
                  >
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="currentColor" style={{ color: '#000000', flexShrink: 0 }}>
                      <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z" />
                    </svg>
                    从 GitHub 添加
                  </button>
                </div>
              </div>
            )}
          </div>

          <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '4px' }}>
            <button
              type="button"
              onClick={cycleTier}
              onMouseEnter={() => setProHovered(true)}
              onMouseLeave={() => setProHovered(false)}
              className="relative top-px flex h-8 items-center rounded-lg font-semibold"
              style={{
                padding: '0 10px',
                justifyContent: 'center',
                width: selectedTier === 'Lite' ? '50px' : selectedTier === 'Pro' ? '44px' : selectedTier === 'Ultra' ? '58px' : '54px',
                overflow: 'hidden',
                whiteSpace: 'nowrap',
                flexShrink: 0,
                background: (selectedTier === 'Pro' || selectedTier === 'Ultra') ? 'var(--c-pro-bg)' : proHovered ? 'var(--c-bg-deep)' : 'transparent',
                color: (selectedTier === 'Pro' || selectedTier === 'Ultra') ? '#4691F6' : 'var(--c-text-secondary)',
                opacity: (selectedTier === 'Pro' || selectedTier === 'Ultra') ? 1 : proHovered ? 1 : 0.7,
                fontSize: '14px',
                transition: 'width 0.22s ease, background-color 0.15s ease, color 0.2s ease, opacity 0.15s ease',
              }}
            >
              {selectedTier}
            </button>

            <div className="relative">
              <button
                ref={chevronBtnRef}
                type="button"
                onClick={() => setTierMenuOpen((v) => !v)}
                className="relative top-px flex h-8 w-8 items-center justify-center rounded-lg text-[var(--c-text-secondary)] opacity-70 transition-[opacity,background] duration-150 hover:bg-[var(--c-bg-deep)] hover:opacity-100"
              >
                <ChevronDown size={16} />
              </button>

              {tierMenuOpen && (
                <div
                  ref={tierMenuRef}
                  className="absolute right-0 z-50"
                  style={{
                    ...(variant === 'welcome'
                      ? { top: 'calc(100% + 8px)' }
                      : { bottom: 'calc(100% + 8px)' }),
                    border: '0.5px solid var(--c-border-subtle)',
                    borderRadius: '10px',
                    padding: '4px',
                    background: '#FFFFFF',
                    minWidth: '120px',
                    boxShadow: '0 8px 24px rgba(0,0,0,0.12)',
                  }}
                >
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
                    {(['Auto', 'Lite', 'Pro', 'Ultra'] as const).map((tier) => {
                      const isBlue = tier === 'Pro' || tier === 'Ultra'
                      const isSelected = selectedTier === tier
                      return (
                        <button
                          key={tier}
                          type="button"
                          onClick={() => handleTierSelect(tier)}
                          className="flex w-full items-center px-3 py-2 text-sm transition-colors duration-100"
                          style={{
                            borderRadius: '8px',
                            background: '#FFFFFF',
                            color: isSelected && isBlue ? '#4691F6' : 'var(--c-text-secondary)',
                            fontWeight: isSelected ? 600 : 400,
                          }}
                          onMouseEnter={(e) => (e.currentTarget.style.background = '#F3F3F3')}
                          onMouseLeave={(e) => (e.currentTarget.style.background = '#FFFFFF')}
                        >
                          {tier}
                        </button>
                      )
                    })}
                  </div>
                </div>
              )}
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
