import { useRef, useEffect, useCallback, useMemo, useState } from 'react'
import { Plus, ArrowUp, Square, Paperclip, Mic, X, Check, Loader2, BookOpen, Search } from 'lucide-react'
import type { FormEvent, KeyboardEvent, ClipboardEvent as ReactClipboardEvent } from 'react'
import { listSelectablePersonas, transcribeAudio, type SelectablePersona, type UploadedThreadAttachment } from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { PastedContentModal } from './PastedContentModal'
import { ModelPicker } from './ModelPicker'
import type { SettingsTab } from './SettingsModal'
import {
  DEFAULT_PERSONA_KEY,
  SEARCH_PERSONA_KEY,
  LEARNING_PERSONA_KEY,
  readSelectedPersonaKeyFromStorage,
  writeSelectedPersonaKeyToStorage,
} from '../storage'

export type Attachment = {
  id: string
  file: File
  name: string
  size: number
  mime_type: string
  preview_url?: string
  status: 'uploading' | 'ready' | 'error'
  uploaded?: UploadedThreadAttachment
  pasted?: { text: string; lineCount: number }
}

type Props = {
  value: string
  onChange: (val: string) => void
  onSubmit: (e: FormEvent<HTMLFormElement>, personaKey: string, modelOverride?: string) => void
  onCancel?: () => void
  placeholder?: string
  disabled?: boolean
  isStreaming?: boolean
  canCancel?: boolean
  cancelSubmitting?: boolean
  variant?: 'welcome' | 'chat'
  searchMode?: boolean
  attachments?: Attachment[]
  onAttachFiles?: (files: File[]) => void
  onPasteContent?: (text: string) => void
  onRemoveAttachment?: (id: string) => void
  accessToken?: string
  onAsrError?: (error: unknown) => void
  onPersonaChange?: (personaKey: string) => void
  onOpenSettings?: (tab: SettingsTab) => void
}

const FALLBACK_SELECTOR_NAMES: Record<string, string> = {
  [DEFAULT_PERSONA_KEY]: 'Normal',
  [SEARCH_PERSONA_KEY]: 'Search',
}

function buildFallbackSelectablePersonas(selectedPersonaKey: string): SelectablePersona[] {
  const keys = [DEFAULT_PERSONA_KEY]
  if (selectedPersonaKey !== DEFAULT_PERSONA_KEY) keys.push(selectedPersonaKey)
  return keys.map((personaKey, index) => ({
    persona_key: personaKey,
    selector_name: FALLBACK_SELECTOR_NAMES[personaKey] ?? personaKey,
    selector_order: index,
  }))
}

function pickPreferredPersonaKey(personas: SelectablePersona[], preferred?: string): string {
  if (preferred && personas.some((persona) => persona.persona_key === preferred)) return preferred
  if (personas.some((persona) => persona.persona_key === DEFAULT_PERSONA_KEY)) return DEFAULT_PERSONA_KEY
  return personas[0]?.persona_key ?? DEFAULT_PERSONA_KEY
}
function hasTransferFiles(dataTransfer?: DataTransfer | null): boolean {
  if (!dataTransfer) return false
  if (Array.from(dataTransfer.types).includes('Files')) return true
  if (dataTransfer.files.length > 0) return true
  return Array.from(dataTransfer.items).some((item) => item.kind === 'file')
}

function extractFilesFromTransfer(dataTransfer?: DataTransfer | null): File[] {
  if (!dataTransfer) return []
  const files: File[] = []
  const seenTypes = new Set<string>()

  const itemFiles = Array.from(dataTransfer.items)
    .filter((item) => item.kind === 'file')
    .map((item) => item.getAsFile())
    .filter((f): f is File => f != null)

  const dtFiles = Array.from(dataTransfer.files)

  const allFiles = itemFiles.length > 0 ? itemFiles : dtFiles

  for (const file of allFiles) {
    const prefix = file.type.split('/')[0]
    if (prefix === 'image') {
      if (seenTypes.has('image')) continue
      seenTypes.add('image')
    }
    files.push(file)
  }
  return files
}

function isEditableElement(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  if (target.isContentEditable) return true
  const tagName = target.tagName
  return tagName === 'INPUT' || tagName === 'TEXTAREA' || tagName === 'SELECT'
}

export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

const BAR_COUNT = 52

