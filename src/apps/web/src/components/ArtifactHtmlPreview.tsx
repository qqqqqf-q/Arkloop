import type { ArtifactRef } from '../storage'
import { ArtifactIframe } from './ArtifactIframe'

type Props = {
  artifact: ArtifactRef
  accessToken: string
}

export function ArtifactHtmlPreview({ artifact, accessToken }: Props) {
  return (
    <ArtifactIframe
      mode="static"
      artifact={artifact}
      accessToken={accessToken}
      frameTitle={artifact.title ?? artifact.filename}
      style={{ minHeight: '300px' }}
    />
  )
}
