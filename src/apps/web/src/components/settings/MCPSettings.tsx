import { MCPSettingsContent } from '../MCPSettingsContent'

type Props = {
  accessToken: string
}

export function MCPSettings({ accessToken }: Props) {
  return (
    <div className="mx-auto w-full max-w-[760px] px-1 pb-8">
      <MCPSettingsContent accessToken={accessToken} />
    </div>
  )
}
