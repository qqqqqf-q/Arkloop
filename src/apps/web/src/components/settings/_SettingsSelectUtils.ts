export const settingsSelectBorderColor = 'color-mix(in srgb, var(--c-border) 78%, var(--c-bg-input) 22%)'

export function getAdaptiveMenuLeft(
  rect: Pick<DOMRect, 'left' | 'right' | 'width'>,
  width: number,
  viewportWidth: number,
) {
  const margin = 16
  const leftAligned = rect.left
  const rightAligned = rect.right - width
  const hasRightRoom = leftAligned + width <= viewportWidth - margin
  const hasLeftRoom = rightAligned >= margin
  const triggerCenter = rect.left + rect.width / 2
  const shouldRightAlign =
    width > rect.width
    && hasLeftRoom
    && (!hasRightRoom || triggerCenter > viewportWidth / 2)
  const preferredLeft = shouldRightAlign ? rightAligned : leftAligned
  return Math.max(margin, Math.min(preferredLeft, viewportWidth - width - margin))
}
