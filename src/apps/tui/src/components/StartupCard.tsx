import { Show } from "solid-js"
import { CHAT_CONTENT_GUTTER } from "../lib/chatLayout"
import { formatEffort } from "../lib/effort"
import { tuiTheme } from "../lib/theme"
import {
  appVersion,
  currentEffort,
  currentModel,
  currentModelLabel,
  currentModelSupportsReasoning,
  currentPersona,
  currentPersonaLabel,
  currentUsername,
  workingDirectory,
} from "../store/app"

export function StartupCard() {
  const modelText = () => {
    const model = currentModelLabel() || currentModel() || "loading..."
    if (!currentModelSupportsReasoning() || currentEffort() === "none") {
      return model
    }
    return `${model} · ${formatEffort(currentEffort())}`
  }
  const personaText = () => currentPersonaLabel() || currentPersona() || "work"
  const directoryText = () => workingDirectory() || "."
  const versionText = () => `v${appVersion()}`

  function renderInfoRow(label: string, value: () => string, paddingTop = 0, paddingBottom = 0) {
    return (
      <box flexDirection="row" width="100%" paddingLeft={2} paddingRight={2} paddingTop={paddingTop} paddingBottom={paddingBottom}>
        <box width={11}>
          <text content={label} fg={tuiTheme.textMuted} />
        </box>
        <box flexGrow={1}>
          <text content={value()} fg={tuiTheme.text} wrapMode="word" />
        </box>
      </box>
    )
  }

  return (
    <box width="100%" paddingLeft={CHAT_CONTENT_GUTTER} paddingRight={1} paddingTop={1} paddingBottom={1}>
      <box
        width={84}
        maxWidth="100%"
        flexDirection="column"
        borderStyle="rounded"
        border={["top", "right", "bottom", "left"]}
        borderColor={tuiTheme.border}
      >
        <box
          width="100%"
          flexDirection="row"
          justifyContent="space-between"
          paddingLeft={2}
          paddingRight={2}
          paddingTop={1}
          paddingBottom={1}
          border={["bottom"]}
          borderColor={tuiTheme.borderSubtle}
        >
          <box flexDirection="row" gap={1}>
            <text content="Arkloop" fg={tuiTheme.primary} />
            <text content="TUI" fg={tuiTheme.text} />
            <text content={versionText()} fg={tuiTheme.textMuted} />
          </box>
          <Show when={currentUsername()}>
            <text content={`@${currentUsername()}`} fg={tuiTheme.info} />
          </Show>
        </box>
        {renderInfoRow("model", modelText, 1, 0)}
        {renderInfoRow("persona", personaText, 0, 0)}
        {renderInfoRow("directory", directoryText, 0, 1)}
      </box>
    </box>
  )
}
