import { onMount, Show } from "solid-js"
import { ChatView } from "./ChatView"
import { ModelSelect } from "./ModelSelect"
import { SessionList } from "./SessionList"
import { EffortSelect } from "./EffortSelect"
import type { ApiClient } from "../api/client"
import { createRunHandler } from "../hooks/useRun"
import { useKeybindings } from "../hooks/useKeybindings"
import { applyCurrentModel, applyCurrentPersona, currentModel, currentPersona, focusInput, overlay } from "../store/app"
import { defaultModel, listFlatModels } from "../lib/models"
import { tuiTheme } from "../lib/theme"
import { handleSlashCommand } from "../lib/commands"
import { readLastModel } from "../lib/threadMode"

interface Props {
  client: ApiClient
}

export function App(props: Props) {
  const { sendMessage } = createRunHandler(props.client)
  useKeybindings()

  onMount(async () => {
    if (!currentPersona()) {
      applyCurrentPersona("work", "Work")
    }
    if (currentModel()) return
    const models = await listFlatModels(props.client).catch(() => [])
    const lastModel = readLastModel()
    const model = (lastModel ? models.find((item) => item.id === lastModel) : null) ?? defaultModel(models)
    if (!model) return
    applyCurrentModel(model.id, model.label, model.supportsReasoning, model.contextLength)
  })

  async function submitInput(text: string) {
    if (await handleSlashCommand(props.client, text)) return
    await sendMessage(text)
  }

  return (
    <box
      flexDirection="column"
      width="100%"
      height="100%"
      backgroundColor={tuiTheme.background}
      onMouseOver={() => {
        if (!overlay()) focusInput()
      }}
    >
      <ChatView onSubmit={submitInput} />
      <Show when={overlay() === "model"}>
        <ModelSelect client={props.client} />
      </Show>
      <Show when={overlay() === "session"}>
        <SessionList client={props.client} />
      </Show>
      <Show when={overlay() === "effort"}>
        <EffortSelect />
      </Show>
    </box>
  )
}
