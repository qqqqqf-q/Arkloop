import { useState, type FormEvent } from 'react'
import { useNavigate, useOutletContext } from 'react-router-dom'
import { Glasses } from 'lucide-react'
import { ChatInput } from './ChatInput'
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
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<AppError | null>(null)
  const navigate = useNavigate()

  const handleSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const content = draft.trim()
    if (!content || sending) return

    setSending(true)
    setError(null)

    try {
      const title = deriveTitle(content)
      const thread = await createThread(accessToken, { title })
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
        <button className="flex h-5 w-5 items-center justify-center text-[#c2c0b6] transition-opacity hover:opacity-70">
          <Glasses size={20} />
        </button>
      </div>

      {/* 居中内容 */}
      <div className="flex flex-1 flex-col items-center justify-center px-5" style={{ marginTop: '-100px' }}>
        <h2 className="mb-[60px] text-[40px] font-normal tracking-[-0.5px] text-[#e8e8e3]">
          Arkloop Team
        </h2>

        <div className="w-full max-w-[680px]">
          <ChatInput
            value={draft}
            onChange={setDraft}
            onSubmit={handleSubmit}
            placeholder="How can I help you today?"
            disabled={sending}
            isStreaming={false}
            variant="welcome"
          />

          {error && <ErrorCallout error={error} />}
        </div>
      </div>
    </div>
  )
}
