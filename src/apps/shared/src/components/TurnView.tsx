import type { LlmTurn, TurnSegment } from '../run-turns'
import { CollapseBlock, PreText, JsonBlock } from './TurnViewBlocks'

type TurnViewProps = {
  turn: LlmTurn
  index: number
}

function preview(text: string): string {
  return text.slice(0, 80) + (text.length > 80 ? '...' : '')
}

function previewJSON(value: unknown): string {
  return JSON.stringify(value ?? null).slice(0, 60)
}

function executionSegments(turn: LlmTurn): TurnSegment[] {
  let lastAssistantIndex = -1
  for (let index = turn.segments.length - 1; index >= 0; index--) {
    const segment = turn.segments[index]
    if (segment.kind === 'assistant' && segment.isFinal) {
      lastAssistantIndex = index
      break
    }
  }
  return turn.segments.filter((_, index) => index !== lastAssistantIndex)
}

function executionPreview(segments: TurnSegment[]): string {
  return segments
    .slice(0, 3)
    .map((segment) => {
      if (segment.kind === 'assistant') return `assistant: ${preview(segment.text)}`
      if (segment.kind === 'tool_call') return `tool: ${segment.toolName}`
      return `result: ${segment.toolName || 'tool'}`
    })
    .join(' · ')
}

