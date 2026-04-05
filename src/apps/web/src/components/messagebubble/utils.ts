import type { ArtifactRef } from '../../storage'

export function isDocumentArtifact(artifact: ArtifactRef): boolean {
  if (artifact.display === 'panel') return true
  return !artifact.mime_type.startsWith('image/') && artifact.mime_type !== 'text/html'
}

function parseDate(dateStr: string): Date {
  if (dateStr.includes('T') || dateStr.includes('Z') || dateStr.includes('+'))
    return new Date(dateStr)
  return new Date(dateStr.replace(' ', 'T') + 'Z')
}

export function formatShortDate(dateStr: string): string {
  const d = parseDate(dateStr)
  const now = new Date()
  const isSameDay =
    d.getFullYear() === now.getFullYear()
    && d.getMonth() === now.getMonth()
    && d.getDate() === now.getDate()
  if (isSameDay) {
    return d.toLocaleString('en-US', {
      hour: 'numeric',
      minute: '2-digit',
      hour12: true,
    }).replace(/\s/g, '')
  }
  const month = d.toLocaleString('en-US', { month: 'short' })
  return `${month}. ${d.getDate()}`
}

export function formatFullDate(dateStr: string): string {
  const d = parseDate(dateStr)
  return d.toLocaleString('en-US', {
    month: 'long',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    hour12: true,
  })
}

export function isArtifactReferenced(content: string, key: string): boolean {
  return content.includes(`artifact:${key}`)
}

export function getDomain(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, '')
  } catch {
    return url
  }
}

export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export const LIGHTBOX_ANIM_MS = 120

export const USER_TEXT_LINE_HEIGHT = 25.6 // 16px * 1.6
export const USER_TEXT_MAX_LINES = 9
export const USER_TEXT_COLLAPSED_HEIGHT = USER_TEXT_LINE_HEIGHT * USER_TEXT_MAX_LINES
export const USER_TEXT_FADE_HEIGHT = USER_TEXT_LINE_HEIGHT * 2

export const USER_PROMPT_MAX_WIDTH = 663
export const USER_PROMPT_ENTER_BASE_SCALE = 1.025
export const USER_PROMPT_ENTER_MAX_SCALE = 1.06

export function getUserPromptEnterScale(width: number): number {
  const safeWidth = Number.isFinite(width) ? Math.max(0, width) : USER_PROMPT_MAX_WIDTH
  const widthRatio = Math.min(safeWidth / USER_PROMPT_MAX_WIDTH, 1)
  const compensationRatio = 1 - widthRatio
  const compensationBoost = 0.85 + compensationRatio * 0.3
  return Math.min(
    USER_PROMPT_ENTER_MAX_SCALE,
    USER_PROMPT_ENTER_BASE_SCALE
      + (USER_PROMPT_ENTER_MAX_SCALE - USER_PROMPT_ENTER_BASE_SCALE) * compensationRatio * compensationBoost,
  )
}
