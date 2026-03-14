import { ModelConfigContent } from '../ModelConfigContent'

type Props = {
  accessToken: string
}

export function ProvidersSettings({ accessToken }: Props) {
  return <ModelConfigContent accessToken={accessToken} />
}
