import { MessageList } from "./MessageList"
import { InputBar } from "./InputBar"
import { StartupCard } from "./StartupCard"
import { INPUT_BAR_OVERLAY_HEIGHT } from "../lib/chatLayout"

interface Props {
  onSubmit: (text: string) => void
}

export function ChatView(props: Props) {
  return (
    <box position="relative" flexDirection="column" width="100%" flexGrow={1} paddingRight={1}>
      <box flexDirection="column" flexGrow={1} width="100%" paddingBottom={INPUT_BAR_OVERLAY_HEIGHT}>
        <StartupCard />
        <box flexGrow={1} width="100%">
          <MessageList />
        </box>
      </box>
      <InputBar onSubmit={props.onSubmit} />
    </box>
  )
}