export function TurnView({ turn, index }: TurnViewProps) {
  const inputMetaChips = [turn.inputMeta?.channel, turn.inputMeta?.['conversation-type'], turn.inputMeta?.['display-name']]
    .filter((value): value is string => !!value)
  const orderedSegments = executionSegments(turn)

  return (
    <div className="space-y-1.5 rounded-lg border border-[var(--c-border)] p-3">
      <div className="mb-2 flex items-center gap-2 text-xs text-[var(--c-text-muted)]">
        <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 font-mono font-medium text-[var(--c-text-secondary)]">
          Turn {index + 1}
        </span>
        {turn.model && <span className="font-medium text-[var(--c-text-secondary)]">{turn.model}</span>}
        <span>{turn.providerKind}</span>
        {turn.apiMode && <span className="opacity-60">· {turn.apiMode}</span>}
        {turn.inputTokens != null && (
          <span className="ml-auto tabular-nums">
            {turn.inputTokens}in
            {turn.cachedTokens != null && ` · ${turn.cachedTokens}cache`}
            {turn.outputTokens != null && ` / ${turn.outputTokens}out`}
          </span>
        )}
      </div>

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
        {turn.cacheCreationTokens != null && (
          <span className="rounded bg-amber-100 px-1.5 py-0.5 text-[11px] tabular-nums text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
            +{turn.cacheCreationTokens} cache write
          </span>
        )}
        {turn.payloadBytes != null && (
          <span className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[11px] tabular-nums text-[var(--c-text-muted)]">
            {(turn.payloadBytes / 1024).toFixed(1)}KB
          </span>
        )}
        {inputMetaChips.map((value) => (
          <span key={value} className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 text-[11px] text-[var(--c-text-muted)]">
            {value}
          </span>
        ))}
      </div>

      {turn.systemPrompt && (
        <CollapseBlock label="System" preview={preview(turn.systemPrompt)}>
          <PreText text={turn.systemPrompt} />
        </CollapseBlock>
      )}

      {turn.toolNames && turn.toolNames.length > 0 && (
        <CollapseBlock
          label={`Tools (${turn.toolNames.length})`}
          preview={turn.toolNames.slice(0, 5).join(', ') + (turn.toolNames.length > 5 ? ', ...' : '')}
        >
          <div className="flex flex-wrap gap-1">
            {turn.toolNames.map((name) => (
              <span key={name} className="rounded bg-[var(--c-bg-sub)] px-1.5 py-0.5 font-mono text-[11px] text-[var(--c-text-secondary)]">
                {name}
                {turn.toolSchemaBytesMap?.[name] != null && (
                  <span className="ml-1 opacity-50">
                    {(turn.toolSchemaBytesMap[name] / 1024).toFixed(1)}KB
                  </span>
                )}
              </span>
            ))}
          </div>
        </CollapseBlock>
      )}

      {(turn.systemBytes != null || turn.toolsBytes != null || turn.messagesBytes != null) && (
        <CollapseBlock
          label={`Context${turn.messagesBytes != null ? ` ${(turn.messagesBytes / 1024).toFixed(1)}KB` : ''}`}
          preview={[
            turn.systemBytes != null ? `sys ${(turn.systemBytes / 1024).toFixed(1)}KB` : null,
            turn.toolsBytes != null ? `tools ${(turn.toolsBytes / 1024).toFixed(1)}KB` : null,
            turn.stablePrefixHash ? `prefix ${turn.stablePrefixHash.slice(0, 8)}` : null,
          ].filter(Boolean).join(' · ')}
        >
          <div className="space-y-1 text-xs font-mono text-[var(--c-text-muted)]">
            {turn.systemBytes != null && (
              <div className="flex justify-between">
                <span>system</span>
                <span>{(turn.systemBytes / 1024).toFixed(2)} KB</span>
              </div>
            )}
            {turn.toolsBytes != null && (
              <div className="flex justify-between">
                <span>tools</span>
                <span>{(turn.toolsBytes / 1024).toFixed(2)} KB</span>
              </div>
            )}
            {turn.messagesBytes != null && (
              <div className="flex justify-between">
                <span>messages</span>
                <span>{(turn.messagesBytes / 1024).toFixed(2)} KB</span>
              </div>
            )}
            {turn.roleBytes &&
              Object.entries(turn.roleBytes).map(([role, bytes]) => (
                <div key={role} className="flex justify-between pl-3 text-[10px] opacity-70">
                  <span>{role}</span>
                  <span>{(bytes / 1024).toFixed(2)} KB</span>
                </div>
              ))}
            {turn.stablePrefixHash && (
              <div className="mt-1 flex justify-between border-t border-[var(--c-border-subtle)] pt-1">
                <span>prefix hash</span>
                <span className="font-mono">{turn.stablePrefixHash}</span>
              </div>
            )}
          </div>
        </CollapseBlock>
      )}

      <CollapseBlock
        label="Input"
        preview={turn.userInput ? preview(turn.userInput) : 'Input unavailable'}
      >
        <PreText text={turn.userInput ?? 'Input unavailable'} />
      </CollapseBlock>

      {orderedSegments.length > 0 && (
        <CollapseBlock label="Execution" preview={executionPreview(orderedSegments)}>
          <div className="space-y-2">
            {orderedSegments.map((segment, segmentIndex) => {
              if (segment.kind === 'assistant') {
                return (
                  <div key={`assistant-${segmentIndex}`} className="rounded border border-[var(--c-border-subtle)] bg-[var(--c-bg-sub)]/40 p-2">
                    <div className="mb-1 text-[11px] font-medium uppercase tracking-wide text-[var(--c-text-muted)]">
                      assistant
                    </div>
                    <PreText text={segment.text} />
                  </div>
                )
              }

              if (segment.kind === 'tool_call') {
                return (
                  <CollapseBlock
                    key={segment.toolCallId || `tool-call-${segmentIndex}`}
                    label={`tool.call  ${segment.toolName}`}
                    preview={previewJSON(segment.argsJSON)}
                  >
                    <JsonBlock value={segment.argsJSON} />
                  </CollapseBlock>
                )
              }

              return (
                <CollapseBlock
                  key={segment.toolCallId || `tool-result-${segmentIndex}`}
                  label={segment.errorClass ? `tool.result  ${segment.toolName || 'error'}` : `tool.result  ${segment.toolName}`}
                  preview={segment.errorClass ?? previewJSON(segment.resultJSON)}
                  dim={!!segment.errorClass}
                >
                  {segment.errorClass ? (
                    <span className="text-xs text-red-500">{segment.errorClass}</span>
                  ) : (
                    <JsonBlock value={segment.resultJSON} />
                  )}
                </CollapseBlock>
              )
            })}
          </div>
        </CollapseBlock>
      )}

      <CollapseBlock
        label="Assistant"
        preview={turn.assistantText ? preview(turn.assistantText) : 'Assistant output unavailable'}
        defaultOpen
      >
        <PreText text={turn.assistantText || 'Assistant output unavailable'} />
      </CollapseBlock>
    </div>
  )
}
