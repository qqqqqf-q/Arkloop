import { memo, type ReactNode } from 'react'
import type { CodeExecutionRef } from '../storage'
import type { FileOpRef } from '../storage'
import type { GenericToolCallRef, TodoWriteRef } from '../copSegmentTimeline'
import type { CodeExecution } from './CodeExecutionCard'
import { CodeExecutionCard } from './CodeExecutionCard'
import { ExecutionCard } from './ExecutionCard'
import { TodoListCard } from './TodoListCard'
import { FileOpToolRow } from './cop-timeline/ToolRows'
import { markerForToolName, type TimelineMarker } from './cop-timeline/markers'

export type TopLevelCopToolEntry =
  | { kind: 'code'; id: string; seq: number; item: CodeExecutionRef }
  | { kind: 'todo'; id: string; seq: number; item: TodoWriteRef }
  | { kind: 'file'; id: string; seq: number; item: FileOpRef }
  | { kind: 'generic'; id: string; seq: number; item: GenericToolCallRef }

const MONO = 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, Consolas, monospace'
const TOP_LEVEL_TOOL_ICON_WIDTH = 18
const TOP_LEVEL_TOOL_ICON_GAP = 4
const TOP_LEVEL_TOOL_EXPANDED_OFFSET = -(TOP_LEVEL_TOOL_ICON_WIDTH + TOP_LEVEL_TOOL_ICON_GAP)
const TOP_LEVEL_TOOL_LINE_HEIGHT = 20

function livePreviewLines(text: string | undefined): string[] {
  if (!text?.trim()) return []
  return text.replace(/\r\n/g, '\n').split('\n').filter((line) => line.trim() !== '').slice(-5)
}

function TopLevelLivePreview({ text }: { text?: string }) {
  const lines = livePreviewLines(text)
  if (lines.length === 0) return null
  return (
    <div
      className="cop-root-live-preview"
      style={{
        marginTop: 4,
        marginLeft: TOP_LEVEL_TOOL_EXPANDED_OFFSET,
        width: `calc(100% + ${Math.abs(TOP_LEVEL_TOOL_EXPANDED_OFFSET)}px)`,
        maxWidth: 'min(100%, 720px)',
        maxHeight: 86,
        overflow: 'hidden',
        borderRadius: 8,
        border: '0.5px solid var(--c-border-subtle)',
        background: 'var(--c-code-preview-bg)',
      }}
    >
      <pre
        style={{
          margin: 0,
          padding: '7px 10px',
          fontFamily: MONO,
          fontSize: 11,
          lineHeight: '16px',
          color: 'var(--c-text-secondary)',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
        }}
      >
        {lines.join('\n')}
      </pre>
    </div>
  )
}

function TopLevelToolMarker({ marker }: { marker: TimelineMarker }) {
  if (marker.kind === 'icon') {
    return (
      <span
        title={marker.label}
        aria-label={marker.label}
        style={{
          width: TOP_LEVEL_TOOL_ICON_WIDTH,
          height: TOP_LEVEL_TOOL_LINE_HEIGHT,
          flex: `0 0 ${TOP_LEVEL_TOOL_ICON_WIDTH}px`,
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          paddingTop: 1,
          color: 'var(--c-cop-row-fg, var(--c-text-tertiary))',
        }}
      >
        <marker.icon width={13} height={13} strokeWidth={2.1} />
      </span>
    )
  }
  return (
    <span
      aria-hidden="true"
      style={{
        width: TOP_LEVEL_TOOL_ICON_WIDTH,
        height: TOP_LEVEL_TOOL_LINE_HEIGHT,
        flex: `0 0 ${TOP_LEVEL_TOOL_ICON_WIDTH}px`,
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <span
        style={{
          width: 8,
          height: 8,
          borderRadius: '50%',
          background: 'var(--c-cop-row-fg, var(--c-text-tertiary))',
        }}
      />
    </span>
  )
}

function TopLevelToolFrame({ toolName, children }: { toolName: string; children: ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'flex-start', gap: TOP_LEVEL_TOOL_ICON_GAP, padding: '6px 0' }}>
      <TopLevelToolMarker marker={markerForToolName(toolName)} />
      <div style={{ minWidth: 0, flex: 1 }}>
        {children}
      </div>
    </div>
  )
}

export const TopLevelCopToolBlock = memo(function TopLevelCopToolBlock({
  entry,
  live,
  onOpenCodeExecution,
  activeCodeExecutionId,
}: {
  entry: TopLevelCopToolEntry
  live?: boolean
  onOpenCodeExecution?: (ce: CodeExecution) => void
  activeCodeExecutionId?: string
}) {
  if (entry.kind === 'todo') {
    return <TodoListCard todo={entry.item} />
  }

  if (entry.kind === 'file') {
    return (
      <TopLevelToolFrame toolName={entry.item.toolName}>
        <FileOpToolRow op={entry.item} live={live} expandedOffsetLeft={TOP_LEVEL_TOOL_EXPANDED_OFFSET} />
        {live && entry.item.status === 'running' && <TopLevelLivePreview text={entry.item.output} />}
      </TopLevelToolFrame>
    )
  }

  if (entry.kind === 'generic') {
    const item = entry.item
    return (
      <TopLevelToolFrame toolName={item.toolName}>
        <ExecutionCard
          variant="fileop"
          toolName={item.toolName}
          label={item.label}
          displayDescription={item.displayDescription}
          displayText={item.displayText}
          output={item.status === 'running' ? undefined : item.output}
          emptyLabel={item.emptyLabel}
          status={item.status}
          errorMessage={item.errorMessage}
          smooth={!!live && item.status === 'running'}
          expandedOffsetLeft={TOP_LEVEL_TOOL_EXPANDED_OFFSET}
        />
        {live && item.status === 'running' && <TopLevelLivePreview text={item.output} />}
      </TopLevelToolFrame>
    )
  }

  const ce = entry.item
  return (
    <TopLevelToolFrame toolName={ce.language === 'shell' ? 'exec_command' : 'python_execute'}>
      {ce.language === 'shell'
        ? (
          <ExecutionCard
            variant="shell"
            displayDescription={ce.displayDescription}
            displayText={ce.displayText}
            code={ce.code}
            output={ce.output}
            status={ce.status}
            errorMessage={ce.errorMessage}
            smooth={!!live && ce.status === 'running'}
            expandedOffsetLeft={TOP_LEVEL_TOOL_EXPANDED_OFFSET}
          />
        )
        : (
          <CodeExecutionCard
            language={ce.language}
            code={ce.code}
            output={ce.output}
            errorMessage={ce.errorMessage}
            status={ce.status}
            onOpen={onOpenCodeExecution ? () => onOpenCodeExecution(ce) : undefined}
            isActive={activeCodeExecutionId === ce.id}
          />
        )
      }
    </TopLevelToolFrame>
  )
})
