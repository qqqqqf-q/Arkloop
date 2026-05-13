import type { PreviewResource } from './types'
import { isCodeMime, isIframeMime, isJsonMime, isMarkdownMime, isTextMime } from './mime'

export type PreviewRendererKind = 'markdown' | 'json' | 'code' | 'text' | 'image' | 'iframe' | 'binary'

export function getPreviewRendererKind(resource: Pick<PreviewResource, 'mimeType' | 'filename'>): PreviewRendererKind {
  if (isIframeMime(resource.mimeType)) return 'iframe'
  if (resource.mimeType.startsWith('image/')) return 'image'
  if (isMarkdownMime(resource.mimeType, resource.filename)) return 'markdown'
  if (isJsonMime(resource.mimeType, resource.filename)) return 'json'
  if (isCodeMime(resource.mimeType, resource.filename)) return 'code'
  if (isTextMime(resource.mimeType)) return 'text'
  return 'binary'
}