function AttachmentCard({ attachment, onRemove }: { attachment: Attachment; onRemove: () => void }) {
  const [imageLoaded, setImageLoaded] = useState(false)
  const [lineCount, setLineCount] = useState<number | null>(null)
  const [cardHovered, setCardHovered] = useState(false)
  const isImage = attachment.mime_type.startsWith('image/')

  useEffect(() => {
    if (isImage) return
    const reader = new FileReader()
    reader.onload = (e) => {
      const text = e.target?.result as string
      setLineCount(text.split('\n').length)
    }
    reader.readAsText(attachment.file)
  }, [attachment.file, isImage])

  const ext = attachment.name.includes('.')
    ? attachment.name.split('.').pop()!.toUpperCase()
    : ''
  const uploading = attachment.status === 'uploading'
  const ready = !uploading && (isImage ? imageLoaded : lineCount !== null)

  return (
    <div style={{ position: 'relative', flexShrink: 0 }}
      onMouseEnter={() => setCardHovered(true)}
      onMouseLeave={() => setCardHovered(false)}
    >
      <div
        style={{
          width: '120px',
          height: '120px',
          borderRadius: '10px',
          background: 'var(--c-attachment-bg)',
          overflow: 'hidden',
          borderWidth: '0.7px',
          borderStyle: 'solid',
          borderColor: cardHovered ? 'var(--c-attachment-border-hover)' : 'var(--c-attachment-border)',
          transition: 'border-color 0.2s ease',
        }}
      >
        {!ready && (
          <div style={{
            position: 'absolute', inset: 0, padding: '10px',
            display: 'flex', flexDirection: 'column', gap: '8px',
          }}>
            <div className="attachment-shimmer" style={{ width: '80%', height: '10px', borderRadius: '5px' }} />
            <div className="attachment-shimmer" style={{ width: '55%', height: '10px', borderRadius: '5px' }} />
            <div style={{ flex: 1 }} />
            <div className="attachment-shimmer" style={{ width: '30%', height: '10px', borderRadius: '5px' }} />
          </div>
        )}

        {isImage ? (
          <img
            src={attachment.preview_url}
            alt={attachment.name}
            onLoad={() => setImageLoaded(true)}
            style={{
              width: '100%',
              height: '100%',
              objectFit: 'cover',
              opacity: ready ? 1 : 0,
              transition: 'opacity 0.2s ease',
              display: 'block',
            }}
          />
        ) : (
          <div style={{
            padding: '10px',
            display: 'flex', flexDirection: 'column',
            height: '100%',
            opacity: ready ? 1 : 0,
            transition: 'opacity 0.2s ease',
          }}>
            <span style={{
              color: 'var(--c-text-heading)',
              fontSize: '12px',
              fontWeight: 300,
              lineHeight: '1.35',
              wordBreak: 'break-all',
              display: '-webkit-box',
              WebkitLineClamp: 3,
              WebkitBoxOrient: 'vertical',
              overflow: 'hidden',
            }}>
              {attachment.name}
            </span>
            {lineCount !== null && (
              <span style={{ color: 'var(--c-text-muted)', fontSize: '11px', marginTop: '3px' }}>
                {lineCount} lines
              </span>
            )}
            <div style={{ flex: 1 }} />
            {ext && (
              <span style={{
                alignSelf: 'flex-start',
                padding: '2px 6px',
                borderRadius: '5px',
                background: 'var(--c-attachment-bg)',
                border: '0.5px solid var(--c-attachment-badge-border)',
                color: 'var(--c-text-secondary)',
                fontSize: '10px',
                fontWeight: 500,
              }}>
                {ext}
              </span>
            )}
          </div>
        )}
      </div>

      {/* 关闭按钮：浮动圆形，hover 显示 */}
      <button
        type="button"
        className="attachment-close-btn"
        onClick={(e) => { e.stopPropagation(); onRemove() }}
        style={{
          position: 'absolute',
          top: '-5px',
          left: '-5px',
          width: '18px',
          height: '18px',
          borderRadius: '50%',
          background: 'var(--c-attachment-close-bg)',
          border: '0.5px solid var(--c-attachment-close-border)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          cursor: 'pointer',
          opacity: cardHovered ? 1 : 0,
          transition: 'opacity 0.15s ease',
          pointerEvents: cardHovered ? 'auto' : 'none',
          zIndex: 1,
        }}
      >
        <X size={9} />
      </button>
    </div>
  )
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function PastedContentCard({
  attachment,
  onRemove,
  onClick,
}: {
  attachment: Attachment
  onRemove: () => void
  onClick: () => void
}) {
  const [cardHovered, setCardHovered] = useState(false)
  const uploading = attachment.status === 'uploading'
  const text = attachment.pasted?.text ?? ''

  return (
    <div style={{ position: 'relative', flexShrink: 0 }}
      onMouseEnter={() => setCardHovered(true)}
      onMouseLeave={() => setCardHovered(false)}
    >
      <div
        onClick={onClick}
        style={{
          width: '120px',
          height: '120px',
          borderRadius: '10px',
          background: uploading ? 'var(--c-attachment-bg)' : 'var(--c-bg-page)',
          overflow: 'hidden',
          borderWidth: '0.7px',
          borderStyle: 'solid',
          borderColor: cardHovered ? 'var(--c-attachment-border-hover)' : 'var(--c-attachment-border)',
          transition: 'border-color 0.2s ease, background 0.2s ease',
          cursor: 'pointer',
          display: 'flex',
          flexDirection: 'column',
          padding: '10px',
        }}
      >
        {uploading ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', flex: 1 }}>
            <div className="attachment-shimmer" style={{ width: '90%', height: '10px', borderRadius: '5px' }} />
            <div className="attachment-shimmer" style={{ width: '70%', height: '10px', borderRadius: '5px' }} />
            <div className="attachment-shimmer" style={{ width: '50%', height: '10px', borderRadius: '5px' }} />
          </div>
        ) : (
          <>
            <div style={{
              flex: 1,
              overflow: 'hidden',
              color: 'var(--c-text-secondary)',
              fontSize: '11px',
              lineHeight: '1.4',
              display: '-webkit-box',
              WebkitLineClamp: 4,
              WebkitBoxOrient: 'vertical',
              wordBreak: 'break-all',
            }}>
              {text}
            </div>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginTop: '4px' }}>
              <span style={{
                fontSize: '9px',
                color: 'var(--c-text-muted)',
                whiteSpace: 'nowrap',
              }}>
                {formatSize(attachment.size)}
              </span>
              <span style={{
                padding: '1px 6px',
                borderRadius: '5px',
                background: 'var(--c-attachment-bg)',
                border: '0.5px solid var(--c-attachment-badge-border)',
                color: 'var(--c-text-secondary)',
                fontSize: '10px',
                fontWeight: 500,
              }}>
                PASTED
              </span>
            </div>
          </>
        )}
      </div>

      <button
        type="button"
        className="attachment-close-btn"
        onClick={(e) => { e.stopPropagation(); onRemove() }}
        style={{
          position: 'absolute',
          top: '-5px',
          left: '-5px',
          width: '18px',
          height: '18px',
          borderRadius: '50%',
          background: 'var(--c-attachment-close-bg)',
          border: '0.5px solid var(--c-attachment-close-border)',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          cursor: 'pointer',
          opacity: cardHovered ? 1 : 0,
          transition: 'opacity 0.15s ease',
          pointerEvents: cardHovered ? 'auto' : 'none',
          zIndex: 1,
        }}
      >
        <X size={9} />
      </button>
    </div>
  )
}

