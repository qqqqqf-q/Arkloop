import { MCPSettingsContent } from '../MCPSettingsContent'

type Props = {
  accessToken: string
}

export function MCPSettings({ accessToken }: Props) {
  return <MCPSettingsContent accessToken={accessToken} />
}
