import { useState, type ReactNode } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import type { RunEventRaw } from '../api/runs'

export type LlmTurn = {
  llmCallId: string
  providerKind: string
  apiMode: string
  inputTokens?: number
  outputTokens?: number
  // 仅含该轮新增的用户输入（Turn 1 = user prompt；后续轮 = tool result 内容）
  userInput?: string
  assistantText: string
  toolCalls: Array<{
    toolCallId: string
    toolName: string
    argsJSON: Record<string, unknown>
    resultJSON?: Record<string, unknown>
    errorClass?: string
  }>
  model?: string
  systemPrompt?: string
  toolCount?: number
  toolNames?: string[]
  messageCount?: number
  temperature?: number
  maxOutputTokens?: number
}

/** 从 run_events 数组重建轮次列表 */
export function buildTurns(events: RunEventRaw[]): LlmTurn[] {
  return buildTurnsFromLlmRequests(events)
}

/** 从 llm.request 事件精确重建轮次 */
function buildTurnsFromLlmRequests(events: RunEventRaw[]): LlmTurn[] {
  const turns: LlmTurn[] = []
  let current: LlmTurn | null = null
  const assistantChunks: string[] = []
  const resultMap: Record<string, { resultJSON?: Record<string, unknown>; errorClass?: string }> = {}

  const extractToolName = (tool: Record<string, unknown>): string => {
    if (typeof tool.name === 'string') return tool.name
    const fn = tool.function
    if (fn && typeof fn === 'object') {
      const name = (fn as Record<string, unknown>).name
      if (typeof name === 'string') return name
    }
    return ''
  }

  for (const ev of events) {
    if (ev.type === 'llm.request') {
      if (current) {
        current.assistantText = assistantChunks.join('')
        assistantChunks.length = 0
      }
      const d = ev.data as Record<string, unknown>
      const payload = d.payload as Record<string, unknown> | undefined
      const messages = Array.isArray(payload?.messages) ? (payload.messages as Array<Record<string, unknown>>) : []
      let userInput: string | undefined
      for (let i = messages.length - 1; i >= 0; i--) {
        const msg = messages[i]
        if (msg.role === 'user' || msg.role === 'tool') {
          userInput = extractMessageText(msg)
          break
        }
      }

      // 提取 LLM 请求元数据
      const systemMsg = messages.find((m) => m.role === 'system')
      const systemPrompt = systemMsg ? extractMessageText(systemMsg) : undefined
      const tools = Array.isArray(payload?.tools) ? (payload.tools as Array<Record<string, unknown>>) : []
      const toolNames = tools.map(extractToolName).filter(Boolean)

      current = {
        llmCallId: String(d.llm_call_id ?? ''),
        providerKind: String(d.provider_kind ?? ''),
        apiMode: String(d.api_mode ?? ''),
        userInput,
        assistantText: '',
        toolCalls: [],
        model: payload?.model != null ? String(payload.model) : undefined,
        systemPrompt,
        toolCount: tools.length > 0 ? tools.length : undefined,
        toolNames: toolNames.length > 0 ? toolNames : undefined,
        messageCount: messages.length > 0 ? messages.length : undefined,
        temperature: typeof payload?.temperature === 'number' ? payload.temperature : undefined,
        maxOutputTokens: typeof payload?.max_tokens === 'number'
          ? payload.max_tokens
          : typeof payload?.max_output_tokens === 'number'
            ? payload.max_output_tokens
            : undefined,
      }
      turns.push(current)
    } else if (ev.type === 'message.delta' && current) {
      const d = ev.data as Record<string, unknown>
      assistantChunks.push(String(d.content_delta ?? ''))
    } else if (ev.type === 'tool.call' && current) {
      const d = ev.data as Record<string, unknown>
      current.toolCalls.push({
        toolCallId: String(d.tool_call_id ?? ''),
        toolName: String(d.tool_name ?? ev.tool_name ?? ''),
        argsJSON: (d.arguments as Record<string, unknown>) ?? {},
      })
    } else if (ev.type === 'tool.result') {
      const d = ev.data as Record<string, unknown>
      resultMap[String(d.tool_call_id ?? '')] = {
        resultJSON: d.result as Record<string, unknown> | undefined,
        errorClass: ev.error_class,
      }
    } else if (ev.type === 'run.completed' || ev.type === 'run.failed') {
      if (current) {
        current.assistantText = assistantChunks.join('')
        assistantChunks.length = 0
        const usage = (ev.data as Record<string, unknown>).usage as Record<string, unknown> | undefined
        if (usage) {
          current.inputTokens = usage.input_tokens as number | undefined
          current.outputTokens = usage.output_tokens as number | undefined
        }
      }
      current = null
    }
  }

  if (current && assistantChunks.length > 0) {
    current.assistantText = assistantChunks.join('')
  }

  mergeTurnResults(turns, resultMap)
  return turns
}

