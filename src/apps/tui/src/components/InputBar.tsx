import type { KeyEvent, TextareaRenderable } from "@opentui/core"
import { useRenderer } from "@opentui/solid"
import { createEffect, createMemo, createSignal, For, Show } from "solid-js"
import { effortSymbol, formatEffort } from "../lib/effort"
import { addEntry, historyDown, historyUp, loadHistory, saveHistory } from "../lib/history"
import { streaming } from "../store/chat"
import { CHAT_CONTENT_GUTTER, CHAT_PREFIX_WIDTH } from "../lib/chatLayout"
import { tuiTheme } from "../lib/theme"
import {
  connected,
  currentEffort,
  currentModelLabel,
  currentModelSupportsReasoning,
  currentPersonaLabel,
  currentThreadId,
  exitConfirmPending,
  registerInputFocus,
  setExitConfirmPending,
  setOverlay,
  tokenUsage,
} from "../store/app"

interface Props {
  onSubmit: (text: string) => void
}

interface SlashCommand {
  command: string
  insert: string
  description: string
}

const keyBindings = [
  { name: "return", action: "submit" as const },
  { name: "return", meta: true, action: "newline" as const },
]

const slashCommands: SlashCommand[] = [
  { command: "/models", insert: "/models", description: "打开 model 选择器" },
  { command: "/effort", insert: "/effort ", description: "设置思考强度" },
  { command: "/sessions", insert: "/sessions", description: "打开会话列表" },
  { command: "/new", insert: "/new", description: "新建会话" },
  { command: "/model <name>", insert: "/model ", description: "直接切换模型" },
  { command: "/effort <level>", insert: "/effort ", description: "none|minimal|low|medium|high|max" },
]

