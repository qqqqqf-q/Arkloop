import arkloopMark from './assets/arkloop.svg'
import {
  createMessage,
  createRun,
  createThread,
  getMe,
  isApiError,
  login,
  register,
  type CreateRunResponse,
  type MeResponse,
  type ThreadResponse,
} from './api'
import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { useSSE } from './hooks/useSSE'
import { RunEventsPanel } from './components/RunEventsPanel'

type AppError = {
  message: string
  traceId?: string
  code?: string
}

type RunDemoSession = {
  thread: ThreadResponse
  run: CreateRunResponse
  messageContent: string
}

const RUN_DEMO_SESSION_KEY = 'arkloop:web:run_demo_session'

function readRunDemoSession(): RunDemoSession | null {
  try {
    const raw = localStorage.getItem(RUN_DEMO_SESSION_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as unknown
    if (!parsed || typeof parsed !== 'object') return null

    const obj = parsed as Partial<RunDemoSession>
    if (!obj.run || typeof obj.run.run_id !== 'string' || obj.run.run_id.length === 0) return null
    if (!obj.thread || typeof obj.thread.id !== 'string' || obj.thread.id.length === 0) return null
    if (typeof obj.messageContent !== 'string') return null

    return obj as RunDemoSession
  } catch {
    return null
  }
}

function writeRunDemoSession(session: RunDemoSession): void {
  try {
    localStorage.setItem(RUN_DEMO_SESSION_KEY, JSON.stringify(session))
  } catch {
    // 忽略存储失败
  }
}

function clearRunDemoSession(): void {
  try {
    localStorage.removeItem(RUN_DEMO_SESSION_KEY)
  } catch {
    // 忽略存储失败
  }
}

function normalizeError(error: unknown): AppError {
  if (isApiError(error)) {
    return {
      message: error.message,
      traceId: error.traceId,
      code: error.code,
    }
  }
  if (error instanceof Error) {
    return { message: error.message }
  }
  return { message: '请求失败' }
}

function ErrorCallout({ error }: { error: AppError }) {
  return (
    <div className="mt-4 rounded-lg border border-rose-900/40 bg-rose-950/40 px-4 py-3 text-sm">
      <div className="font-medium text-rose-200">请求失败</div>
      <div className="mt-1 text-rose-100/90">{error.message}</div>
      <div className="mt-2 space-y-1 text-rose-100/80">
        {error.code ? <div>code: {error.code}</div> : null}
        {error.traceId ? <div>trace_id: {error.traceId}</div> : null}
      </div>
    </div>
  )
}

function AuthCard({
  onLoggedIn,
}: {
  onLoggedIn: (accessToken: string) => void
}) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [loginValue, setLoginValue] = useState('')
  const [password, setPassword] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<AppError | null>(null)

  const canSubmit = useMemo(() => {
    if (submitting) return false
    if (loginValue.trim().length === 0) return false
    if (password.length === 0) return false
    if (mode === 'register' && displayName.trim().length === 0) return false
    if (mode === 'register' && password.length < 8) return false
    return true
  }, [loginValue, password, displayName, submitting, mode])

  const onSubmit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (!canSubmit) return
    setSubmitting(true)
    setError(null)
    try {
      if (mode === 'login') {
        const resp = await login({ login: loginValue, password })
        onLoggedIn(resp.access_token)
      } else {
        const resp = await register({
          login: loginValue,
          password,
          display_name: displayName,
        })
        onLoggedIn(resp.access_token)
      }
    } catch (err) {
      setError(normalizeError(err))
    } finally {
      setSubmitting(false)
    }
  }

  const switchMode = () => {
    setMode(mode === 'login' ? 'register' : 'login')
    setError(null)
  }

  return (
    <div className="rounded-2xl border border-slate-800 bg-slate-900/40 p-6 shadow-sm">
      <div className="flex items-center justify-between">
        <h2 className="text-base font-semibold text-slate-100">
          {mode === 'login' ? '登录' : '注册'}
        </h2>
        <button
          className="text-sm text-indigo-400 hover:text-indigo-300"
          onClick={switchMode}
          type="button"
        >
          {mode === 'login' ? '没有账号？注册' : '已有账号？登录'}
        </button>
      </div>

      <form className="mt-6 space-y-4" onSubmit={onSubmit}>
        {mode === 'register' && (
          <label className="block">
            <div className="text-sm text-slate-300">显示名称</div>
            <input
              className="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950/40 px-3 py-2 text-slate-100 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/50"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              placeholder="输入显示名称"
              autoComplete="name"
            />
          </label>
        )}

        <label className="block">
          <div className="text-sm text-slate-300">登录名</div>
          <input
            className="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950/40 px-3 py-2 text-slate-100 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/50"
            value={loginValue}
            onChange={(e) => setLoginValue(e.target.value)}
            placeholder="输入登录名"
            autoComplete="username"
          />
        </label>

        <label className="block">
          <div className="text-sm text-slate-300">
            密码{mode === 'register' && <span className="text-slate-500">（至少8位）</span>}
          </div>
          <input
            className="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950/40 px-3 py-2 text-slate-100 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/50"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            type="password"
            placeholder="输入密码"
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
          />
        </label>

        <button
          className="inline-flex w-full items-center justify-center rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
          type="submit"
          disabled={!canSubmit}
        >
          {submitting
            ? mode === 'login'
              ? '登录中...'
              : '注册中...'
            : mode === 'login'
              ? '登录'
              : '注册'}
        </button>
      </form>

      {error ? <ErrorCallout error={error} /> : null}
    </div>
  )
}

