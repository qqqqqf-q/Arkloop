import { useState, useRef, useEffect, useLayoutEffect, type CSSProperties } from 'react'
import { Copy, Check, Pencil, Paperclip } from 'lucide-react'
import type { MessageResponse } from '../../api'
import type { ArtifactRef } from '../../storage'
import { extractLegacyFilesFromContent, isFilePart, isImagePart, isPastedFile, messageAttachmentParts, messageTextContent } from '../../messageContent'
import { useLocale } from '../../contexts/LocaleContext'
import { ImageThumbnailCard } from './ImageThumbnailCard'
import { PastedBubbleCard } from './PastedBubbleCard'
import { ArtifactDownload } from '../ArtifactDownload'
import { MessageDate } from './MessageDate'
import { AutoResizeTextarea } from '@arkloop/shared'
import {
  getUserPromptEnterScale,
  USER_PROMPT_ENTER_BASE_SCALE,
  USER_TEXT_COLLAPSED_HEIGHT,
  USER_TEXT_FADE_HEIGHT,
} from './utils'

type Props = {
  message: MessageResponse
  animateEnter?: boolean
  onEnterAnimationEnd?: () => void
  onRetry?: () => void
  onEdit?: (newContent: string) => void
  accessToken?: string
}

const useIsomorphicLayoutEffect = typeof window === 'undefined' ? useEffect : useLayoutEffect

