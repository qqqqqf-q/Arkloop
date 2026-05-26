import { ArtifactImage } from './ArtifactImage'
import type { GeneratedImageItem } from '../generatedImages'

export function GeneratedImageGroup({
  items,
  accessToken,
}: {
  items?: GeneratedImageItem[]
  accessToken?: string
}) {
  if (!items || items.length === 0 || !accessToken) return null

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', marginTop: '8px' }}>
      {items.map((item) => (
        <ArtifactImage
          key={item.artifact.key}
          artifact={item.artifact}
          accessToken={accessToken}
        />
      ))}
    </div>
  )
}
