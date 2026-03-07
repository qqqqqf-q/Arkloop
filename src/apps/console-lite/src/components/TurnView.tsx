import { useState, type ReactNode } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import type { LlmTurn } from '../run-turns'
import { useLocale } from '../contexts/LocaleContext'

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
    <div className="overflow-hidden rounded border border-[var(--c-border)]">
      <button
        onClick={() => setOpen((value) => !value)}
        className={[
          'flex w-full items-start gap-1.5 px-2.5 py-1.5 text-left transition-colors hover:bg-[var(--c-bg-sub)]',
          dim ? 'opacity-60' : '',
        ].join(' ')}
      >
        <span className="mt-0.5 shrink-0 text-[var(--c-text-muted)]">
          {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        </span>
        <span className="text-[11px] font-medium text-[var(--c-text-secondary)]">{label}</span>
        {!open && preview && (
          <span className="ml-1 truncate text-[11px] text-[var(--c-text-muted)]">{preview}</span>
        )}
      </button>
      {open && (
        <div className="border-t border-[var(--c-border)] bg-[var(--c-bg-deep2)] px-2.5 py-2">
          {children}
        </div>
      )}
    </div>
  )
}

function PreText({ text }: { text: string }) {
  return (
    <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-[var(--c-text-secondary)]">
      {text}
    </pre>
  )
}

function JsonBlock({ value }: { value: unknown }) {
  return (
    <pre className="whitespace-pre-wrap break-words font-mono text-[11px] leading-relaxed text-[var(--c-text-secondary)]">
      {JSON.stringify(value, null, 2)}
    </pre>
  )
}

function previewText(text: string): string {
  return text.slice(0, 80) + (text.length > 80 ? '...' : '')
}

type TurnViewProps = {
  turn: LlmTurn
  index: number
}

export function TurnView({ turn, index }: TurnViewProps) {
  const { t } = useLocale()
  const tr = t.runs
  const tokenLabel =
    turn.inputTokens != null && turn.outputTokens != null
      ? `${turn.inputTokens} / ${turn.outputTokens}`
      : ''

  return (
    <div className="space-y-1.5 rounded-lg border border-[var(--c-border)] p-2.5">
      <div className="mb-1.5 flex items-center gap-1.5 text-[11px] text-[var(--c-text-muted)]">
        <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 font-mono font-medium text-[var(--c-text-secondary)]">
          {tr.turn} {index + 1}
        </span>
        {turn.model && <span className="font-medium text-[var(--c-text-secondary)]">{turn.model}</span>}
        <span>{turn.providerKind}</span>
        {turn.apiMode && <span className="opacity-60">· {turn.apiMode}</span>}
        {tokenLabel && <span className="ml-auto tabular-nums">{tokenLabel}</span>}
      </div>

      <div className="mb-1.5 flex flex-wrap items-center gap-1">
        {turn.toolCount != null && (
          <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[10px] tabular-nums text-[var(--c-text-muted)]">
            {turn.toolCount} {tr.toolsLabel}
          </span>
        )}
        {turn.messageCount != null && (
          <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[10px] tabular-nums text-[var(--c-text-muted)]">
            {turn.messageCount} msgs
          </span>
        )}
        {turn.temperature != null && (
          <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[10px] tabular-nums text-[var(--c-text-muted)]">
            temp {turn.temperature}
          </span>
        )}
        {turn.maxOutputTokens != null && (
          <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[10px] tabular-nums text-[var(--c-text-muted)]">
            max {turn.maxOutputTokens}
          </span>
        )}
      </div>

      {turn.systemPrompt && (
        <CollapseBlock label={tr.system} preview={previewText(turn.systemPrompt)}>
          <PreText text={turn.systemPrompt} />
        </CollapseBlock>
      )}

      {turn.toolNames && turn.toolNames.length > 0 && (
        <CollapseBlock
          label={`${tr.toolsLabel} (${turn.toolNames.length})`}
          preview={turn.toolNames.slice(0, 5).join(', ') + (turn.toolNames.length > 5 ? ', ...' : '')}
        >
          <div className="flex flex-wrap gap-1">
            {turn.toolNames.map((name) => (
              <span key={name} className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 font-mono text-[10px] text-[var(--c-text-secondary)]">
                {name}
              </span>
            ))}
          </div>
        </CollapseBlock>
      )}

      {turn.userInput != null && (
        <CollapseBlock label={tr.input} preview={previewText(turn.userInput)}>
          <PreText text={turn.userInput} />
        </CollapseBlock>
      )}

      {turn.toolCalls.map((toolCall, idx) => (
        <div key={toolCall.toolCallId || idx} className="space-y-1">
          <CollapseBlock
            label={`${tr.toolCall} ${toolCall.toolName}`}
            preview={JSON.stringify(toolCall.argsJSON).slice(0, 60)}
          >
            <JsonBlock value={toolCall.argsJSON} />
          </CollapseBlock>
          {(toolCall.resultJSON != null || toolCall.errorClass) && (
            <CollapseBlock
              label={toolCall.errorClass ? `${tr.toolResult} ${tr.error}` : tr.toolResult}
              preview={
                toolCall.errorClass
                  ? toolCall.errorClass
                  : JSON.stringify(toolCall.resultJSON).slice(0, 60)
              }
              dim={Boolean(toolCall.errorClass)}
            >
              {toolCall.errorClass ? (
                <span className="text-[11px] text-[var(--c-status-error-text)]">{toolCall.errorClass}</span>
              ) : (
                <JsonBlock value={toolCall.resultJSON} />
              )}
            </CollapseBlock>
          )}
        </div>
      ))}

      {turn.assistantText && (
        <CollapseBlock label={tr.output} preview={previewText(turn.assistantText)} defaultOpen>
          <PreText text={turn.assistantText} />
        </CollapseBlock>
      )}
    </div>
  )
}
