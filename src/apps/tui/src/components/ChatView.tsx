import { MessageList } from "./MessageList"
import { InputBar } from "./InputBar"
import { INPUT_BAR_OVERLAY_HEIGHT } from "../lib/chatLayout"
import type { MessageComposePayload } from "../api/types"

interface Props {
  onSubmit: (payload: MessageComposePayload) => void | Promise<void>
}

export function ChatView(props: Props) {
  return (
    <box position="relative" flexDirection="column" width="100%" flexGrow={1} paddingRight={1}>
      <box flexGrow={1} width="100%" paddingBottom={INPUT_BAR_OVERLAY_HEIGHT}>
        <MessageList />
      </box>
      <InputBar onSubmit={props.onSubmit} />
    </box>
  )
}
