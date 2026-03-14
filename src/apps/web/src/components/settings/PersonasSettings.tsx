import { AgentSettingsContent } from '../AgentSettingsContent'

type Props = {
  accessToken: string
}

export function PersonasSettings({ accessToken }: Props) {
  return <AgentSettingsContent accessToken={accessToken} />
}
