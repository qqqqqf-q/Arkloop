import { useState, useRef, useEffect } from 'react'
import { Copy, Check, RefreshCw, Share2, Split, Paperclip, Pencil } from 'lucide-react'
import type { MessageResponse } from '../api'
import { MarkdownRenderer } from './MarkdownRenderer'

type Props = {
  message: MessageResponse
  onRetry?: () => void
  onEdit?: (newContent: string) => void
}

function extractFilesFromContent(content: string): { text: string; fileNames: string[] } {
  const fileNames: string[] = []
  const text = content
    .replace(/<file name="([^"]+)" encoding="[^"]+">[\s\S]*?<\/file>/g, (_, name: string) => {
      fileNames.push(name)
      return ''
    })
    .trim()
  return { text, fileNames }
}

export function MessageBubble({ message, onRetry, onEdit }: Props) {
  const [copied, setCopied] = useState(false)
  const [hovered, setHovered] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editText, setEditText] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const handleCopy = () => {
    const { text } = extractFilesFromContent(message.content)
    const plainText = message.role === 'user' ? text : message.content
    void navigator.clipboard.writeText(plainText).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }

  const handleEditStart = () => {
    const { text } = extractFilesFromContent(message.content)
    setEditText(text)
    setEditing(true)
    setHovered(false)
  }

  const handleEditCancel = () => {
    setEditing(false)
    setEditText('')
  }

  const handleEditDone = () => {
    const trimmed = editText.trim()
    if (trimmed && onEdit) {
      onEdit(trimmed)
    }
    setEditing(false)
    setEditText('')
  }

  // 自动调整 textarea 高度
  useEffect(() => {
    if (editing && textareaRef.current) {
      const el = textareaRef.current
      el.style.height = 'auto'
      el.style.height = `${el.scrollHeight}px`
      el.focus()
      el.setSelectionRange(el.value.length, el.value.length)
    }
  }, [editing])

  const handleTextareaInput = () => {
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto'
      textareaRef.current.style.height = `${textareaRef.current.scrollHeight}px`
    }
  }

  if (message.role === 'user') {
    const { text, fileNames } = extractFilesFromContent(message.content)

    if (editing) {
      return (
        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', width: '100%', maxWidth: '663px' }}>
            {fileNames.length > 0 && (
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px', justifyContent: 'flex-end' }}>
                {fileNames.map((name) => (
                  <div
                    key={name}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: '6px',
                      background: 'var(--c-bg-sub)',
                      border: '0.5px solid var(--c-border-subtle)',
                      borderRadius: '8px',
                      padding: '4px 10px',
                      fontSize: '12px',
                      color: 'var(--c-text-secondary)',
                    }}
                  >
                    <Paperclip size={11} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
                    <span style={{ maxWidth: '160px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {name}
                    </span>
                  </div>
                ))}
              </div>
            )}
            <div style={{ position: 'relative', background: 'var(--c-bg-deep)', borderRadius: '11px', padding: '10px 16px' }}>
              <textarea
                ref={textareaRef}
                value={editText}
                onChange={(e) => setEditText(e.target.value)}
                onInput={handleTextareaInput}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') handleEditCancel()
                }}
                style={{
                  width: '100%',
                  background: 'transparent',
                  border: 'none',
                  outline: 'none',
                  resize: 'none',
                  color: 'var(--c-text-primary)',
                  fontSize: '16px',
                  lineHeight: 1.6,
                  letterSpacing: '-0.64px',
                  fontFamily: 'inherit',
                  minHeight: '28px',
                  overflow: 'hidden',
                }}
              />
            </div>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}>
              <button
                onClick={handleEditCancel}
                style={{
                  padding: '6px 14px',
                  borderRadius: '8px',
                  border: '0.5px solid var(--c-border-subtle)',
                  background: 'transparent',
                  color: 'var(--c-text-primary)',
                  fontSize: '14px',
                  cursor: 'pointer',
                  fontFamily: 'inherit',
                }}
              >
                Cancel
              </button>
              <button
                onClick={handleEditDone}
                disabled={!editText.trim()}
                style={{
                  padding: '6px 14px',
                  borderRadius: '8px',
                  border: 'none',
                  background: editText.trim() ? 'var(--c-text-primary)' : 'var(--c-text-muted)',
                  color: 'var(--c-bg-page)',
                  fontSize: '14px',
                  cursor: editText.trim() ? 'pointer' : 'default',
                  fontFamily: 'inherit',
                  fontWeight: 500,
                }}
              >
                Done
              </button>
            </div>
          </div>
        </div>
      )
    }

    return (
      <div
        style={{ display: 'flex', justifyContent: 'flex-end', alignItems: 'center', gap: '8px' }}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      >
        {/* hover 时左侧操作按钮 */}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: '2px',
            opacity: hovered ? 1 : 0,
            transition: 'opacity 150ms ease',
            pointerEvents: hovered ? 'auto' : 'none',
          }}
        >
          <button
            onClick={handleCopy}
            title="复制"
            style={{
              width: '28px',
              height: '28px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              borderRadius: '7px',
              border: 'none',
              background: 'transparent',
              color: 'var(--c-text-secondary)',
              cursor: 'pointer',
              transition: 'background 150ms',
            }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-deep)' }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'transparent' }}
          >
            {copied ? <Check size={15} /> : <Copy size={15} />}
          </button>
          <button
            onClick={handleEditStart}
            title="编辑"
            style={{
              width: '28px',
              height: '28px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              borderRadius: '7px',
              border: 'none',
              background: 'transparent',
              color: 'var(--c-text-secondary)',
              cursor: 'pointer',
              transition: 'background 150ms',
            }}
            onMouseEnter={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'var(--c-bg-deep)' }}
            onMouseLeave={(e) => { (e.currentTarget as HTMLButtonElement).style.background = 'transparent' }}
          >
            <Pencil size={15} />
          </button>
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '8px', maxWidth: '663px' }}>
          {fileNames.length > 0 && (
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px', justifyContent: 'flex-end' }}>
              {fileNames.map((name) => (
                <div
                  key={name}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: '6px',
                    background: 'var(--c-bg-sub)',
                    border: '0.5px solid var(--c-border-subtle)',
                    borderRadius: '8px',
                    padding: '4px 10px',
                    fontSize: '12px',
                    color: 'var(--c-text-secondary)',
                  }}
                >
                  <Paperclip size={11} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
                  <span style={{ maxWidth: '160px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {name}
                  </span>
                </div>
              ))}
            </div>
          )}
          {text && (
            <div
              style={{
                background: 'var(--c-bg-deep)',
                borderRadius: '11px',
                padding: '10px 16px',
                color: 'var(--c-text-primary)',
                fontSize: '16px',
                lineHeight: 1.6,
                letterSpacing: '-0.64px',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
              }}
            >
              {text}
            </div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column' }}>
      <div style={{ maxWidth: '663px' }}>
        <MarkdownRenderer content={message.content} />
        <div style={{ marginTop: '16px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
            <div style={{ position: 'relative' }}>
              <button
                onClick={handleCopy}
                className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-secondary)] opacity-60 transition-[opacity,background] duration-150 hover:bg-[var(--c-bg-deep)] hover:opacity-100 cursor-pointer border-none bg-transparent"
              >
                {copied ? <Check size={15} /> : <Copy size={15} />}
              </button>
              <span
                style={{
                  position: 'absolute',
                  top: '100%',
                  left: '50%',
                  transform: copied
                    ? 'translateX(-50%) translateY(2px)'
                    : 'translateX(-50%) translateY(-2px)',
                  marginTop: '4px',
                  fontSize: '11px',
                  color: 'var(--c-text-tertiary)',
                  background: 'var(--c-bg-deep)',
                  border: '0.5px solid var(--c-border-subtle)',
                  borderRadius: '5px',
                  padding: '2px 6px',
                  whiteSpace: 'nowrap',
                  opacity: copied ? 1 : 0,
                  transition: 'opacity 150ms ease, transform 150ms ease',
                  pointerEvents: 'none',
                  userSelect: 'none',
                  zIndex: 10,
                }}
              >
                已复制
              </span>
            </div>
            <button
              onClick={onRetry}
              disabled={!onRetry}
              className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-[opacity,background] duration-150 border-none bg-transparent"
              style={{ opacity: onRetry ? 0.6 : 0.25, cursor: onRetry ? 'pointer' : 'default' }}
              onMouseEnter={(e) => { if (onRetry) (e.currentTarget as HTMLButtonElement).style.opacity = '1' }}
              onMouseLeave={(e) => { if (onRetry) (e.currentTarget as HTMLButtonElement).style.opacity = '0.6' }}
            >
              <RefreshCw size={15} />
            </button>
            <button
              disabled
              className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-secondary)] border-none bg-transparent"
              style={{ opacity: 0.25, cursor: 'default' }}
            >
              <Share2 size={15} />
            </button>
            <button
              disabled
              className="flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-secondary)] border-none bg-transparent"
              style={{ opacity: 0.25, cursor: 'default' }}
            >
              <Split size={15} />
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

type StreamingBubbleProps = {
  content: string
}

export function StreamingBubble({ content }: StreamingBubbleProps) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column' }}>
      <div style={{ maxWidth: '663px' }}>
        <MarkdownRenderer content={content} disableMath />
      </div>
    </div>
  )
}
