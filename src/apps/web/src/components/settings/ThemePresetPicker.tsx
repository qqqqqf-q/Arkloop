import { useEffect, useRef, useState } from 'react'
import { useAppearance } from '../../contexts/AppearanceContext'
import { BUILTIN_PRESETS } from '../../themes/presets'
import type { ThemePreset, ThemeDefinition, ThemeBackgroundImage } from '../../themes/types'
import { useLocale } from '../../contexts/LocaleContext'
import { Check, Image as ImageIcon, RotateCcw, SlidersHorizontal, Trash2, Upload, X } from 'lucide-react'
import { SettingsButton } from './_SettingsButton'

type SwatchVars = Partial<{ '--c-bg-page': string; '--c-bg-sidebar': string; '--c-accent': string; '--c-text-primary': string }>

type PresetCardProps = {
  name: string
  dark: SwatchVars
  active: boolean
  onClick: () => void
  imageUrl?: string | null
}

function PresetCard({ name, dark, active, onClick, imageUrl }: PresetCardProps) {
  const [hovered, setHovered] = useState(false)
  const bg = dark['--c-bg-page'] ?? '#1e1d1c'
  const sidebar = dark['--c-bg-sidebar'] ?? '#242422'
  const accent = dark['--c-accent'] ?? '#faf9f6'
  const text = dark['--c-text-primary'] ?? '#faf9f5'
  const imageMode = imageUrl !== undefined

  return (
    <button
      type="button"
      onClick={onClick}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      className="relative flex flex-col overflow-hidden rounded-xl"
      style={{
        border: active
          ? '1.5px solid var(--c-btn-bg)'
          : `1.5px solid ${hovered ? 'var(--c-input-border-color-hover)' : 'var(--c-input-border-color)'}`,
        width: '120px',
        background: 'var(--c-bg-page)',
        transition: 'border-color 0.14s ease-out',
      }}
    >
      {/* Mini preview */}
      <div
        className="flex w-full"
        style={{
          height: '60px',
          background: imageMode ? 'var(--c-bg-input)' : bg,
          backgroundImage: imageUrl ? backgroundCssUrl(imageUrl) : undefined,
          backgroundPosition: 'center',
          backgroundSize: 'cover',
          position: 'relative',
          overflow: 'hidden',
        }}
      >
        {imageMode ? (
          !imageUrl && (
            <div className="flex h-full w-full items-center justify-center text-[var(--c-text-muted)]">
              <ImageIcon size={16} />
            </div>
          )
        ) : (
          <>
            <div style={{ width: '28px', height: '100%', background: sidebar }} />
            <div className="flex flex-1 flex-col gap-1 p-2">
              <div style={{ height: '5px', width: '80%', background: text, borderRadius: '2px', opacity: 0.7 }} />
              <div style={{ height: '5px', width: '60%', background: text, borderRadius: '2px', opacity: 0.4 }} />
              <div style={{ height: '5px', width: '70%', background: text, borderRadius: '2px', opacity: 0.3 }} />
              <div
                style={{
                  marginTop: 'auto',
                  height: '8px',
                  width: '40px',
                  background: accent,
                  borderRadius: '3px',
                }}
              />
            </div>
          </>
        )}
        {active && (
          <div
            className="absolute right-1 top-1 z-10 flex items-center justify-center rounded-full"
            style={{ width: '16px', height: '16px', background: 'var(--c-btn-bg)' }}
          >
            <Check size={10} style={{ color: 'var(--c-btn-text)' }} strokeWidth={3} />
          </div>
        )}
      </div>
      <div
        className="px-2 py-1.5 text-xs truncate"
        style={{
          color: active ? 'var(--c-text-heading)' : 'var(--c-text-secondary)',
          fontWeight: active ? 500 : 400,
          borderTop: '0.5px solid var(--c-border-subtle)',
        }}
      >
        {name}
      </div>
    </button>
  )
}

