import { SearchFetchSettings } from './SearchFetchSettings'

type Props = {
  accessToken: string
}

export function ToolsSettings({ accessToken }: Props) {
  return <SearchFetchSettings accessToken={accessToken} />
}