function mergeTurnResults(
  turns: LlmTurn[],
  resultMap: Record<string, { resultJSON?: Record<string, unknown>; errorClass?: string }>,
) {
  for (const turn of turns) {
    for (const tc of turn.toolCalls) {
      const r = resultMap[tc.toolCallId]
      if (r) {
        tc.resultJSON = r.resultJSON
        tc.errorClass = r.errorClass
      }
    }
  }
}

function extractMessageText(msg: Record<string, unknown>): string {
  const content = msg.content
  if (typeof content === 'string') return content
  if (Array.isArray(content)) {
    return content
      .map((part: unknown) => {
        if (typeof part === 'string') return part
        if (typeof part === 'object' && part !== null) {
          const p = part as Record<string, unknown>
          return typeof p.text === 'string' ? p.text : JSON.stringify(p)
        }
        return ''
      })
      .join('')
  }
  return JSON.stringify(content)
}

// ---- 展示组件 ----

type CollapseBlockProps = {
  label: string
  preview?: string
  defaultOpen?: boolean
  children: ReactNode
  dim?: boolean
}

function CollapseBlock({ label, preview, defaultOpen = false, children, dim }: CollapseBlockProps) {
  const [open, setOpen] = useState(defaultOpen)

  return (
    <div className="rounded border border-[var(--c-border)] overflow-hidden">
      <button
        onClick={() => setOpen((v) => !v)}
        className={[
          'flex w-full items-start gap-2 px-3 py-2 text-left transition-colors hover:bg-[var(--c-bg-sub)]',
          dim ? 'opacity-60' : '',
        ].join(' ')}
      >
        <span className="mt-0.5 shrink-0 text-[var(--c-text-muted)]">
          {open ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
        </span>
        <span className="text-xs font-medium text-[var(--c-text-secondary)]">{label}</span>
        {!open && preview && (
          <span className="ml-1 truncate text-xs text-[var(--c-text-muted)]">{preview}</span>
        )}
      </button>
      {open && (
        <div className="border-t border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-3 py-2">
          {children}
        </div>
      )}
    </div>
  )
}

function PreText({ text }: { text: string }) {
  return (
    <pre className="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-[var(--c-text-secondary)]">
      {text}
    </pre>
  )
}

function JsonBlock({ value }: { value: unknown }) {
  return (
    <pre className="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-[var(--c-text-secondary)]">
      {JSON.stringify(value, null, 2)}
    </pre>
  )
}

type TurnViewProps = {
  turn: LlmTurn
  index: number
}

export function TurnView({ turn, index }: TurnViewProps) {
  const tokenLabel =
    turn.inputTokens != null && turn.outputTokens != null
      ? `${turn.inputTokens}in / ${turn.outputTokens}out`
      : ''

  return (
    <div className="space-y-1.5 rounded-lg border border-[var(--c-border)] p-3">
      {/* 轮次头部 */}
      <div className="mb-2 flex items-center gap-2 text-xs text-[var(--c-text-muted)]">
        <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 font-mono font-medium text-[var(--c-text-secondary)]">
          Turn {index + 1}
        </span>
        {turn.model && <span className="font-medium text-[var(--c-text-secondary)]">{turn.model}</span>}
        <span>{turn.providerKind}</span>
        {turn.apiMode && <span className="opacity-60">· {turn.apiMode}</span>}
        {tokenLabel && (
          <span className="ml-auto tabular-nums">{tokenLabel}</span>
        )}
      </div>

      {/* 元数据标签行 */}
      <div className="mb-2 flex flex-wrap items-center gap-1.5">
        {turn.toolCount != null && (
          <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[11px] tabular-nums text-[var(--c-text-muted)]">
            {turn.toolCount} tools
          </span>
        )}
        {turn.messageCount != null && (
          <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[11px] tabular-nums text-[var(--c-text-muted)]">
            {turn.messageCount} msgs
          </span>
        )}
        {turn.temperature != null && (
          <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[11px] tabular-nums text-[var(--c-text-muted)]">
            temp {turn.temperature}
          </span>
        )}
        {turn.maxOutputTokens != null && (
          <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[11px] tabular-nums text-[var(--c-text-muted)]">
            max {turn.maxOutputTokens}
          </span>
        )}
      </div>

      {/* System Prompt */}
      {turn.systemPrompt && (
        <CollapseBlock
          label="System"
          preview={turn.systemPrompt.slice(0, 80) + (turn.systemPrompt.length > 80 ? '...' : '')}
        >
          <PreText text={turn.systemPrompt} />
        </CollapseBlock>
      )}

      {/* Tools 列表 */}
      {turn.toolNames && turn.toolNames.length > 0 && (
        <CollapseBlock
          label={`Tools (${turn.toolNames.length})`}
          preview={turn.toolNames.slice(0, 5).join(', ') + (turn.toolNames.length > 5 ? ', ...' : '')}
        >
          <div className="flex flex-wrap gap-1">
            {turn.toolNames.map((name) => (
              <span key={name} className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--c-text-secondary)]">
                {name}
              </span>
            ))}
          </div>
        </CollapseBlock>
      )}

      {/* 本轮输入（折叠，预览截断 80 字） */}
      {turn.userInput != null && (
        <CollapseBlock
          label="Input"
          preview={turn.userInput.slice(0, 80) + (turn.userInput.length > 80 ? '...' : '')}
        >
          <PreText text={turn.userInput} />
        </CollapseBlock>
      )}

      {/* Tool calls */}
      {turn.toolCalls.map((tc, i) => {
        const isBrowser = tc.toolName === 'browser'
        const browserCommand = isBrowser && typeof tc.argsJSON?.command === 'string' ? tc.argsJSON.command : null
        const hasScreenshot = isBrowser && tc.resultJSON?.has_screenshot === true
        const artifactCount = isBrowser && Array.isArray(tc.resultJSON?.artifacts) ? (tc.resultJSON.artifacts as unknown[]).length : 0
        return (
        <div key={tc.toolCallId || i} className="space-y-1">
          <CollapseBlock
            label={isBrowser ? `browser  ${browserCommand ?? ''}` : `tool.call  ${tc.toolName}`}
            preview={isBrowser ? undefined : JSON.stringify(tc.argsJSON).slice(0, 60)}
          >
            <JsonBlock value={tc.argsJSON} />
          </CollapseBlock>
          {(tc.resultJSON != null || tc.errorClass) && (
            <CollapseBlock
              label={
                tc.errorClass
                  ? `tool.result  error`
                  : hasScreenshot
                    ? `tool.result  screenshot`
                    : `tool.result`
              }
              preview={
                tc.errorClass
                  ? tc.errorClass
                  : hasScreenshot
                    ? `${artifactCount} artifact(s)`
                    : JSON.stringify(tc.resultJSON).slice(0, 60)
              }
              dim={!!tc.errorClass}
            >
              {tc.errorClass ? (
                <span className="text-xs text-red-500">{tc.errorClass}</span>
              ) : (
                <JsonBlock value={tc.resultJSON} />
              )}
            </CollapseBlock>
          )}
        </div>
        )
      })}

      {/* Assistant 回复（折叠，预览截断 80 字） */}
      {turn.assistantText && (
        <CollapseBlock
          label="Assistant"
          preview={turn.assistantText.slice(0, 80) + (turn.assistantText.length > 80 ? '...' : '')}
        >
          <PreText text={turn.assistantText} />
        </CollapseBlock>
      )}
    </div>
  )
}