const BACKGROUND_SOURCE_MAX_BYTES = 12 * 1024 * 1024
const BACKGROUND_STORED_MAX_CHARS = 3_500_000
const BACKGROUND_MAX_SIDE = 2400
const BACKGROUND_EDITOR_MAX_HEIGHT = 260
const BACKGROUND_MIN_CROP_SIZE = 0.08
const BACKGROUND_ACCEPT = 'image/png,image/jpeg,image/webp,image/avif'
const BACKGROUND_ALLOWED_TYPES = new Set(BACKGROUND_ACCEPT.split(','))

type CropRect = {
  x: number
  y: number
  width: number
  height: number
}

type CropSource = {
  file: File
  url: string
  width: number
  height: number
}

type CropDragMode = 'move' | 'nw' | 'ne' | 'sw' | 'se'

type CropDragState = {
  mode: CropDragMode
  startX: number
  startY: number
  startCrop: CropRect
}

const CROP_HANDLES: Array<{ mode: Exclude<CropDragMode, 'move'>; cursor: string; className: string }> = [
  { mode: 'nw', cursor: 'nwse-resize', className: '-left-1.5 -top-1.5' },
  { mode: 'ne', cursor: 'nesw-resize', className: '-right-1.5 -top-1.5' },
  { mode: 'sw', cursor: 'nesw-resize', className: '-bottom-1.5 -left-1.5' },
  { mode: 'se', cursor: 'nwse-resize', className: '-bottom-1.5 -right-1.5' },
]
const BACKGROUND_ICON_BUTTON_CLS =
  'flex h-7 w-7 items-center justify-center rounded-lg text-[var(--c-text-tertiary)] transition-colors hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--c-accent)] focus-visible:ring-offset-1 focus-visible:ring-offset-[var(--c-bg-menu)] disabled:opacity-50'
const BACKGROUND_PRIMARY_ICON_BUTTON_CLS =
  'flex h-7 w-7 items-center justify-center rounded-lg transition-[filter,opacity] hover:brightness-110 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--c-accent)] focus-visible:ring-offset-1 focus-visible:ring-offset-[var(--c-bg-menu)] disabled:opacity-50'

function backgroundCssUrl(value: string): string {
  return `url(${JSON.stringify(value)})`
}

function clamp(value: number, min: number, max: number): number {
  if (max < min) return min
  return Math.min(Math.max(value, min), max)
}

function fullCrop(): CropRect {
  return { x: 0, y: 0, width: 1, height: 1 }
}

function cropStageSize(source: CropSource | null, containerWidth: number): { width: number; height: number } {
  if (!source || containerWidth <= 0) return { width: 0, height: 0 }
  const scale = Math.min(
    containerWidth / source.width,
    BACKGROUND_EDITOR_MAX_HEIGHT / source.height,
    2,
  )
  return {
    width: Math.max(1, Math.round(source.width * scale)),
    height: Math.max(1, Math.round(source.height * scale)),
  }
}

function resizeCrop(start: CropRect, mode: CropDragMode, dx: number, dy: number): CropRect {
  if (mode === 'move') {
    return {
      ...start,
      x: clamp(start.x + dx, 0, 1 - start.width),
      y: clamp(start.y + dy, 0, 1 - start.height),
    }
  }

  let { x, y, width, height } = start
  const right = start.x + start.width
  const bottom = start.y + start.height

  if (mode.includes('e')) {
    width = clamp(start.width + dx, BACKGROUND_MIN_CROP_SIZE, 1 - start.x)
  }
  if (mode.includes('s')) {
    height = clamp(start.height + dy, BACKGROUND_MIN_CROP_SIZE, 1 - start.y)
  }
  if (mode.includes('w')) {
    x = clamp(start.x + dx, 0, right - BACKGROUND_MIN_CROP_SIZE)
    width = right - x
  }
  if (mode.includes('n')) {
    y = clamp(start.y + dy, 0, bottom - BACKGROUND_MIN_CROP_SIZE)
    height = bottom - y
  }

  return { x, y, width, height }
}

