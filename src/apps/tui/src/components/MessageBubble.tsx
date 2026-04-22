import { SyntaxStyle } from "@opentui/core"
import { Show } from "solid-js"
import { CHAT_CONTENT_GUTTER, CHAT_LEAD_GUTTER_WIDTH, CHAT_PREFIX_WIDTH } from "../lib/chatLayout"
import { tuiTheme } from "../lib/theme"

const markdownSyntaxStyle = SyntaxStyle.create()

interface Props {
  role: "user" | "assistant" | "tool"
  content?: string
  toolName?: string
  toolSummary?: string
  toolStatus?: "pending" | "success" | "error"
  toolError?: string
  streaming?: boolean
}

export function MessageBubble(props: Props) {
  const isUser = () => props.role === "user"
  const toolLineText = () => props.toolSummary?.trim() || props.toolName?.trim() || "Tool"

  const roleFg = () => {
    switch (props.role) {
      case "user": return tuiTheme.info
      case "assistant": return tuiTheme.primary
      case "tool": return props.toolStatus === "error" ? tuiTheme.error : tuiTheme.warning
    }
  }

  return (
    <box flexDirection="column" width="100%" paddingBottom={1}>
      {!isUser() && props.role === "tool" ? (
        <box flexDirection="row" width="100%" paddingLeft={CHAT_CONTENT_GUTTER} paddingRight={1}>
          <box width={CHAT_LEAD_GUTTER_WIDTH}>
            <text content="•" fg={roleFg()} />
          </box>
          <box width={CHAT_PREFIX_WIDTH} />
          <box flexGrow={1} flexDirection="column" gap={0}>
            <text content={toolLineText()} fg={tuiTheme.textMuted} wrapMode="word" />
            <Show when={props.toolStatus === "error" && props.toolError}>
              <text content={props.toolError ?? ""} fg={tuiTheme.error} wrapMode="word" />
            </Show>
          </box>
        </box>
      ) : null}
      {isUser() ? (
        <box flexDirection="row" width="100%" paddingLeft={CHAT_CONTENT_GUTTER} paddingRight={1}>
          <box width={CHAT_LEAD_GUTTER_WIDTH}>
            <text content="•" fg={tuiTheme.textMuted} />
          </box>
          <box flexGrow={1} backgroundColor={tuiTheme.userPromptBg} paddingLeft={1} paddingRight={1}>
            <box flexDirection="row" alignItems="flex-start">
              <box width={CHAT_PREFIX_WIDTH}>
                <text content="❯" fg={tuiTheme.primary} />
              </box>
              <box flexGrow={1}>
                <text content={props.content ?? ""} wrapMode="word" fg={tuiTheme.text} />
              </box>
            </box>
          </box>
        </box>
      ) : props.role === "assistant" ? (
        <box flexDirection="row" width="100%" paddingLeft={CHAT_CONTENT_GUTTER} paddingRight={1}>
          <box width={CHAT_LEAD_GUTTER_WIDTH} />
          <box width={CHAT_PREFIX_WIDTH} />
          <box flexGrow={1}>
            <markdown content={props.content ?? ""} syntaxStyle={markdownSyntaxStyle} streaming={props.streaming} />
          </box>
        </box>
      ) : null}
    </box>
  )
}