export function InputBar(props: Props) {
  let input: TextareaRenderable
  const renderer = useRenderer()
  const [text, setText] = createSignal("")
  const [selectedIndex, setSelectedIndex] = createSignal(0)

  // input history
  let history = loadHistory()
  let historyCursor = -1
  let draft = ""
  let exitConfirmTimer: ReturnType<typeof setTimeout> | null = null

  const suggestions = createMemo(() => {
    const value = text().trimStart()
    if (!value.startsWith("/")) return []
    const needle = value.toLowerCase()
    return slashCommands.filter((item) => item.command.toLowerCase().startsWith(needle) || item.command.toLowerCase().includes(needle))
  })

  const activeSuggestion = createMemo(() => suggestions()[selectedIndex()] ?? suggestions()[0])
  const connectionText = () => connected() ? "connected" : "offline"
  const connectionColor = () => connected() ? tuiTheme.success : tuiTheme.error
  const usageText = () => {
    const usage = tokenUsage()
    const parts: string[] = []
    if (usage.input > 0 || usage.output > 0) {
      parts.push(`tokens ${usage.input}/${usage.output}`)
    }
    if (usage.context > 0) {
      parts.push(`ctx ${usage.context}`)
    }
    return parts.length > 0 ? parts.join(" · ") : null
  }

  function submit() {
    if (streaming() || !input || input.isDestroyed) return
    const value = (input.plainText ?? "").trim()
    if (!value) return
    props.onSubmit(value)
    history = addEntry(history, value)
    saveHistory(history)
    historyCursor = -1
    draft = ""
    input.clear()
    setText("")
  }

  createEffect(() => {
    if (!input || input.isDestroyed) return
    if (!input.focused) input.focus()
  })

  createEffect(() => {
    registerInputFocus(() => {
      if (!input || input.isDestroyed) return
      input.focus()
    })
  })

  createEffect(() => {
    const items = suggestions()
    if (items.length === 0) {
      setSelectedIndex(0)
      return
    }
    if (selectedIndex() > items.length - 1) {
      setSelectedIndex(items.length - 1)
    }
  })

  function replaceWithSuggestion(command: string) {
    if (!input || input.isDestroyed) return
    input.setText(command)
    setText(input.plainText ?? "")
    input.focus()
  }

  function handleKeyDown(event: KeyEvent) {
    // Ctrl+C three-tier logic
    if (event.ctrl && event.name === "c") {
      event.preventDefault()
      const value = (input?.plainText ?? "").trim()

      // tier 1: input has content — clear and save to history
      if (value) {
        history = addEntry(history, value)
        saveHistory(history)
        historyCursor = -1
        draft = ""
        input?.clear()
        setText("")
        setExitConfirmPending(false)
        if (exitConfirmTimer) clearTimeout(exitConfirmTimer)
        exitConfirmTimer = null
        return
      }

      // tier 2: input empty, not confirming — enter confirm state
      if (!exitConfirmPending()) {
        setExitConfirmPending(true)
        exitConfirmTimer = setTimeout(() => {
          setExitConfirmPending(false)
          exitConfirmTimer = null
        }, 3000)
        return
      }

      // tier 3: already confirming — exit
      if (exitConfirmTimer) clearTimeout(exitConfirmTimer)
      renderer.destroy()
      const threadId = currentThreadId()
      if (threadId) {
        process.stderr.write(`\nTo resume this session:\n  ark --resume ${threadId}\n`)
      }
      process.exit(0)
    }

    // any other key resets exit confirm
    if (exitConfirmPending()) {
      setExitConfirmPending(false)
      if (exitConfirmTimer) clearTimeout(exitConfirmTimer)
      exitConfirmTimer = null
    }

    // slash command suggestions take priority
    if (suggestions().length > 0) {
      if (event.name === "up") {
        event.preventDefault()
        setSelectedIndex((prev) => (prev <= 0 ? suggestions().length - 1 : prev - 1))
        return
      }

      if (event.name === "down") {
        event.preventDefault()
        setSelectedIndex((prev) => (prev >= suggestions().length - 1 ? 0 : prev + 1))
        return
      }

      if (event.name === "tab") {
        event.preventDefault()
        const next = activeSuggestion()
        if (!next) return
        replaceWithSuggestion(next.insert)
        return
      }

      if (event.name === "return") {
        const value = (input?.plainText ?? "").trim()
        if (value.startsWith("/") && !slashCommands.some((item) => item.command === value) && activeSuggestion()) {
          event.preventDefault()
          replaceWithSuggestion(activeSuggestion()!.insert)
        }
      }
      return
    }

    // history navigation (no suggestions active)
    if (event.name === "up") {
      const current = (input?.plainText ?? "")
      if (historyCursor < 0) draft = current
      const [next, value] = historyUp(history, historyCursor, draft)
      if (next !== historyCursor) {
        event.preventDefault()
        historyCursor = next
        input?.setText(value)
        setText(value)
      }
      return
    }

    if (event.name === "down") {
      if (historyCursor < 0) return
      const [next, value] = historyDown(history, historyCursor, draft)
      event.preventDefault()
      historyCursor = next
      input?.setText(value)
      setText(value)
      return
    }
  }

  return (
    <box
      position="absolute"
      left={0}
      right={0}
      bottom={0}
      zIndex={40}
      width="100%"
      flexDirection="column"
      backgroundColor={tuiTheme.background}
    >
      <box
        position="relative"
        width="100%"
        flexDirection="column"
        border={["top", "bottom"]}
        borderColor={tuiTheme.borderSubtle}
        backgroundColor={tuiTheme.background}
        onMouseOver={() => input?.focus()}
        onMouseDown={() => input?.focus()}
      >
        <Show when={suggestions().length > 0}>
          <box
            position="absolute"
            bottom="100%"
            left={0}
            right={0}
            zIndex={60}
            width="100%"
            paddingLeft={CHAT_CONTENT_GUTTER + CHAT_PREFIX_WIDTH}
            paddingRight={1}
          >
            <box flexDirection="column" width="100%" backgroundColor={tuiTheme.panel}>
              <For each={suggestions()}>
                {(item, index) => {
                  const active = () => index() === selectedIndex()
                  return (
                    <box
                      flexDirection="row"
                      justifyContent="space-between"
                      paddingLeft={2}
                      paddingRight={2}
                      backgroundColor={active() ? tuiTheme.primary : tuiTheme.panel}
                    >
                      <text content={item.command} fg={active() ? tuiTheme.background : tuiTheme.text} />
                      <text content={item.description} fg={active() ? tuiTheme.background : tuiTheme.textMuted} />
                    </box>
                  )
                }}
              </For>
            </box>
          </box>
        </Show>
        <box
          width="100%"
          flexDirection="row"
          alignItems="flex-start"
          paddingLeft={CHAT_CONTENT_GUTTER}
          paddingRight={1}
        >
          <box width={CHAT_PREFIX_WIDTH} justifyContent="center">
            <text content="❯" fg={streaming() ? tuiTheme.primary : tuiTheme.text} />
          </box>
          <box flexGrow={1}>
            <textarea
              ref={(r: TextareaRenderable) => {
                input = r
              }}
              onSubmit={() => {
                setTimeout(() => setTimeout(() => submit(), 0), 0)
              }}
              onContentChange={() => {
                setText(input?.plainText ?? "")
              }}
              onKeyDown={handleKeyDown}
              keyBindings={keyBindings}
              placeholder={streaming() ? "Waiting for response..." : "Type a message or / for commands..."}
              placeholderColor={tuiTheme.textMuted}
              textColor={tuiTheme.text}
              focusedTextColor={tuiTheme.text}
              focusedBackgroundColor={tuiTheme.background}
              cursorColor={tuiTheme.primary}
              width="100%"
              minHeight={1}
              maxHeight={4}
            />
          </box>
        </box>
      </box>
      <box
        flexDirection="row"
        justifyContent="space-between"
        width="100%"
        paddingLeft={CHAT_CONTENT_GUTTER + CHAT_PREFIX_WIDTH}
        paddingRight={1}
      >
        <box flexDirection="row" gap={1}>
          <text content={currentPersonaLabel() || "Work"} fg={tuiTheme.textMuted} />
          <text content="·" fg={tuiTheme.border} />
          <text content={currentModelLabel() || "auto"} fg={tuiTheme.textMuted} />
          <Show when={currentModelSupportsReasoning() && currentEffort() !== "none"}>
            <text content="·" fg={tuiTheme.border} />
            <text
              content={`${effortSymbol(currentEffort())}${effortSymbol(currentEffort()) ? " " : ""}${formatEffort(currentEffort())}`}
              fg={tuiTheme.primary}
              onMouseUp={() => setOverlay("effort")}
            />
          </Show>
        </box>
        <box flexDirection="row" gap={1}>
          <Show when={exitConfirmPending()} fallback={
            <>
              <Show when={usageText()}>
                <text content={usageText() ?? ""} fg={tuiTheme.textMuted} />
                <text content="·" fg={tuiTheme.border} />
              </Show>
              <text content={connectionText()} fg={connectionColor()} />
              <text content="·" fg={tuiTheme.border} />
              <text content={suggestions().length > 0 ? "tab 补全" : "/ 命令"} fg={tuiTheme.textMuted} />
            </>
          }>
            <text content="Press Ctrl+C again to exit" fg={tuiTheme.warning ?? tuiTheme.error} />
          </Show>
        </box>
      </box>
      <box width="100%" height={1} backgroundColor={tuiTheme.background} />
    </box>
  )
}