function MeCard({
  accessToken,
  onLogout,
}: {
  accessToken: string
  onLogout: () => void
}) {
  const [me, setMe] = useState<MeResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<AppError | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const resp = await getMe(accessToken)
      setMe(resp)
    } catch (err) {
      setError(normalizeError(err))
    } finally {
      setLoading(false)
    }
  }, [accessToken])

  useEffect(() => {
    void refresh()
  }, [refresh])

  return (
    <div className="rounded-2xl border border-slate-800 bg-slate-900/40 p-6 shadow-sm">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-base font-semibold text-slate-100">当前用户</h2>
        </div>
        <button
          className="rounded-lg border border-slate-700 bg-slate-950/40 px-3 py-2 text-sm text-slate-200 hover:bg-slate-950/60 disabled:cursor-not-allowed disabled:opacity-50"
          onClick={onLogout}
          type="button"
        >
          退出登录
        </button>
      </div>

      <div className="mt-6">
        <div className="flex items-center gap-3">
          <button
            className="rounded-lg bg-slate-200 px-3 py-2 text-sm font-medium text-slate-950 hover:bg-white disabled:cursor-not-allowed disabled:opacity-50"
            onClick={refresh}
            type="button"
            disabled={loading}
          >
            {loading ? '刷新中...' : '刷新'}
          </button>
        </div>

        {me ? (
          <div className="mt-4 rounded-lg border border-slate-800 bg-slate-950/30 px-4 py-3 text-sm">
            <div className="text-slate-300">id</div>
            <div className="mt-1 font-mono text-slate-100">{me.id}</div>
            <div className="mt-4 text-slate-300">display_name</div>
            <div className="mt-1 text-slate-100">{me.display_name}</div>
          </div>
        ) : null}

        {error ? <ErrorCallout error={error} /> : null}
      </div>
    </div>
  )
}

