import { createMemo, For, Index, Match, Show, Switch, type Accessor } from "solid-js"
import { error, historyTurns, liveAssistantTurn, pendingUserInput, streaming } from "../store/chat"
import type { AssistantTurnSegment, CopBlockItem } from "../lib/assistantTurn"
import { MessageBubble } from "./MessageBubble"
import { StartupCard } from "./StartupCard"
import { compressTurnSegments, summarizeLiveToolCall, type ToolRenderItem } from "../lib/toolSummary"
import type { LlmTurn } from "../lib/runTurns"

export function MessageList() {
  function renderTurn(turn: LlmTurn) {
    const segments = compressTurnSegments(turn.segments)
    const userText = decorateUserInput(turn.userInput, turn.imagePartCount)
    return (
      <box flexDirection="column" width="100%">
        <Show when={userText}>
          <MessageBubble role="user" content={userText} />
        </Show>
        <Index each={segments}>
          {(segment) => {
            const current = segment()
            if (current.kind === "tool") {
              return (
                <MessageBubble
                  role="tool"
                  toolName={current.tool.toolName}
                  toolSummary={current.tool.summary}
                  toolStatus={current.tool.status}
                  toolError={current.tool.errorSummary}
                  toolResultContent={current.tool.resultContent}
                  toolResultLineCount={current.tool.resultLineCount}
                />
              )
            }
            return <MessageBubble role="assistant" content={current.text} />
          }}
        </Index>
      </box>
    )
  }

  // 流式期间 liveAssistantTurn 每个 delta 都是新快照；这里只按下标读取 accessor，
  // 让已有气泡原地更新文本，避免整段重建造成闪烁和历史 markdown 反复解析。
  function renderLiveCopItem(item: Accessor<CopBlockItem>) {
    const tool = createMemo(() => {
      const current = item()
      return current.kind === "call" ? summarizeLiveToolCall(current.call) : null
    })
    return (
      <Switch>
        <Match when={item().kind === "assistant_text"}>
          <MessageBubble role="assistant" content={liveItemText(item())} streaming={streaming()} />
        </Match>
        <Match when={tool()}>
          {(current: Accessor<ToolRenderItem>) => (
            <MessageBubble
              role="tool"
              toolName={current().toolName}
              toolSummary={current().summary}
              toolStatus={current().status}
              toolError={current().errorSummary}
              toolResultContent={current().resultContent}
              toolResultLineCount={current().resultLineCount}
            />
          )}
        </Match>
      </Switch>
    )
  }

  function renderLiveSegment(segment: Accessor<AssistantTurnSegment>) {
    return (
      <Switch>
        <Match when={segment().type === "text"}>
          <MessageBubble role="assistant" content={liveSegmentText(segment())} streaming={streaming()} />
        </Match>
        <Match when={segment().type === "cop"}>
          <Index each={liveSegmentItems(segment())}>
            {(item) => renderLiveCopItem(item)}
          </Index>
        </Match>
      </Switch>
    )
  }

  return (
    <scrollbox stickyScroll={true} stickyStart="bottom" flexGrow={1} width="100%">
      <box flexDirection="column" width="100%" minHeight="100%">
        <StartupCard />
        <box flexGrow={1} width="100%" />
        {/* opentui 的动态插入不保证同级锚点顺序，历史区和实时区各用一个固定容器隔开 */}
        <box flexDirection="column" width="100%">
          <For each={historyTurns}>
            {(turn) => renderTurn(turn)}
          </For>
        </box>
        <box flexDirection="column" width="100%">
          <Show when={pendingUserInput() !== null}>
            <MessageBubble role="user" content={pendingUserInput() ?? ""} />
          </Show>
          <Index each={liveAssistantTurn()?.segments ?? []}>
            {(segment) => renderLiveSegment(segment)}
          </Index>
        </box>
        <Show when={error()}>
          <MessageBubble role="tool" toolName="error" toolStatus="error" toolError={error() ?? ""} />
        </Show>
      </box>
    </scrollbox>
  )
}

function liveSegmentText(segment: AssistantTurnSegment): string {
  return segment.type === "text" ? segment.content : ""
}

function liveSegmentItems(segment: AssistantTurnSegment): CopBlockItem[] {
  return segment.type === "cop" ? segment.items : []
}

function liveItemText(item: CopBlockItem): string {
  return item.kind === "assistant_text" ? item.content : ""
}

function decorateUserInput(text?: string, imagePartCount?: number): string {
  const lines: string[] = []
  const value = text?.trim()
  if (value) {
    lines.push(value)
  }
  if (imagePartCount && imagePartCount > 0) {
    lines.push(imagePartCount === 1 ? "[图片]" : `[图片 x${imagePartCount}]`)
  }
  return lines.join("\n")
}