function loadCropSource(file: File): Promise<CropSource> {
  return new Promise((resolve, reject) => {
    const objectUrl = URL.createObjectURL(file)
    const image = new window.Image()
    image.onload = () => {
      const width = image.naturalWidth || image.width
      const height = image.naturalHeight || image.height
      if (width <= 0 || height <= 0) {
        URL.revokeObjectURL(objectUrl)
        reject(new Error('invalid image'))
        return
      }
      resolve({ file, url: objectUrl, width, height })
    }
    image.onerror = () => {
      URL.revokeObjectURL(objectUrl)
      reject(new Error('image load failed'))
    }
    image.src = objectUrl
  })
}

function encodeBackgroundCanvas(canvas: HTMLCanvasElement): string {
  const candidates: Array<[string, number]> = [
    ['image/webp', 0.86],
    ['image/jpeg', 0.82],
    ['image/jpeg', 0.72],
    ['image/jpeg', 0.62],
  ]
  let fallback = ''
  for (const [mimeType, quality] of candidates) {
    const dataUrl = canvas.toDataURL(mimeType, quality)
    fallback = dataUrl
    if (dataUrl.length <= BACKGROUND_STORED_MAX_CHARS) return dataUrl
  }
  return fallback
}

function cropImageToDataUrl(source: CropSource, crop: CropRect): Promise<string> {
  return new Promise((resolve, reject) => {
    const image = new window.Image()
    image.onload = () => {
      try {
        const sourceX = Math.round(crop.x * source.width)
        const sourceY = Math.round(crop.y * source.height)
        const sourceWidth = Math.max(1, Math.round(crop.width * source.width))
        const sourceHeight = Math.max(1, Math.round(crop.height * source.height))
        const scale = Math.min(1, BACKGROUND_MAX_SIDE / Math.max(sourceWidth, sourceHeight))
        const canvas = document.createElement('canvas')
        canvas.width = Math.max(1, Math.round(sourceWidth * scale))
        canvas.height = Math.max(1, Math.round(sourceHeight * scale))
        const ctx = canvas.getContext('2d')
        if (!ctx) {
          reject(new Error('canvas unavailable'))
          return
        }
        ctx.drawImage(
          image,
          sourceX,
          sourceY,
          sourceWidth,
          sourceHeight,
          0,
          0,
          canvas.width,
          canvas.height,
        )
        resolve(encodeBackgroundCanvas(canvas))
      } catch (err) {
        reject(err)
      }
    }
    image.onerror = () => reject(new Error('image load failed'))
    image.src = source.url
  })
}

function dataUrlMimeType(dataUrl: string): string {
  const match = /^data:([^;]+);base64,/.exec(dataUrl)
  return match?.[1] ?? 'image/jpeg'
}

