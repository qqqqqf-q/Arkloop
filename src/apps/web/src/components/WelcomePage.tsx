import { useState, useCallback, type FormEvent } from 'react'
import { useNavigate, useOutletContext } from 'react-router-dom'
import { Glasses, Paperclip, X } from 'lucide-react'
import { ChatInput, type Attachment, formatFileSize } from './ChatInput'
import { ErrorCallout, type AppError } from './ErrorCallout'
import { createThread, createMessage, createRun, isApiError, type ThreadResponse } from '../api'
import { writeActiveThreadIdToStorage } from '../storage'

function normalizeError(error: unknown): AppError {
  if (isApiError(error)) {
    return { message: error.message, traceId: error.traceId, code: error.code }
  }
  if (error instanceof Error) {
    return { message: error.message }
  }
  return { message: '请求失败' }
}

function deriveTitle(content: string): string {
  const cleaned = content.trim().replace(/\s+/g, ' ')
  if (!cleaned) return '新会话'
  return cleaned.length > 40 ? `${cleaned.slice(0, 40)}…` : cleaned
}

type OutletContext = {
  accessToken: string
  onLoggedOut: () => void
  onThreadCreated: (thread: ThreadResponse) => void
}

export function WelcomePage() {
  const { accessToken, onLoggedOut, onThreadCreated } = useOutletContext<OutletContext>()
  const [draft, setDraft] = useState('')
  const [attachments, setAttachments] = useState<Attachment[]>([])
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<AppError | null>(null)
  const navigate = useNavigate()

  const handleAttachFiles = useCallback((files: File[]) => {
    const readers = files.map((file) => {
      return new Promise<Attachment>((resolve, reject) => {
        const isText = file.type.startsWith('text/') || file.type === ''
        const reader = new FileReader()
        reader.onload = () => {
          resolve({
            id: `${file.name}-${file.size}-${Date.now()}`,
            name: file.name,
            size: file.size,
            content: reader.result as string,
            encoding: isText ? 'text' : 'base64',
          })
        }
        reader.onerror = () => reject(reader.error ?? new Error(`读取失败: ${file.name}`))
        if (isText) {
          reader.readAsText(file)
        } else {
          reader.readAsDataURL(file)
        }
      })
    })
    void Promise.allSettled(readers).then((results) => {
      const newAttachments = results
        .filter((r): r is PromiseFulfilledResult<Attachment> => r.status === 'fulfilled')
        .map((r) => r.value)
      if (newAttachments.length === 0) return
      setAttachments((prev) => {
        const existingNames = new Set(prev.map((a) => a.name))
        const deduped = newAttachments.filter((a) => !existingNames.has(a.name))
        return [...prev, ...deduped]
      })
    })
  }, [])

  const handleRemoveAttachment = useCallback((id: string) => {
    setAttachments((prev) => prev.filter((a) => a.id !== id))
  }, [])

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const text = draft.trim()
    if ((!text && attachments.length === 0) || sending) return

    setSending(true)
    setError(null)

    try {
      const title = deriveTitle(text)
      const thread = await createThread(accessToken, { title })

      const fileParts = attachments.map(
        (a) => `<file name="${a.name}" encoding="${a.encoding}">\n${a.content}\n</file>`,
      )
      const content = fileParts.length > 0
        ? `${fileParts.join('\n\n')}${text ? `\n\n${text}` : ''}`
        : text

      await createMessage(accessToken, thread.id, { content })
      const run = await createRun(accessToken, thread.id)

      writeActiveThreadIdToStorage(thread.id)
      onThreadCreated(thread)
      navigate(`/t/${thread.id}`, { state: { initialRunId: run.run_id } })
    } catch (err) {
      if (isApiError(err) && err.status === 401) {
        onLoggedOut()
        return
      }
      setError(normalizeError(err))
      setSending(false)
    }
  }

  return (
    <div className="flex h-full flex-col">
      {/* 顶部 header */}
      <div className="flex min-h-[51px] items-center justify-end px-[15px] py-[15px]">
        <button className="flex h-5 w-5 items-center justify-center text-[var(--c-text-secondary)] transition-opacity hover:opacity-70">
          <Glasses size={20} />
        </button>
      </div>

      {/* 居中内容 */}
      <div className="flex flex-1 flex-col items-center justify-center px-5" style={{ marginTop: '-100px' }}>
        <h2 className="mb-[60px] text-[40px] font-normal tracking-[-0.5px] text-[var(--c-text-heading)]">
          Arkloop Team
        </h2>

        <div className="w-full max-w-[680px]">
          {attachments.length > 0 && (
            <div className="mb-2 flex flex-wrap gap-2">
              {attachments.map((att) => (
                <div
                  key={att.id}
                  className="flex items-center gap-1.5 rounded-lg px-2.5 py-1.5"
                  style={{ background: 'var(--c-bg-sub)', border: '0.5px solid var(--c-border-subtle)' }}
                >
                  <Paperclip size={12} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
                  <span
                    className="text-xs"
                    style={{ color: 'var(--c-text-secondary)', maxWidth: '160px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                  >
                    {att.name}
                  </span>
                  <span className="text-xs" style={{ color: 'var(--c-text-muted)', flexShrink: 0 }}>
                    {formatFileSize(att.size)}
                  </span>
                  <button
                    type="button"
                    onClick={() => handleRemoveAttachment(att.id)}
                    className="flex items-center justify-center rounded transition-opacity duration-100 hover:opacity-100"
                    style={{ color: 'var(--c-text-muted)', opacity: 0.7, marginLeft: '2px' }}
                  >
                    <X size={12} />
                  </button>
                </div>
              ))}
            </div>
          )}
          <ChatInput
            value={draft}
            onChange={setDraft}
            onSubmit={handleSubmit}
            placeholder="How can I help you today?"
            disabled={sending}
            isStreaming={false}
            variant="welcome"
            attachments={attachments}
            onAttachFiles={handleAttachFiles}
          />

          {error && <ErrorCallout error={error} />}
        </div>
      </div>
    </div>
  )
}
