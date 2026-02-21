import { useState } from 'react'
import { Copy, Check, RefreshCw, Share2, Split, Paperclip } from 'lucide-react'
import type { MessageResponse } from '../api'
import { MarkdownRenderer } from './MarkdownRenderer'

type Props = {
  message: MessageResponse
  onRetry?: () => void
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

export function MessageBubble({ message, onRetry }: Props) {
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    const { text } = extractFilesFromContent(message.content)
    const plainText = message.role === 'user' ? text : message.content
    void navigator.clipboard.writeText(plainText).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }

  if (message.role === 'user') {
    const { text, fileNames } = extractFilesFromContent(message.content)
    return (
      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
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