function BackgroundImagePicker() {
  const { t } = useLocale()
  const {
    backgroundImage,
    setBackgroundImage,
    backgroundImageOpacity,
    setBackgroundImageOpacity,
  } = useAppearance()
  const fileInputRef = useRef<HTMLInputElement>(null)
  const cropMeasureRef = useRef<HTMLDivElement>(null)
  const cropStageRef = useRef<HTMLDivElement>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [cropSource, setCropSource] = useState<CropSource | null>(null)
  const [crop, setCrop] = useState<CropRect>(() => fullCrop())
  const [cropDrag, setCropDrag] = useState<CropDragState | null>(null)
  const [cropContainerWidth, setCropContainerWidth] = useState(0)
  const [expanded, setExpanded] = useState(false)
  const [opacityOpen, setOpacityOpen] = useState(false)
  const stageSize = cropStageSize(cropSource, cropContainerWidth)

  useEffect(() => {
    const el = cropMeasureRef.current
    if (!el) return
    const updateWidth = () => setCropContainerWidth(el.clientWidth)
    updateWidth()
    const resizeObserver = new ResizeObserver(updateWidth)
    resizeObserver.observe(el)
    return () => resizeObserver.disconnect()
  }, [cropSource])

  useEffect(() => {
    return () => {
      if (cropSource) URL.revokeObjectURL(cropSource.url)
    }
  }, [cropSource])

  const handleFileChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file) return
    if (!BACKGROUND_ALLOWED_TYPES.has(file.type)) {
      setError(t.backgroundImageInvalid)
      return
    }
    if (file.size > BACKGROUND_SOURCE_MAX_BYTES) {
      setError(t.backgroundImageTooLarge)
      return
    }

    setBusy(true)
    setError(null)
    try {
      const source = await loadCropSource(file)
      setCropSource(source)
      setCrop(fullCrop())
      setExpanded(true)
      setOpacityOpen(false)
    } catch {
      setError(t.backgroundImageLoadFailed)
    } finally {
      setBusy(false)
    }
  }

  const handleApplyCrop = async () => {
    if (!cropSource) return
    setBusy(true)
    setError(null)
    try {
      const dataUrl = await cropImageToDataUrl(cropSource, crop)
      if (dataUrl.length > BACKGROUND_STORED_MAX_CHARS) {
        setError(t.backgroundImageTooLarge)
        return
      }
      const image: ThemeBackgroundImage = {
        dataUrl,
        name: cropSource.file.name,
        mimeType: dataUrlMimeType(dataUrl),
        size: cropSource.file.size,
        updatedAt: Date.now(),
      }
      if (!setBackgroundImage(image)) {
        setError(t.backgroundImageSaveFailed)
        return
      }
      setCropSource(null)
      setExpanded(true)
      setOpacityOpen(false)
    } catch {
      setError(t.backgroundImageLoadFailed)
    } finally {
      setBusy(false)
    }
  }

  const handleRemove = () => {
    if (setBackgroundImage(null)) {
      setError(null)
      setExpanded(false)
      setOpacityOpen(false)
    } else {
      setError(t.backgroundImageSaveFailed)
    }
  }

  const handleCancelCrop = () => {
    setCropSource(null)
    setCrop(fullCrop())
    setCropDrag(null)
    setExpanded(Boolean(backgroundImage))
    setOpacityOpen(false)
  }

  const handleSummaryClick = () => {
    if (!backgroundImage || cropSource) return
    setExpanded((value) => !value)
    setOpacityOpen(false)
  }

  const handleOpacityClick = () => {
    const next = !opacityOpen
    setOpacityOpen(next)
    if (next) setExpanded(true)
  }

  const handleCropPointerDown = (mode: CropDragMode, e: React.PointerEvent) => {
    if (!cropSource || stageSize.width <= 0 || stageSize.height <= 0) return
    e.preventDefault()
    e.stopPropagation()
    cropStageRef.current?.setPointerCapture(e.pointerId)
    setCropDrag({
      mode,
      startX: e.clientX,
      startY: e.clientY,
      startCrop: crop,
    })
  }

  const handleCropPointerMove = (e: React.PointerEvent) => {
    if (!cropDrag || stageSize.width <= 0 || stageSize.height <= 0) return
    const dx = (e.clientX - cropDrag.startX) / stageSize.width
    const dy = (e.clientY - cropDrag.startY) / stageSize.height
    setCrop(resizeCrop(cropDrag.startCrop, cropDrag.mode, dx, dy))
  }

  const handleCropPointerUp = (e: React.PointerEvent) => {
    const stage = cropStageRef.current
    if (stage?.hasPointerCapture(e.pointerId)) stage.releasePointerCapture(e.pointerId)
    setCropDrag(null)
  }

  const cropStyle = stageSize.width > 0 && stageSize.height > 0
    ? {
      left: `${crop.x * stageSize.width}px`,
      top: `${crop.y * stageSize.height}px`,
      width: `${crop.width * stageSize.width}px`,
      height: `${crop.height * stageSize.height}px`,
    }
    : undefined
  const activeImageUrl = cropSource?.url ?? backgroundImage?.dataUrl
  const previewSurface = activeImageUrl
    ? backgroundCssUrl(activeImageUrl)
    : undefined
  const previewBackgroundImage = activeImageUrl
    ? [
      'linear-gradient(rgb(var(--c-background-image-scrim-rgb) / var(--c-background-image-scrim-alpha)), rgb(var(--c-background-image-scrim-rgb) / var(--c-background-image-scrim-alpha)))',
      'radial-gradient(ellipse at center, rgb(var(--c-background-image-scrim-rgb) / 0) 36%, rgb(var(--c-background-image-scrim-rgb) / var(--c-background-image-vignette-alpha)) 100%)',
      previewSurface,
    ].join(', ')
    : undefined
  const previewBackgroundPosition = activeImageUrl ? 'center, center, center' : 'center'
  const previewBackgroundSize = activeImageUrl ? '100% 100%, 100% 100%, cover' : 'cover'
  const thumbnailBackgroundImage = activeImageUrl ? previewSurface : undefined
  const displayName = cropSource?.file.name ?? backgroundImage?.name ?? t.backgroundImageEmpty
  const rangeProgress = `${backgroundImageOpacity}%`

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-[var(--c-text-secondary)]">{t.backgroundImageSection}</span>
        <div className="flex items-center gap-1">
          <button
            type="button"
            aria-label={t.backgroundImageSection}
            title={t.backgroundImageSection}
            onClick={() => fileInputRef.current?.click()}
            disabled={busy}
            className={BACKGROUND_ICON_BUTTON_CLS}
          >
            <Upload size={14} />
          </button>
          {backgroundImage && !cropSource && (
            <button
              type="button"
              aria-label={t.backgroundImageOpacity}
              aria-expanded={opacityOpen}
              title={t.backgroundImageOpacity}
              onClick={handleOpacityClick}
              className={`${BACKGROUND_ICON_BUTTON_CLS} ${opacityOpen ? 'bg-[var(--c-bg-deep)] text-[var(--c-text-primary)]' : ''}`}
            >
              <SlidersHorizontal size={14} />
            </button>
          )}
          {backgroundImage && (
            <button
              type="button"
              aria-label={t.backgroundImageRemove}
              title={t.backgroundImageRemove}
              onClick={handleRemove}
              className={BACKGROUND_ICON_BUTTON_CLS}
            >
              <Trash2 size={14} />
            </button>
          )}
        </div>
      </div>
      <input ref={fileInputRef} type="file" accept={BACKGROUND_ACCEPT} className="hidden" onChange={handleFileChange} />
      <div className="overflow-hidden rounded-lg bg-[var(--c-bg-menu)] ring-1 ring-[var(--c-border-subtle)]">
        <div className="flex min-h-12 w-full items-center gap-3 px-3 text-left">
          <button
            type="button"
            aria-label={backgroundImage ? t.backgroundImageSection : t.backgroundImageEmpty}
            aria-expanded={backgroundImage ? expanded : undefined}
            title={backgroundImage ? t.backgroundImageSection : t.backgroundImageEmpty}
            onClick={handleSummaryClick}
            disabled={busy || !backgroundImage || !!cropSource}
            className="group flex min-w-0 flex-1 items-center gap-3 rounded-md py-2 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--c-accent)] disabled:cursor-default"
          >
            <span
              className="relative h-8 w-12 shrink-0 overflow-hidden rounded-md bg-[var(--c-bg-input)] ring-1 ring-[var(--c-border-subtle)]"
              style={{
                backgroundColor: 'var(--c-bg-page)',
                backgroundImage: thumbnailBackgroundImage,
                backgroundPosition: 'center',
                backgroundSize: 'cover',
              }}
            >
              {!activeImageUrl && (
                <span className="absolute inset-0 flex items-center justify-center text-[var(--c-text-muted)]">
                  <ImageIcon size={13} />
                </span>
              )}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm text-[var(--c-text-heading)]">
                {displayName}
              </span>
            </span>
          </button>
          {backgroundImage && !opacityOpen && (
            <span className="shrink-0 text-xs tabular-nums text-[var(--c-text-tertiary)]">
              {backgroundImageOpacity}%
            </span>
          )}
        </div>

        {expanded && backgroundImage && !cropSource && (
          <div
            className="relative h-28 w-full overflow-hidden bg-[var(--c-bg-input)]"
            style={{
              borderTop: '0.5px solid var(--c-border-subtle)',
              backgroundColor: 'var(--c-bg-page)',
              backgroundImage: previewBackgroundImage,
              backgroundPosition: previewBackgroundPosition,
              backgroundSize: previewBackgroundSize,
            }}
          />
        )}

        {opacityOpen && backgroundImage && !cropSource && (
          <div className="px-3 pb-3" style={{ borderTop: '0.5px solid var(--c-border-subtle)' }}>
            <div className="mb-2 mt-2 flex items-center justify-between gap-3">
              <span className="text-xs text-[var(--c-text-tertiary)]">{t.backgroundImageOpacity}</span>
              <span className="text-xs tabular-nums text-[var(--c-text-tertiary)]">
                {backgroundImageOpacity}%
              </span>
            </div>
            <input
              type="range"
              aria-label={t.backgroundImageOpacity}
              min={0}
              max={100}
              step={1}
              value={backgroundImageOpacity}
              onChange={(e) => setBackgroundImageOpacity(Number(e.target.value))}
              className="background-opacity-range h-1.5 w-full cursor-pointer appearance-none rounded-full"
              style={{
                background: `linear-gradient(to right, var(--c-accent) 0%, var(--c-accent) ${rangeProgress}, var(--c-border-subtle) ${rangeProgress}, var(--c-border-subtle) 100%)`,
              }}
            />
          </div>
        )}

        {error && (
          <div className="min-w-0 truncate px-3 py-2 text-xs text-[var(--c-status-error-text)]" style={{ borderTop: '0.5px solid var(--c-border-subtle)' }}>
            {error}
          </div>
        )}

        {cropSource && (
          <div className="overflow-hidden" style={{ borderTop: '0.5px solid var(--c-border-subtle)' }}>
            <div className="flex items-center justify-between gap-3 px-3 py-2.5">
              <div className="min-w-0 truncate text-xs text-[var(--c-text-tertiary)]">
                {cropSource.file.name}
              </div>
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  aria-label={t.backgroundImageReset}
                  title={t.backgroundImageReset}
                  onClick={() => setCrop(fullCrop())}
                  className={BACKGROUND_ICON_BUTTON_CLS}
                >
                  <RotateCcw size={14} />
                </button>
                <button
                  type="button"
                  aria-label={t.backgroundImageCancel}
                  title={t.backgroundImageCancel}
                  onClick={handleCancelCrop}
                  disabled={busy}
                  className={BACKGROUND_ICON_BUTTON_CLS}
                >
                  <X size={14} />
                </button>
                <button
                  type="button"
                  aria-label={t.backgroundImageApply}
                  title={t.backgroundImageApply}
                  onClick={handleApplyCrop}
                  disabled={busy}
                  className={BACKGROUND_PRIMARY_ICON_BUTTON_CLS}
                  style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
                >
                  <Check size={14} />
                </button>
              </div>
            </div>
            <div ref={cropMeasureRef} className="flex justify-center bg-[color-mix(in_srgb,var(--c-bg-page)_82%,var(--c-bg-menu))] px-3 pb-3 pt-1">
              <div
                ref={cropStageRef}
                className="relative overflow-hidden rounded-lg"
                onPointerMove={handleCropPointerMove}
                onPointerUp={handleCropPointerUp}
                onPointerCancel={handleCropPointerUp}
                style={{
                  width: stageSize.width,
                  height: stageSize.height,
                  backgroundColor: 'var(--c-bg-deep)',
                  backgroundImage: backgroundCssUrl(cropSource.url),
                  backgroundSize: '100% 100%',
                  boxShadow: '0 0 0 0.5px var(--c-border-subtle), 0 12px 34px rgba(0, 0, 0, 0.18)',
                  touchAction: 'none',
                }}
              >
                {cropStyle && (
                  <div
                    className="absolute"
                    onPointerDown={(e) => handleCropPointerDown('move', e)}
                    style={{
                      ...cropStyle,
                      cursor: cropDrag?.mode === 'move' ? 'grabbing' : 'grab',
                      border: '1.5px solid var(--c-accent)',
                      boxShadow: '0 0 0 9999px rgba(0,0,0,0.48), inset 0 0 0 0.5px rgba(255,255,255,0.35)',
                    }}
                  >
                    {CROP_HANDLES.map(({ mode, cursor, className }) => (
                      <button
                        key={mode}
                        type="button"
                        aria-label={mode}
                        onPointerDown={(e) => handleCropPointerDown(mode, e)}
                        className={`absolute h-3.5 w-3.5 rounded-full shadow-sm ${className}`}
                        style={{
                          cursor,
                          background: 'var(--c-accent)',
                          border: '0.5px solid var(--c-bg-page)',
                        }}
                      />
                    ))}
                  </div>
                )}
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

export function ThemePresetPicker({
  onEditColors,
  showTitle = true,
}: {
  onEditColors: () => void
  showTitle?: boolean
}) {
  const { t } = useLocale()
  const {
    themePreset, setThemePreset,
    customThemeId, setActiveCustomTheme,
    customThemes, deleteCustomTheme,
    backgroundImage,
  } = useAppearance()

  const builtins: { id: ThemePreset; name: string; def: ThemeDefinition | null }[] = [
    { id: 'default',      name: t.themePresetDefault, def: null },
    { id: 'terra',        name: 'Terra',              def: BUILTIN_PRESETS['terra'] },
    { id: 'github',       name: 'GitHub',             def: BUILTIN_PRESETS['github'] },
    { id: 'nord',         name: 'Nord',               def: BUILTIN_PRESETS['nord'] },
    { id: 'catppuccin',   name: 'Catppuccin',         def: BUILTIN_PRESETS['catppuccin'] },
    { id: 'tokyo-night',  name: 'Tokyo Night',        def: BUILTIN_PRESETS['tokyo-night'] },
    { id: 'retina-burn',  name: t.themePresetRetinaBurn, def: BUILTIN_PRESETS['retina-burn'] },
    { id: 'background-image', name: t.themePresetCustomBackground, def: null },
  ]

  const defaultDark = { '--c-bg-page': '#1e1d1c', '--c-bg-sidebar': '#242422', '--c-accent': '#faf9f6', '--c-text-primary': '#faf9f5' } as const
  const customThemeList = Object.values(customThemes)

  return (
    <div className="flex flex-col gap-4">
      {showTitle && (
        <div className="flex items-center justify-between">
          <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.themePresetSection}</span>
          <button
            type="button"
            onClick={onEditColors}
            className="text-xs text-[var(--c-text-secondary)] transition-colors hover:text-[var(--c-text-primary)]"
          >
            {t.editColors}
          </button>
        </div>
      )}

      {/* Built-in presets */}
      <div className="flex flex-wrap gap-3">
        {builtins.map(({ id, name, def }) => {
          const dark = def ? (def.dark as typeof defaultDark) : defaultDark
          const imageUrl = id === 'background-image' ? backgroundImage?.dataUrl ?? null : undefined
          return (
            <PresetCard
              key={id}
              name={name}
              dark={dark}
              active={themePreset === id}
              onClick={() => setThemePreset(id)}
              imageUrl={imageUrl}
            />
          )
        })}
      </div>
      {!showTitle && (
        <SettingsButton
          type="button"
          onClick={onEditColors}
          className="self-start"
        >
          {t.editColors}
        </SettingsButton>
      )}

      {/* Custom themes */}
      {customThemeList.length > 0 && (
        <div className="flex flex-col gap-2">
          <span className="text-xs text-[var(--c-text-tertiary)]">{t.myThemes}</span>
          <div className="flex flex-wrap gap-3">
            {customThemeList.map((theme) => (
              <div key={theme.id} className="relative group">
                <PresetCard
                  name={theme.name}
                  dark={theme.dark as typeof defaultDark}
                  active={themePreset === 'custom' && customThemeId === theme.id}
                  onClick={() => setActiveCustomTheme(theme.id)}
                />
                <button
                  type="button"
                  onClick={(e) => { e.stopPropagation(); deleteCustomTheme(theme.id) }}
                  className="absolute -right-1.5 -top-1.5 hidden group-hover:flex items-center justify-center rounded-full transition-colors"
                  style={{
                    width: '18px', height: '18px',
                    background: 'var(--c-status-danger-bg)',
                    border: '0.5px solid var(--c-border-subtle)',
                  }}
                >
                  <Trash2 size={10} style={{ color: 'var(--c-status-error-text)' }} />
                </button>
              </div>
            ))}
          </div>
        </div>
      )}

      {themePreset === 'background-image' && <BackgroundImagePicker />}
    </div>
  )
}
