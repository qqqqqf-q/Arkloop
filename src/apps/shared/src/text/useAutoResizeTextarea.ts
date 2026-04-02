import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react'
import { measureTextareaHeight } from './measureTextareaHeight'

type BoxSizing = 'border-box' | 'content-box'

type Options = {
  value: string
  font?: string
  lineHeight?: number
  minRows?: number
  maxHeight?: number
}

type Result<T extends HTMLTextAreaElement> = {
  ref: RefObject<T | null>
  height: number | null
  overflowY: 'hidden' | 'auto'
  recompute: () => void
}

const useIsoLayoutEffect = typeof window === 'undefined' ? useEffect : useLayoutEffect

function readNumber(style: CSSStyleDeclaration, key: string) {
  const value = Number.parseFloat(style.getPropertyValue(key))
  return Number.isFinite(value) ? value : 0
}

function readLineHeight(style: CSSStyleDeclaration) {
  const explicit = Number.parseFloat(style.lineHeight)
  if (Number.isFinite(explicit)) return explicit
  const fontSize = Number.parseFloat(style.fontSize)
  if (Number.isFinite(fontSize)) return fontSize * 1.5
  return 20
}

function readFont(style: CSSStyleDeclaration) {
  if (style.font) return style.font
  const parts = [
    style.fontStyle,
    style.fontVariant,
    style.fontWeight,
    style.fontSize,
    style.fontFamily,
  ].filter(Boolean)
  return parts.join(' ')
}

function computeContentWidth(element: HTMLTextAreaElement, style: CSSStyleDeclaration) {
  const horizontalPadding = readNumber(style, 'padding-left') + readNumber(style, 'padding-right')
  const horizontalBorder = readNumber(style, 'border-left-width') + readNumber(style, 'border-right-width')
  return Math.max(element.clientWidth - horizontalPadding, element.offsetWidth - horizontalPadding - horizontalBorder, 1)
}

function computeOuterHeight(contentHeight: number, style: CSSStyleDeclaration, boxSizing: BoxSizing) {
  if (boxSizing === 'border-box') {
    const verticalPadding = readNumber(style, 'padding-top') + readNumber(style, 'padding-bottom')
    const verticalBorder = readNumber(style, 'border-top-width') + readNumber(style, 'border-bottom-width')
    return contentHeight + verticalPadding + verticalBorder
  }
  return contentHeight
}

function measureContentHeightWithScrollHeight(
  element: HTMLTextAreaElement,
  style: CSSStyleDeclaration,
  minContentHeight: number,
) {
  const previousHeight = element.style.height
  const previousOverflowY = element.style.overflowY
  element.style.height = '0px'
  element.style.overflowY = 'hidden'
  const scrollHeight = element.scrollHeight
  element.style.height = previousHeight
  element.style.overflowY = previousOverflowY
  if (!(scrollHeight > 0)) return null
  const verticalPadding = readNumber(style, 'padding-top') + readNumber(style, 'padding-bottom')
  return Math.max(scrollHeight - verticalPadding, minContentHeight)
}

function createResizeObserver(callback: () => void) {
  if (typeof ResizeObserver !== 'function') return null
  return new ResizeObserver(() => callback())
}

export function useAutoResizeTextarea<T extends HTMLTextAreaElement>({
  value,
  font,
  lineHeight,
  minRows = 1,
  maxHeight,
}: Options): Result<T> {
  const ref = useRef<T | null>(null)
  const [height, setHeight] = useState<number | null>(null)
  const [overflowY, setOverflowY] = useState<'hidden' | 'auto'>('hidden')
  const metricsRef = useRef({ width: 0, height: 0, overflowY: 'hidden' as 'hidden' | 'auto' })

  const recompute = useCallback(() => {
    const element = ref.current
    if (!element) return
    const style = window.getComputedStyle(element)
    const resolvedLineHeight = lineHeight ?? readLineHeight(style)
    const contentWidth = computeContentWidth(element, style)
    const minContentHeight = resolvedLineHeight * Math.max(minRows, 1)
    const contentHeight = measureContentHeightWithScrollHeight(element, style, minContentHeight) ?? measureTextareaHeight({
      value,
      width: contentWidth,
      font: font ?? readFont(style),
      lineHeight: resolvedLineHeight,
      minRows,
    })
    const boxSizing = (style.boxSizing === 'border-box' ? 'border-box' : 'content-box') satisfies BoxSizing
    const outerHeight = computeOuterHeight(contentHeight, style, boxSizing)
    const nextHeight = maxHeight ? Math.min(outerHeight, maxHeight) : outerHeight
    const nextOverflowY = maxHeight && outerHeight > maxHeight ? 'auto' : 'hidden'
    const prev = metricsRef.current
    if (prev.width === contentWidth && prev.height === nextHeight && prev.overflowY === nextOverflowY) return
    metricsRef.current = { width: contentWidth, height: nextHeight, overflowY: nextOverflowY }
    setHeight(nextHeight)
    setOverflowY(nextOverflowY)
  }, [font, lineHeight, maxHeight, minRows, value])

  useIsoLayoutEffect(() => {
    recompute()
  }, [recompute])

  useEffect(() => {
    const element = ref.current
    if (!element) return
    const observer = createResizeObserver(recompute)
    if (!observer) return
    observer.observe(element)
    return () => observer.disconnect()
  }, [recompute])

  return { ref, height, overflowY, recompute }
}