function RunDemoCard({ accessToken }: { accessToken: string }) {
  const [thread, setThread] = useState<ThreadResponse | null>(null)
  const [run, setRun] = useState<CreateRunResponse | null>(null)
  const [messageContent, setMessageContent] = useState('Hello, Arkloop!')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<AppError | null>(null)

  const baseUrl = (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? ''

  const sse = useSSE({
    runId: run?.run_id ?? '',
    accessToken,
    baseUrl,
  })

  // 刷新后恢复上一次的 run（用于 after_seq 续传）
  useEffect(() => {
    if (run || thread) return
    const session = readRunDemoSession()
    if (!session) return
    setThread(session.thread)
    setRun(session.run)
    setMessageContent(session.messageContent)
  }, [])

  // 当 run 创建后自动连接 SSE
  useEffect(() => {
    if (run?.run_id) {
      sse.clearEvents()
      sse.connect()
    }
  }, [run?.run_id]) // 故意不依赖 sse 避免循环

  const handleCreateRun = async () => {
    setLoading(true)
    setError(null)

    try {
      // 创建线程
      const threadResp = await createThread(accessToken)
      setThread(threadResp)

      // 创建消息
      await createMessage(accessToken, threadResp.id, { content: messageContent })

      // 创建运行
      const runResp = await createRun(accessToken, threadResp.id)
      setRun(runResp)
      writeRunDemoSession({ thread: threadResp, run: runResp, messageContent })
    } catch (err) {
      setError(normalizeError(err))
    } finally {
      setLoading(false)
    }
  }

  const handleReset = () => {
    sse.disconnect()
    sse.reset()
    clearRunDemoSession()
    setThread(null)
    setRun(null)
    setError(null)
  }

  return (
    <div className="space-y-6">
      <div className="rounded-2xl border border-slate-800 bg-slate-900/40 p-6 shadow-sm">
        <h2 className="text-base font-semibold text-slate-100">SSE 演示</h2>

        <div className="mt-4 space-y-4">
          <label className="block">
            <div className="text-sm text-slate-300">消息内容</div>
            <input
              className="mt-1 w-full rounded-lg border border-slate-700 bg-slate-950/40 px-3 py-2 text-slate-100 placeholder:text-slate-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/50"
              value={messageContent}
              onChange={(e) => setMessageContent(e.target.value)}
              placeholder="输入消息"
              disabled={!!run}
            />
          </label>

          <div className="flex items-center gap-3">
            {!run ? (
              <button
                className="rounded-lg bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
                onClick={handleCreateRun}
                disabled={loading || !messageContent.trim()}
                type="button"
              >
                {loading ? '创建中...' : '创建 Run'}
              </button>
            ) : (
              <button
                className="rounded-lg border border-slate-700 bg-slate-950/40 px-4 py-2 text-sm text-slate-200 hover:bg-slate-950/60"
                onClick={handleReset}
                type="button"
              >
                重置
              </button>
            )}
          </div>

          {thread && (
            <div className="rounded-lg border border-slate-800 bg-slate-950/30 px-4 py-3 text-sm">
              <div className="text-slate-300">thread_id</div>
              <div className="mt-1 font-mono text-xs text-slate-100">{thread.id}</div>
            </div>
          )}

          {run && (
            <div className="rounded-lg border border-slate-800 bg-slate-950/30 px-4 py-3 text-sm">
              <div className="text-slate-300">run_id</div>
              <div className="mt-1 font-mono text-xs text-slate-100">{run.run_id}</div>
              <div className="mt-2 text-slate-300">trace_id</div>
              <div className="mt-1 font-mono text-xs text-slate-100">{run.trace_id}</div>
            </div>
          )}

          {error ? <ErrorCallout error={error} /> : null}
        </div>
      </div>

      {run && (
        <RunEventsPanel
          events={sse.events}
          state={sse.state}
          lastSeq={sse.lastSeq}
          error={sse.error}
          onReconnect={sse.reconnect}
          onClear={sse.clearEvents}
        />
      )}
    </div>
  )
}

function App() {
  const [accessToken, setAccessToken] = useState<string | null>(null)

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100">
      <main className="mx-auto max-w-3xl px-6 py-16">
        <div className="flex items-center gap-4">
          <img src={arkloopMark} alt="Arkloop" className="h-10 w-10" />
          <h1 className="text-4xl font-semibold tracking-tight">Arkloop Web</h1>
        </div>

        <div className="mt-10 space-y-6">
          {accessToken ? (
            <>
              <MeCard
                accessToken={accessToken}
                onLogout={() => setAccessToken(null)}
              />
              <RunDemoCard accessToken={accessToken} />
            </>
          ) : (
            <AuthCard onLoggedIn={(t) => setAccessToken(t)} />
          )}
        </div>
      </main>
    </div>
  )
}

export default App