export function UserMessage({ message, onEdit, accessToken, animateEnter, onEnterAnimationEnd }: Props) {
  const { t } = useLocale()
  const [copied, setCopied] = useState(false)
  const [editing, setEditing] = useState(false)
  const [editText, setEditText] = useState('')
  const [userTextExpanded, setUserTextExpanded] = useState(false)
  const [userTextOverflows, setUserTextOverflows] = useState(false)
  const [enterScale, setEnterScale] = useState(USER_PROMPT_ENTER_BASE_SCALE)
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const userTextRef = useRef<HTMLDivElement>(null)
  const enterBubbleRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!animateEnter || !onEnterAnimationEnd) return
    const el = enterBubbleRef.current
    let cleared = false
    const done = () => {
      if (cleared) return
      cleared = true
      onEnterAnimationEnd()
    }
    const reduced = typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
    const fallbackMs = reduced ? 48 : 2000
    const t = window.setTimeout(done, fallbackMs)
    const onEnd = () => {
      window.clearTimeout(t)
      done()
    }
    el?.addEventListener('animationend', onEnd, { once: true })
    return () => {
      window.clearTimeout(t)
      el?.removeEventListener('animationend', onEnd)
    }
  }, [animateEnter, onEnterAnimationEnd])

  const handleCopy = () => {
    const plainText = messageTextContent(message)
    void navigator.clipboard.writeText(plainText).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }

  const handleEditStart = () => {
    setEditText(messageTextContent(message))
    setEditing(true)
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

  useEffect(() => {
    if (editing && textareaRef.current) {
      const el = textareaRef.current
      el.focus()
      el.setSelectionRange(el.value.length, el.value.length)
    }
  }, [editing])

  useEffect(() => {
    if (!userTextRef.current) return
    const overflows = userTextRef.current.scrollHeight > USER_TEXT_COLLAPSED_HEIGHT + 1
    setUserTextOverflows(overflows)
    if (!overflows) setUserTextExpanded(false)
  }, [message.content])

  const legacy = extractLegacyFilesFromContent(message.content)
  const attachmentParts = messageAttachmentParts(message)
  const imageAttachments = attachmentParts.filter(isImagePart)
  const allFileAttachments = attachmentParts.filter(isFilePart)
  const pastedAttachments = allFileAttachments.filter((p) => isPastedFile(p.attachment.filename))
  const fileAttachments = allFileAttachments.filter((p) => !isPastedFile(p.attachment.filename))
  const text = messageTextContent(message)
  const displayText = !accessToken && attachmentParts.length > 0 ? message.content : text
  const fileNames = attachmentParts.length > 0
    ? [...imageAttachments, ...allFileAttachments].map((part) => part.attachment.filename)
    : legacy.fileNames

  useIsomorphicLayoutEffect(() => {
    if (!animateEnter) {
      setEnterScale(USER_PROMPT_ENTER_BASE_SCALE)
      return
    }
    const width = enterBubbleRef.current?.getBoundingClientRect().width ?? 0
    setEnterScale(getUserPromptEnterScale(width))
  }, [animateEnter, displayText])

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
          <div style={{ position: 'relative', background: 'var(--c-bg-deep)', borderRadius: '12px', padding: '10px 16px' }}>
            <AutoResizeTextarea
              ref={textareaRef}
              value={editText}
              onChange={(e) => setEditText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Escape') handleEditCancel()
              }}
              minRows={1}
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
      className="group"
      style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px' }}
    >
      <div
        className="pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 transition-[opacity] duration-[180ms] ease-out"
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'flex-end',
          position: 'sticky',
          top: '6px',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '2px' }}>
        <button
          onClick={handleCopy}
          title={t.copyAction}
          style={{
            width: '32px',
            height: '32px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            borderRadius: '8px',
            border: 'none',
            color: 'var(--c-text-secondary)',
            cursor: 'pointer',
            transition: 'background 60ms',
          }}
          className="hover:bg-[var(--c-bg-deep)]"
        >
          {copied ? <Check size={16} /> : <Copy size={16} />}
        </button>
        <button
          onClick={handleEditStart}
          title={t.editAction}
          style={{
            width: '32px',
            height: '32px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            borderRadius: '8px',
            border: 'none',
            color: 'var(--c-text-secondary)',
            cursor: 'pointer',
            transition: 'background 60ms',
          }}
          className="hover:bg-[var(--c-bg-deep)]"
        >
          <Pencil size={16} />
        </button>
        </div>
        <div style={{ marginTop: '4px', paddingRight: '2px' }}>
          <MessageDate createdAt={message.created_at} />
        </div>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'flex-end', gap: '8px', maxWidth: '663px' }}>
        {(imageAttachments.length > 0 || pastedAttachments.length > 0) && accessToken && (
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '12px', justifyContent: 'flex-end' }}>
            {imageAttachments.map((part) => (
              <ImageThumbnailCard
                key={part.attachment.key}
                artifact={part.attachment as ArtifactRef}
                accessToken={accessToken}
                pathPrefix="/v1/attachments"
              />
            ))}
            {pastedAttachments.map((part) => {
              const fullText = part.extracted_text || ''
              const preview = fullText.split('\n').slice(0, 4).join('\n')
              return (
                <PastedBubbleCard
                  key={part.attachment.key}
                  preview={preview}
                  fullText={fullText}
                  size={part.attachment.size}
                />
              )
            })}
          </div>
        )}
        {fileAttachments.length > 0 && accessToken && (
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px', justifyContent: 'flex-end' }}>
            {fileAttachments.map((part) => (
              <ArtifactDownload
                key={part.attachment.key}
                artifact={part.attachment as ArtifactRef}
                accessToken={accessToken}
                pathPrefix="/v1/attachments"
              />
            ))}
          </div>
        )}
        {((!accessToken && fileNames.length > 0) || (fileAttachments.length === 0 && imageAttachments.length === 0 && pastedAttachments.length === 0 && fileNames.length > 0)) && (
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
        {displayText && (() => {
          const isCollapsed = userTextOverflows && !userTextExpanded
          const fadeMask = `linear-gradient(to bottom, black calc(100% - ${USER_TEXT_FADE_HEIGHT}px), transparent)`
          return (
            <div
              ref={enterBubbleRef}
              className={[animateEnter ? 'user-prompt-bubble-enter' : '', 'user-prompt-bubble'].filter(Boolean).join(' ')}
              style={{
                '--user-prompt-enter-scale': String(enterScale),
                borderRadius: '12px',
                padding: '10px 16px',
                fontSize: '16.5px',
                fontWeight: 300,
                lineHeight: 1.6,
                letterSpacing: '-0.64px',
                wordBreak: 'break-word',
              } as CSSProperties}
            >
              <div
                ref={userTextRef}
                style={{
                  maxHeight: !userTextExpanded ? `${USER_TEXT_COLLAPSED_HEIGHT}px` : undefined,
                  overflow: 'hidden',
                  ...(isCollapsed ? {
                    WebkitMaskImage: fadeMask,
                    maskImage: fadeMask,
                  } : {}),
                }}
              >
                {displayText.split(/(\n{2,})/).map((part, i) =>
                  /^\n{2,}$/.test(part)
                    ? <div key={i} style={{ height: '0.3em' }} />
                    : <span key={i} style={{ whiteSpace: 'pre-wrap' }}>{part}</span>
                )}
              </div>
              {userTextOverflows && (
                <div
                  onClick={() => setUserTextExpanded(prev => !prev)}
                  className="text-[var(--c-text-muted)] hover:text-[var(--c-text-icon)]"
                  style={{
                    marginTop: '6px',
                    fontSize: '13px',
                    fontWeight: 300,
                    cursor: 'pointer',
                    userSelect: 'none',
                    transition: 'color 150ms',
                  }}
                >
                  {userTextExpanded ? 'Show less' : 'Show more'}
                </div>
              )}
            </div>
          )
        })()}
      </div>
    </div>
  )
}
