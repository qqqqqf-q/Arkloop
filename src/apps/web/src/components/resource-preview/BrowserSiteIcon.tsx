import { useState } from 'react'
import { Globe2 } from 'lucide-react'
import { browserFaviconUrl } from './browserIdentity'

type Props = {
  url?: string
  faviconUrl?: string
  size?: number
}

export function BrowserSiteIcon({ url, faviconUrl, size = 15 }: Props) {
  const src = faviconUrl ?? (url ? browserFaviconUrl(url) : undefined)
  const [failed, setFailed] = useState(false)

  if (!src || failed) return <Globe2 size={size} />

  return (
    <img
      src={src}
      alt=""
      aria-hidden="true"
      draggable={false}
      width={size}
      height={size}
      onError={() => setFailed(true)}
      style={{ width: size, height: size, flexShrink: 0, borderRadius: Math.max(2, Math.floor(size / 5)) }}
    />
  )
}