export function ChatInput({
  value,
  onChange,
  onSubmit,
  onCancel,
  placeholder = '输入消息...',
  disabled = false,
  isStreaming = false,
  canCancel = false,
  cancelSubmitting = false,
  variant = 'chat',
  searchMode = false,
  attachments = [],
  onAttachFiles,
  onPasteContent,
  onRemoveAttachment,
  accessToken,
  onAsrError,
  onPersonaChange,
  onOpenSettings,
}: Props) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const plusBtnRef = useRef<HTMLButtonElement>(null)
  const mediaRecorderRef = useRef<MediaRecorder | null>(null)
  const audioChunksRef = useRef<Blob[]>([])
  const analyserRef = useRef<AnalyserNode | null>(null)
  const waveformHistoryRef = useRef<number[]>(Array(BAR_COUNT).fill(0))
  const animFrameRef = useRef<number>(0)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const discardRef = useRef(false)
  const dragDepthRef = useRef(0)
  // stable refs so closures inside startRecording always see latest values
  const valueRef = useRef(value)
  const onChangeRef = useRef(onChange)
  const accessTokenRef = useRef(accessToken)
  const onAsrErrorRef = useRef(onAsrError)
  useEffect(() => { valueRef.current = value }, [value])
  useEffect(() => { onChangeRef.current = onChange }, [onChange])
  useEffect(() => { accessTokenRef.current = accessToken }, [accessToken])
  useEffect(() => { onAsrErrorRef.current = onAsrError }, [onAsrError])

  const { t } = useLocale()

  const [menuOpen, setMenuOpen] = useState(false)
  const [selectablePersonas, setSelectablePersonas] = useState<SelectablePersona[]>([])
  const [selectedPersonaKey, setSelectedPersonaKey] = useState(readSelectedPersonaKeyFromStorage)
  const [focused, setFocused] = useState(false)
  const [isRecording, setIsRecording] = useState(false)
  const [isTranscribing, setIsTranscribing] = useState(false)
  const [recordingSeconds, setRecordingSeconds] = useState(0)
  const [waveformBars, setWaveformBars] = useState<number[]>(Array(BAR_COUNT).fill(0))
  const [isFileDragging, setIsFileDragging] = useState(false)
  const [collapsingGrid, setCollapsingGrid] = useState(false)
  const [pastedModalAttachment, setPastedModalAttachment] = useState<Attachment | null>(null)
  const lastPasteRef = useRef(0)
  const pasteProcessingRef = useRef(false)

  // cleanup on unmount
  useEffect(() => {
    return () => {
      cancelAnimationFrame(animFrameRef.current)
      if (timerRef.current) clearInterval(timerRef.current)
    }
  }, [])

  const persistSelectedPersona = useCallback((personaKey: string) => {
    setSelectedPersonaKey(personaKey)
    writeSelectedPersonaKeyToStorage(personaKey)
    onPersonaChange?.(personaKey)
  }, [onPersonaChange])

  useEffect(() => {
    let cancelled = false

    if (!accessToken) {
      setSelectablePersonas([])
      return () => { cancelled = true }
    }

    void listSelectablePersonas(accessToken)
      .then((personas) => {
        if (cancelled) return
        setSelectablePersonas(personas)
        if (personas.length === 0) return

        const preferredKey = readSelectedPersonaKeyFromStorage()
        const nextKey = pickPreferredPersonaKey(personas, preferredKey)
        if (nextKey !== preferredKey) persistSelectedPersona(nextKey)
      })
      .catch(() => {
        if (cancelled) return
        setSelectablePersonas([])
      })

    return () => { cancelled = true }
  }, [accessToken, persistSelectedPersona])

  const personas = useMemo(
    () => selectablePersonas.length > 0
      ? selectablePersonas
      : buildFallbackSelectablePersonas(selectedPersonaKey),
    [selectablePersonas, selectedPersonaKey],
  )

  const selectedPersona = useMemo(
    () => personas.find((persona) => persona.persona_key === selectedPersonaKey) ?? null,
    [personas, selectedPersonaKey],
  )

  const [chipExiting, setChipExiting] = useState(false)
  const [selectedModel, setSelectedModel] = useState<string | null>(null)

  const isNonDefaultMode = selectedPersonaKey !== DEFAULT_PERSONA_KEY

  const deactivateMode = useCallback(() => {
    setChipExiting(true)
    setTimeout(() => {
      persistSelectedPersona(DEFAULT_PERSONA_KEY)
      setChipExiting(false)
    }, 120)
  }, [persistSelectedPersona])

  const handleModeSelect = useCallback((personaKey: string) => {
    if (selectedPersonaKey === personaKey && !chipExiting) {
      deactivateMode()
    } else {
      persistSelectedPersona(personaKey)
    }
    setMenuOpen(false)
  }, [selectedPersonaKey, chipExiting, persistSelectedPersona, deactivateMode])

  const formatRecordingTime = (secs: number) => {
    const m = Math.floor(secs / 60)
    const s = secs % 60
    return `${m}:${String(s).padStart(2, '0')}`
  }

  const startRecording = useCallback(async () => {
    if (isRecording || isTranscribing || !accessTokenRef.current) return

    let stream: MediaStream
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true })
    } catch {
      return
    }

    const audioCtx = new AudioContext()
    const analyser = audioCtx.createAnalyser()
    analyser.fftSize = 1024
    analyser.smoothingTimeConstant = 0.5
    audioCtx.createMediaStreamSource(stream).connect(analyser)
    analyserRef.current = analyser
    waveformHistoryRef.current = Array(BAR_COUNT).fill(0)

    const dataArray = new Float32Array(analyser.fftSize)
    let lastSample = 0
    const tick = () => {
      analyser.getFloatTimeDomainData(dataArray)
      const now = performance.now()
      if (now - lastSample >= 80) {
        lastSample = now
        let sum = 0
        for (let i = 0; i < dataArray.length; i++) sum += dataArray[i] ** 2
        const rms = Math.sqrt(sum / dataArray.length)
        const history = waveformHistoryRef.current
        history.shift()
        history.push(Math.min(1, rms * 8))
        setWaveformBars([...history])
      }
      animFrameRef.current = requestAnimationFrame(tick)
    }
    animFrameRef.current = requestAnimationFrame(tick)

    setRecordingSeconds(0)
    timerRef.current = setInterval(() => setRecordingSeconds((s) => s + 1), 1000)

    const recorder = new MediaRecorder(stream)
    mediaRecorderRef.current = recorder
    audioChunksRef.current = []
    discardRef.current = false

    recorder.ondataavailable = (e) => {
      if (e.data.size > 0) audioChunksRef.current.push(e.data)
    }

    recorder.onstop = async () => {
      cancelAnimationFrame(animFrameRef.current)
      if (timerRef.current) { clearInterval(timerRef.current); timerRef.current = null }
      try { audioCtx.close() } catch { /* ignore */ }
      stream.getTracks().forEach((t) => t.stop())
      setIsRecording(false)

      if (discardRef.current) {
        discardRef.current = false
        audioChunksRef.current = []
        setWaveformBars(Array(BAR_COUNT).fill(0))
        return
      }

      const token = accessTokenRef.current
      if (!token || audioChunksRef.current.length === 0) return

      // 取浏览器语言的 ISO-639-1 部分（"zh-CN" → "zh"）
      const lang = navigator.language?.split('-')[0] ?? undefined

      const blob = new Blob(audioChunksRef.current, { type: 'audio/webm' })
      setIsTranscribing(true)
      try {
        const result = await transcribeAudio(token, blob, 'audio.webm', lang)
        if (result.text) {
          const prev = valueRef.current
          onChangeRef.current(prev ? `${prev} ${result.text}` : result.text)
        }
      } catch (err) {
        onAsrErrorRef.current?.(err)
      } finally {
        setIsTranscribing(false)
        setWaveformBars(Array(BAR_COUNT).fill(0))
      }
    }

    recorder.start()
    setIsRecording(true)
  }, [isRecording, isTranscribing])

  const stopAndTranscribe = useCallback(() => {
    discardRef.current = false
    mediaRecorderRef.current?.stop()
  }, [])

  const cancelRecording = useCallback(() => {
    discardRef.current = true
    mediaRecorderRef.current?.stop()
  }, [])


  // 进入搜索模式时强制切到搜索人格
  useEffect(() => {
    if (searchMode && selectedPersonaKey !== SEARCH_PERSONA_KEY) {
      persistSelectedPersona(SEARCH_PERSONA_KEY)
    }
  }, [persistSelectedPersona, searchMode, selectedPersonaKey])

  const adjustHeight = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    const from = el.offsetHeight
    el.style.transition = 'none'
    el.style.overflow = 'hidden'
    el.style.height = 'auto'
    const to = Math.min(el.scrollHeight, 300)
    if (from === to) {
      el.style.overflow = 'auto'
      el.style.height = `${to}px`
      return
    }
    el.style.height = `${from}px`
    requestAnimationFrame(() => {
      el.style.transition = 'height 30ms cubic-bezier(0.25, 0.1, 0.25, 1)'
      el.style.height = `${to}px`
    })
    const restore = () => {
      el.style.overflow = 'auto'
      el.removeEventListener('transitionend', restore)
    }
    el.addEventListener('transitionend', restore, { once: true })
  }, [])

  useEffect(() => {
    adjustHeight()
  }, [value, adjustHeight])

  useEffect(() => {
    if (!menuOpen) return
    const handleClick = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node) ||
        plusBtnRef.current?.contains(e.target as Node)
      ) return
      setMenuOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [menuOpen])

  const handleAttachTransfer = useCallback((dataTransfer?: DataTransfer | null) => {
    if (pasteProcessingRef.current) return false
    const files = extractFilesFromTransfer(dataTransfer)
    if (files.length === 0 || !onAttachFiles) return false
    pasteProcessingRef.current = true
    onAttachFiles(files)
    textareaRef.current?.focus()
    setMenuOpen(false)
    requestAnimationFrame(() => { pasteProcessingRef.current = false })
    return true
  }, [onAttachFiles])

  useEffect(() => {
    if (!onAttachFiles) return

    const resetDragState = () => {
      dragDepthRef.current = 0
      setIsFileDragging(false)
    }

    const handleDragEnter = (e: DragEvent) => {
      if (!hasTransferFiles(e.dataTransfer)) return
      e.preventDefault()
      dragDepthRef.current += 1
      setIsFileDragging(true)
    }

    const handleDragOver = (e: DragEvent) => {
      if (!hasTransferFiles(e.dataTransfer)) return
      e.preventDefault()
      if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy'
      setIsFileDragging(true)
    }

    const handleDragLeave = (e: DragEvent) => {
      if (dragDepthRef.current === 0 && !hasTransferFiles(e.dataTransfer)) return
      e.preventDefault()
      dragDepthRef.current = Math.max(0, dragDepthRef.current - 1)
      if (dragDepthRef.current === 0) {
        setIsFileDragging(false)
      }
    }

    const handleDrop = (e: DragEvent) => {
      if (dragDepthRef.current === 0 && !hasTransferFiles(e.dataTransfer)) return
      e.preventDefault()
      handleAttachTransfer(e.dataTransfer)
      resetDragState()
    }

    const handleWindowBlur = () => {
      resetDragState()
    }

    window.addEventListener('dragenter', handleDragEnter)
    window.addEventListener('dragover', handleDragOver)
    window.addEventListener('dragleave', handleDragLeave)
    window.addEventListener('drop', handleDrop)
    window.addEventListener('blur', handleWindowBlur)

    return () => {
      window.removeEventListener('dragenter', handleDragEnter)
      window.removeEventListener('dragover', handleDragOver)
      window.removeEventListener('dragleave', handleDragLeave)
      window.removeEventListener('drop', handleDrop)
      window.removeEventListener('blur', handleWindowBlur)
      resetDragState()
    }
  }, [handleAttachTransfer, onAttachFiles])

  useEffect(() => {
    if (!onAttachFiles) return
    const handlePaste = (e: ClipboardEvent) => {
      if (e.target === textareaRef.current) return
      if (isEditableElement(e.target)) return
      if (!hasTransferFiles(e.clipboardData)) return
      if (pasteProcessingRef.current) { e.preventDefault(); return }
      const now = Date.now()
      if (now - lastPasteRef.current < 1000) { e.preventDefault(); return }
      lastPasteRef.current = now
      if (!handleAttachTransfer(e.clipboardData)) return
      e.preventDefault()
    }
    document.addEventListener('paste', handlePaste)
    return () => document.removeEventListener('paste', handlePaste)
  }, [handleAttachTransfer, onAttachFiles])

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      if (!disabled && value.trim()) {
        e.currentTarget.form?.requestSubmit()
      }
    }
  }

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? [])
    if (files.length > 0) onAttachFiles?.(files)
    e.target.value = ''
    setMenuOpen(false)
  }

  const PASTE_LINE_THRESHOLD = 20

  const handleTextareaPaste = (e: ReactClipboardEvent<HTMLTextAreaElement>) => {
    if (hasTransferFiles(e.clipboardData)) {
      if (pasteProcessingRef.current) { e.preventDefault(); return }
      const now = Date.now()
      if (now - lastPasteRef.current < 1000) { e.preventDefault(); return }
      lastPasteRef.current = now
      if (handleAttachTransfer(e.clipboardData)) { e.preventDefault(); return }
    }
    const text = e.clipboardData.getData('text/plain')
    if (!text) return

    const lineCount = text.split('\n').length
    if (lineCount >= PASTE_LINE_THRESHOLD && onPasteContent) {
      e.preventDefault()
      onPasteContent(text)
      return
    }

    if (/\n{2,}/.test(text)) {
      e.preventDefault()
      const cleaned = text.replace(/\n{2,}/g, '\n')
      const el = e.currentTarget
      const start = el.selectionStart
      const end = el.selectionEnd
      const before = value.slice(0, start)
      const after = value.slice(end)
      onChange(before + cleaned + after)
      requestAnimationFrame(() => {
        const pos = start + cleaned.length
        el.selectionStart = el.selectionEnd = pos
      })
    }
  }



  return (
    <div className="w-full max-w-[840px]" style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
      {isFileDragging && (
        <div
          className="flex items-center justify-center rounded-xl px-4 py-2 text-sm"
          style={{
            border: '0.5px dashed var(--c-border-subtle)',
            background: 'var(--c-bg-sub)',
            color: 'var(--c-text-secondary)',
          }}
        >
          {t.dragToAttach}
        </div>
      )}

      {/* 录音 / 转写进行中时显示的波形条 */}
      {(isRecording || isTranscribing) && (
        <div
          style={{
            border: 'var(--c-input-border)',
            borderRadius: '20px',
            padding: '10px 20px',
            background: 'var(--c-bg-input)',
            boxShadow: 'var(--c-input-shadow)',
            display: 'flex',
            alignItems: 'center',
            gap: '10px',
          }}
        >
          {/* 波形可视化 */}
          <div
            style={{
              flex: 1,
              display: 'flex',
              alignItems: 'center',
              gap: '3px',
              height: '40px',
              overflow: 'hidden',
              // 左侧渐隐效果
              WebkitMaskImage: 'linear-gradient(to right, rgba(0,0,0,0.15) 0%, rgba(0,0,0,1) 60%)',
              maskImage: 'linear-gradient(to right, rgba(0,0,0,0.15) 0%, rgba(0,0,0,1) 60%)',
            }}
          >
            {waveformBars.map((h, i) => (
              <div
                key={i}
                style={{
                  width: '2px',
                  height: `${Math.max(3, Math.round(h * 38))}px`,
                  borderRadius: '999px',
                  background: 'var(--c-text-secondary)',
                  flexShrink: 0,
                  transition: 'height 0.06s ease',
                }}
              />
            ))}
          </div>

          {/* 计时器 */}
          <span
            style={{
              fontVariantNumeric: 'tabular-nums',
              fontSize: '14px',
              color: 'var(--c-text-secondary)',
              flexShrink: 0,
              minWidth: '36px',
              textAlign: 'right',
            }}
          >
            {formatRecordingTime(recordingSeconds)}
          </span>

          {/* 取消 */}
          <button
            type="button"
            onClick={cancelRecording}
            disabled={isTranscribing}
            className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg bg-[var(--c-bg-deep)] text-[var(--c-text-secondary)] transition-[opacity,background] duration-[60ms] hover:bg-[var(--c-bg-deep)] hover:opacity-100 opacity-70 disabled:cursor-not-allowed disabled:opacity-40"
          >
            <X size={14} />
          </button>

          {/* 确认 */}
          <button
            type="button"
            onClick={stopAndTranscribe}
            disabled={isTranscribing}
            className="flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg bg-[var(--c-accent-send)] text-[var(--c-accent-send-text)] transition-[background-color,opacity] duration-[60ms] hover:bg-[var(--c-accent-send-hover)] active:opacity-[0.75] active:scale-[0.93] disabled:cursor-not-allowed disabled:opacity-60"
          >
            {isTranscribing
              ? <Loader2 size={14} className="animate-spin" />
              : <Check size={14} />}
          </button>
        </div>
      )}

      {/* 主输入框 */}
      <div
        className={[
          'bg-[var(--c-bg-input)] chat-input-box',
          focused && 'is-focused',
        ].filter(Boolean).join(' ')}
        style={{
          borderWidth: '0.5px',
          borderStyle: 'solid',
          borderColor: focused
            ? 'var(--c-input-border-color-focus)'
            : 'var(--c-input-border-color)',
          borderRadius: '20px',
          boxShadow: focused
            ? 'var(--c-input-shadow-focus)'
            : 'var(--c-input-shadow)',
          transition: 'border-color 0.2s ease, box-shadow 0.2s ease',
          cursor: 'default',
        }}
        onClick={(e) => {
          const tag = (e.target as HTMLElement).tagName
          if (tag !== 'BUTTON' && tag !== 'TEXTAREA' && tag !== 'INPUT' && tag !== 'SVG' && tag !== 'PATH') {
            textareaRef.current?.focus()
          }
        }}
      >
      {/* 附件卡片 */}
      <div
        style={{
          display: 'grid',
          gridTemplateRows: (attachments.length > 0 && !collapsingGrid) ? '1fr' : '0fr',
          transition: 'grid-template-rows 0.3s ease',
          overflow: 'hidden',
        }}
      >
        <div style={{ minHeight: 0, padding: '14px 16px 0' }}>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '12px', paddingBottom: '8px' }}>
            {attachments.map((att) => {
              const removeHandler = () => {
                if (attachments.length === 1) {
                  setCollapsingGrid(true)
                  setTimeout(() => {
                    onRemoveAttachment?.(att.id)
                    setCollapsingGrid(false)
                  }, 350)
                } else {
                  onRemoveAttachment?.(att.id)
                }
              }
              if (att.pasted) {
                return (
                  <PastedContentCard
                    key={att.id}
                    attachment={att}
                    onRemove={removeHandler}
                    onClick={() => setPastedModalAttachment(att)}
                  />
                )
              }
              return (
                <AttachmentCard
                  key={att.id}
                  attachment={att}
                  onRemove={removeHandler}
                />
              )
            })}
          </div>
        </div>
      </div>
      <form onSubmit={(e) => onSubmit(e, selectedPersonaKey, selectedModel ?? undefined)} style={{ padding: '8px 22px 20px' }}>
        <textarea
          ref={textareaRef}
          rows={1}
          className="w-full resize-none bg-transparent outline-none placeholder:text-[var(--c-placeholder)] placeholder:font-[360] disabled:cursor-not-allowed"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={handleKeyDown}
          onPaste={handleTextareaPaste}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          placeholder={placeholder}
          disabled={disabled}
          style={{
            fontFamily: 'inherit',
            fontSize: '16px',
            fontWeight: 310,
            color: 'var(--c-text-primary)',
            marginTop: '0px',
            marginBottom: '20px',
            letterSpacing: '-0.16px',
            overflow: 'auto',
          }}
        />

        <div className="flex items-center" style={{ gap: '2px' }}>
          {/* + 按钮及菜单 */}
          <div className="relative -ml-1.5">
            <button
              ref={plusBtnRef}
              type="button"
              onClick={() => setMenuOpen((v) => !v)}
              className="relative top-px flex h-8 w-8 items-center justify-center rounded-lg text-[var(--c-text-secondary)] transition-[background] duration-[60ms] hover:bg-[var(--c-bg-deep)]"
            >
              <Plus size={20} strokeWidth={1.5} />
            </button>

            {menuOpen && (
              <div
                ref={menuRef}
                className={`absolute left-0 z-50 ${variant === 'welcome' ? 'dropdown-menu' : 'dropdown-menu-up'}`}
                style={{
                  ...(variant === 'welcome'
                    ? { top: 'calc(100% + 8px)' }
                    : { bottom: 'calc(100% + 8px)' }),
                  border: '0.5px solid var(--c-border-subtle)',
                  borderRadius: '10px',
                  padding: '4px',
                  background: 'var(--c-bg-menu)',
                  minWidth: '200px',
                  boxShadow: 'var(--c-dropdown-shadow)',
                }}
              >
                <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
                  <button
                    type="button"
                    onClick={() => { fileInputRef.current?.click(); setMenuOpen(false) }}
                    className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] hover:text-[var(--c-text-primary)]"
                  >
                    <Paperclip size={14} style={{ color: 'var(--c-text-secondary)', flexShrink: 0 }} />
                    {t.addFromLocal}
                  </button>
                  <div style={{ height: '1px', background: 'var(--c-border-subtle)', margin: '2px 4px' }} />
                  {personas.map((persona) => {
                    const isActive = selectedPersonaKey === persona.persona_key
                    const icon = persona.persona_key === LEARNING_PERSONA_KEY
                      ? <BookOpen size={14} style={{ flexShrink: 0 }} />
                      : persona.persona_key === SEARCH_PERSONA_KEY
                        ? <Search size={14} style={{ flexShrink: 0 }} />
                        : null
                    return (
                      <button
                        key={persona.persona_key}
                        type="button"
                        onClick={() => handleModeSelect(persona.persona_key)}
                        className="flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm hover:bg-[var(--c-bg-deep)]"
                        style={{
                          color: isActive ? 'var(--c-text-primary)' : 'var(--c-text-secondary)',
                          fontWeight: isActive ? 500 : 400,
                        }}
                      >
                        <span style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                          {icon}
                          {persona.selector_name}
                        </span>
                        {(isActive || (chipExiting && selectedPersonaKey === persona.persona_key)) && (
                          <Check size={13} style={{ color: '#4691F6', flexShrink: 0 }} />
                        )}
                      </button>
                    )
                  })}
                </div>
              </div>
            )}
          </div>

          {(isNonDefaultMode || chipExiting) && (
            <button
              type="button"
              onClick={deactivateMode}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: '2px',
                height: '32px',
                padding: '0 8px 0 9px',
                borderRadius: '8px',
                background: 'var(--c-bg-deep)',
                border: '0.5px solid var(--c-border-subtle)',
                flexShrink: 0,
                marginLeft: '4px',
                position: 'relative',
                top: '1px',
                cursor: 'pointer',
                animation: chipExiting
                  ? 'chip-exit 0.12s cubic-bezier(0.4, 0, 1, 1) both'
                  : 'chip-enter 0.14s cubic-bezier(0.16, 1, 0.3, 1) both',
              }}
            >
              {selectedPersonaKey === LEARNING_PERSONA_KEY && (
                <BookOpen size={12} style={{ color: 'var(--c-text-secondary)', flexShrink: 0 }} />
              )}
              {selectedPersonaKey === SEARCH_PERSONA_KEY && (
                <Search size={12} style={{ color: '#4691F6', flexShrink: 0 }} />
              )}
              <span style={{
                fontSize: '13px',
                color: selectedPersonaKey === SEARCH_PERSONA_KEY ? '#4691F6' : 'var(--c-text-secondary)',
                fontWeight: 450,
                whiteSpace: 'nowrap',
                margin: '0 4px',
              }}>
                {selectedPersona?.selector_name ?? selectedPersonaKey}
              </span>
              <X size={9} style={{ color: 'var(--c-text-muted)', flexShrink: 0 }} />
            </button>
          )}

          <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: '2px', position: 'relative' }}>
            <ModelPicker
              accessToken={accessToken}
              value={selectedModel}
              onChange={setSelectedModel}
              onAddApiKey={() => onOpenSettings?.('models')}
              variant={variant}
            /></div>

            <button
              type="button"
              onClick={startRecording}
              disabled={isRecording || isTranscribing || !accessToken}
              className="flex h-8 w-8 items-center justify-center rounded-lg text-[var(--c-text-secondary)] opacity-70 transition-[opacity,background] duration-[60ms] hover:bg-[var(--c-bg-deep)] hover:opacity-100 disabled:cursor-not-allowed disabled:opacity-30"
            >
              <Mic size={16} />
            </button>
            {isStreaming && canCancel ? (
              <button
                type="button"
                onClick={onCancel}
                disabled={cancelSubmitting}
                className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--c-accent-send)] text-[var(--c-accent-send-text)] transition-[background-color,opacity] duration-[60ms] hover:bg-[var(--c-accent-send-hover)] active:opacity-[0.75] disabled:cursor-not-allowed disabled:opacity-50"
              >
                <Square size={14} fill="currentColor" />
              </button>
            ) : (
              <button
                type="submit"
                disabled={disabled || isStreaming || (!value.trim() && attachments.length === 0)}
                className="flex h-8 w-8 items-center justify-center rounded-lg bg-[var(--c-accent-send)] text-[var(--c-accent-send-text)] transition-[background-color,opacity] duration-[60ms] hover:bg-[var(--c-accent-send-hover)] active:opacity-[0.75] active:scale-[0.93] disabled:cursor-not-allowed disabled:opacity-50"
              >
                <ArrowUp size={16} />
              </button>
            )}
          </div>
      </form>

      {/* 隐藏的 file input */}
      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={handleFileChange}
      />
      </div>

      {pastedModalAttachment?.pasted && (
        <PastedContentModal
          text={pastedModalAttachment.pasted.text}
          size={pastedModalAttachment.size}
          lineCount={pastedModalAttachment.pasted.lineCount}
          onClose={() => setPastedModalAttachment(null)}
        />
      )}
    </div>
  )
}
