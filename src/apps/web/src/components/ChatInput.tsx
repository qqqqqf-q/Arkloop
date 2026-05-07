import { useRef, useEffect, useCallback, useMemo, useState, forwardRef, useImperativeHandle, useLayoutEffect } from 'react'
import { ArrowUp, Mic, X, Check, Loader2, Pencil } from 'lucide-react'
import type { FormEvent, KeyboardEvent, ClipboardEvent as ReactClipboardEvent } from 'react'
import { listSelectablePersonas, type SelectablePersona, type UploadedThreadAttachment } from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { PastedContentModal } from './PastedContentModal'
import type { SettingsTab } from './SettingsModal'
import {
  DEFAULT_PERSONA_KEY,
  SEARCH_PERSONA_KEY,
  WORK_PERSONA_KEY,
  type InputDraftScope,
  readSelectedPersonaKeyFromStorage,
  writeSelectedPersonaKeyToStorage,
  readSelectedModelFromStorage,
  writeSelectedModelToStorage,
  readSelectedReasoningMode,
  writeSelectedReasoningMode,
  readThreadReasoningMode,
  writeThreadReasoningMode,
  readInputDraftText,
  writeInputDraftText,
  readInputHistory,
  appendInputHistory,
} from '../storage'
import type { AppMode } from '../storage'
import {
  AttachmentCard,
  PastedContentCard,
  hasTransferFiles,
} from './chat-input'
import { useAudioRecorder } from './chat-input/useAudioRecorder'
import { useAttachments } from './chat-input/useAttachments'
import { PersonaModelBar } from './chat-input/PersonaModelBar'
import { ModelPicker } from './ModelPicker'
import { AutoResizeTextarea, measureTextareaHeight } from '@arkloop/shared'
import { useLatest } from '../hooks/useLatest'
import { useInputPerfDebug } from '../hooks/useInputPerfDebug'

export type ChatInputHandle = {
  clear: () => void
  setValue: (text: string) => void
  getValue: () => string
}

export type Attachment = {
  id: string
  file?: File
  name: string
  size: number
  mime_type: string
  preview_url?: string
  status: 'uploading' | 'ready' | 'error'
  uploaded?: UploadedThreadAttachment
  pasted?: { text: string; lineCount: number }
}

type Props = {
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
  onOpenSettings?: (tab: SettingsTab | 'voice') => void
  appMode?: AppMode
  hasMessages?: boolean
  messagesLoading?: boolean
  workThreadId?: string
  queuedEditLabel?: string
  onCancelQueuedEdit?: () => void
  draftOwnerKey?: string | null
  planMode?: boolean
  onTogglePlanMode?: (currentMode: boolean) => Promise<void>
  learningModeEnabled?: boolean
  learningModeUpdating?: boolean
  onToggleLearningMode?: (currentMode: boolean) => Promise<void>
}

type TextareaSelection = {
  start: number
  end: number
  direction: 'forward' | 'backward' | 'none'
}

function buildFallbackSelectablePersonas(_selectedPersonaKey: string): SelectablePersona[] {
  return []
}

function pickPreferredPersonaKey(personas: SelectablePersona[], preferred?: string): string {
  if (preferred && personas.some((persona) => persona.persona_key === preferred)) return preferred
  if (personas.some((persona) => persona.persona_key === DEFAULT_PERSONA_KEY)) return DEFAULT_PERSONA_KEY
  return DEFAULT_PERSONA_KEY
}

export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function countLinesWithinLimit(text: string, limit: number) {
  let lines = 1
  for (let index = 0; index < text.length; index += 1) {
    if (text.charCodeAt(index) !== 10) continue
    lines += 1
    if (lines >= limit) return lines
  }
  return lines
}

function readStyleNumber(style: CSSStyleDeclaration, key: string) {
  const value = Number.parseFloat(style.getPropertyValue(key))
  return Number.isFinite(value) ? value : 0
}

function readTextareaLineHeight(style: CSSStyleDeclaration) {
  const explicit = Number.parseFloat(style.lineHeight)
  if (Number.isFinite(explicit)) return explicit
  const fontSize = Number.parseFloat(style.fontSize)
  return Number.isFinite(fontSize) ? fontSize * 1.5 : 20
}

function readTextareaFont(style: CSSStyleDeclaration) {
  if (style.font) return style.font
  return [
    style.fontStyle,
    style.fontVariant,
    style.fontWeight,
    style.fontSize,
    style.fontFamily,
  ].filter(Boolean).join(' ')
}

function measureTextareaContentWidth(element: HTMLTextAreaElement, style: CSSStyleDeclaration) {
  const horizontalPadding = readStyleNumber(style, 'padding-left') + readStyleNumber(style, 'padding-right')
  const horizontalBorder = readStyleNumber(style, 'border-left-width') + readStyleNumber(style, 'border-right-width')
  return Math.max(
    element.clientWidth - horizontalPadding,
    element.offsetWidth - horizontalPadding - horizontalBorder,
    1,
  )
}

function isSameDraftDomain(left: InputDraftScope | null, right: InputDraftScope): boolean {
  if (!left) return false
  return left.page === right.page
    && (left.threadId ?? null) === (right.threadId ?? null)
    && left.appMode === right.appMode
    && !!left.searchMode === !!right.searchMode
}

export const ChatInput = forwardRef<ChatInputHandle, Props>(function ChatInput({
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
  appMode,
  hasMessages,
  messagesLoading,
  workThreadId,
  queuedEditLabel,
  onCancelQueuedEdit,
  draftOwnerKey,
  learningModeEnabled = false,
  learningModeUpdating = false,
  onToggleLearningMode,
}, ref) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const [draft, setDraft] = useState('')
  const draftRef = useLatest(draft)

  const historyRef = useRef<string[]>([])
  const historyCursorRef = useRef(-1)
  const historyDraftRef = useRef('')

  const resetHistoryCursor = useCallback(() => {
    historyCursorRef.current = -1
    historyDraftRef.current = ''
  }, [])

  useImperativeHandle(ref, () => ({
    clear: () => {
      resetHistoryCursor()
      setDraft('')
    },
    setValue: (text: string) => {
      resetHistoryCursor()
      setDraft(text)
    },
    getValue: () => draftRef.current,
  }))

  const { wrapOnChange } = useInputPerfDebug()
  const trackedSetDraft = useMemo(() => wrapOnChange(setDraft), [wrapOnChange])

  const valueRef = useLatest(draft)
  const onChangeRef = useLatest(setDraft)
  const accessTokenRef = useLatest(accessToken)
  const onAsrErrorRef = useLatest(onAsrError)
  const onVoiceNotConfiguredRef = useLatest<(() => void) | undefined>(() => onOpenSettings?.('voice'))

  const { t } = useLocale()

  const [selectablePersonas, setSelectablePersonas] = useState<SelectablePersona[]>([])
  const [selectedPersonaKey, setSelectedPersonaKey] = useState(readSelectedPersonaKeyFromStorage)
  const [focused, setFocused] = useState(false)
  const [collapsingGrid, setCollapsingGrid] = useState(false)
  const [pastedModalAttachment, setPastedModalAttachment] = useState<Attachment | null>(null)
  const [chipExiting, setChipExiting] = useState(false)
  const [typewriterText, setTypewriterText] = useState('')
  const [workCompactInputWraps, setWorkCompactInputWraps] = useState(false)
  const [textareaFocusRestoreTick, setTextareaFocusRestoreTick] = useState(0)
  const [selectedModel, setSelectedModel] = useState<string | null>(readSelectedModelFromStorage)
  const compactTextareaWidthRef = useRef<number | null>(null)
  const pendingTextareaFocusRef = useRef<TextareaSelection | null>(null)
  const inputLayoutChangingRef = useRef(false)
  const isComposingRef = useRef(false)
  const [isComposingInput, setIsComposingInput] = useState(false)
  const composingWorkCompactInputRef = useRef<boolean | null>(null)
  const draftScope = useMemo<InputDraftScope>(() => ({
    ownerKey: draftOwnerKey,
    page: variant === 'welcome' ? 'welcome' : 'thread',
    threadId: variant === 'welcome' ? undefined : workThreadId,
    appMode: appMode === 'work' ? 'work' : 'chat',
    searchMode,
  }), [appMode, draftOwnerKey, searchMode, variant, workThreadId])
  const draftScopeKey = useMemo(() => JSON.stringify(draftScope), [draftScope])
  const skipDraftPersistRef = useRef(false)
  const prevDraftScopeRef = useRef<InputDraftScope | null>(null)

  const [reasoningMode, setReasoningMode] = useState(() => {
    if (!workThreadId) return readSelectedReasoningMode()
    return readThreadReasoningMode(workThreadId)
  })

  useEffect(() => {
    if (!workThreadId) {
      setReasoningMode(readSelectedReasoningMode())
      return
    }
    setReasoningMode(readThreadReasoningMode(workThreadId))
  }, [workThreadId])

  const { isRecording, isTranscribing, recordingSeconds, waveformBars, startRecording, stopAndTranscribe, cancelRecording } =
    useAudioRecorder({ accessTokenRef, valueRef, onChangeRef, onAsrErrorRef, onVoiceNotConfiguredRef })

  const { isFileDragging, handleAttachTransfer, pasteProcessingRef, lastPasteRef } =
    useAttachments({ onAttachFiles, textareaRef })

  const persistSelectedPersona = useCallback((personaKey: string) => {
    setSelectedPersonaKey(personaKey)
    writeSelectedPersonaKeyToStorage(personaKey)
    onPersonaChange?.(personaKey)
  }, [onPersonaChange])

  useEffect(() => {
    let cancelled = false

    if (!accessToken) {
      const clearId = requestAnimationFrame(() => setSelectablePersonas([]))
      return () => {
        cancelled = true
        cancelAnimationFrame(clearId)
      }
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

  const handleModelChange = useCallback((model: string | null) => {
    setSelectedModel(model)
    writeSelectedModelToStorage(model)
    setReasoningMode('off')
    if (workThreadId) {
      writeThreadReasoningMode(workThreadId, 'off')
      return
    }
    writeSelectedReasoningMode('off')
  }, [workThreadId])

  const handleReasoningModeChange = useCallback((mode: string) => {
    setReasoningMode(mode)
    if (workThreadId) {
      writeThreadReasoningMode(workThreadId, mode)
      return
    }
    writeSelectedReasoningMode(mode)
  }, [workThreadId])

  const handleMenuOpenChange = useCallback((open: boolean) => {
    const el = textareaRef.current
    if (!el) return
    if (open) {
      el.blur()
    } else {
      el.focus()
    }
  }, [])

  const isNonDefaultMode = selectedPersonaKey !== DEFAULT_PERSONA_KEY && selectedPersonaKey !== WORK_PERSONA_KEY
  const showSendButton = draft.trim().length > 0 || attachments.length > 0
  const resolvedPlaceholder = typewriterText
  const isWelcomeInput = variant === 'welcome'
  const isWorkChat = variant === 'chat' && appMode === 'work'
  const hasAttachments = attachments.length > 0
  const showAttachmentGrid = hasAttachments && !collapsingGrid
  const showWelcomeAttachmentSpacer = isWelcomeInput && (!hasAttachments || collapsingGrid)
  const isPlainChatThread = variant === 'chat' && appMode !== 'work'
  const isEditingQueuedPrompt = !!queuedEditLabel
  const isWorkSingleLogicalLine = countLinesWithinLimit(draft, 2) === 1
  const measuredWorkCompactInput = isWorkChat && isWorkSingleLogicalLine && !workCompactInputWraps
  const isWorkCompactInput = isWorkChat && isComposingInput && composingWorkCompactInputRef.current !== null
    ? composingWorkCompactInputRef.current
    : measuredWorkCompactInput
  const isWorkExpandedInput = isWorkChat && !isWorkCompactInput
  const formPadding = isPlainChatThread
    ? '19px 12px 11px 20px'
    : isWelcomeInput
      ? '10px 14px 14px 22px'
      : isWorkChat
        ? '8px 12px 8px 14px'
        : '6px 12px 11px 20px'
  const textareaWrapperMarginBottom = isPlainChatThread
    ? '1px'
    : isWelcomeInput
      ? '12px'
      : isWorkExpandedInput
        ? '6px'
        : '9px'

  const readTextareaSelection = useCallback((textarea: HTMLTextAreaElement): TextareaSelection => ({
    start: textarea.selectionStart,
    end: textarea.selectionEnd,
    direction: textarea.selectionDirection as TextareaSelection['direction'],
  }), [])

  const restoreTextareaFocus = useCallback((selection?: TextareaSelection | null) => {
    const textarea = textareaRef.current
    if (!textarea || disabled) return
    textarea.focus({ preventScroll: true })
    setFocused(true)
    if (!selection) return
    const start = Math.min(selection.start, textarea.value.length)
    const end = Math.min(selection.end, textarea.value.length)
    textarea.setSelectionRange(start, end, selection.direction)
  }, [disabled])

  const requestTextareaFocusRestore = useCallback((selection: TextareaSelection) => {
    pendingTextareaFocusRef.current = selection
    setTextareaFocusRestoreTick((tick) => tick + 1)
  }, [])

  const measureWorkInputWraps = useCallback((value: string, textarea: HTMLTextAreaElement) => {
    const style = window.getComputedStyle(textarea)
    const measuredWidth = measureTextareaContentWidth(textarea, style)
    if (isWorkCompactInput) compactTextareaWidthRef.current = measuredWidth
    const compactWidth = compactTextareaWidthRef.current ?? measuredWidth
    const lineHeight = readTextareaLineHeight(style)
    const visualHeight = measureTextareaHeight({
      value,
      width: compactWidth,
      font: readTextareaFont(style),
      lineHeight,
      minRows: 1,
    })
    return visualHeight > lineHeight + 0.5
  }, [isWorkCompactInput])

  const willWorkInputLayoutChange = useCallback((value: string, textarea: HTMLTextAreaElement) => {
    if (!isWorkChat) return false
    const nextSingleLogicalLine = countLinesWithinLimit(value, 2) === 1
    const nextWraps = nextSingleLogicalLine ? measureWorkInputWraps(value, textarea) : false
    const nextCompactInput = nextSingleLogicalLine && !nextWraps
    return nextCompactInput !== isWorkCompactInput
  }, [isWorkChat, isWorkCompactInput, measureWorkInputWraps])

  useLayoutEffect(() => {
    if (isComposingInput) return
    if (!isWorkChat) {
      compactTextareaWidthRef.current = null
      if (workCompactInputWraps) setWorkCompactInputWraps(false)
      return
    }
    if (!isWorkSingleLogicalLine) {
      if (workCompactInputWraps) setWorkCompactInputWraps(false)
      return
    }

    const textarea = textareaRef.current
    if (!textarea) return
    const style = window.getComputedStyle(textarea)
    const measuredWidth = measureTextareaContentWidth(textarea, style)
    if (isWorkCompactInput) compactTextareaWidthRef.current = measuredWidth

    const compactWidth = compactTextareaWidthRef.current ?? measuredWidth
    const lineHeight = readTextareaLineHeight(style)
    const visualHeight = measureTextareaHeight({
      value: draft,
      width: compactWidth,
      font: readTextareaFont(style),
      lineHeight,
      minRows: 1,
    })
    const nextWraps = visualHeight > lineHeight + 0.5
    if (nextWraps !== workCompactInputWraps) {
      inputLayoutChangingRef.current = true
      setWorkCompactInputWraps(nextWraps)
    }
  }, [draft, isComposingInput, isWorkChat, isWorkCompactInput, isWorkSingleLogicalLine, workCompactInputWraps])

  useLayoutEffect(() => {
    if (!pendingTextareaFocusRef.current) return
    if (inputLayoutChangingRef.current) {
      inputLayoutChangingRef.current = false
      setTextareaFocusRestoreTick((tick) => tick + 1)
      return
    }
    const selection = pendingTextareaFocusRef.current
    pendingTextareaFocusRef.current = null
    restoreTextareaFocus(selection)
  }, [draft, isWorkCompactInput, isWorkExpandedInput, restoreTextareaFocus, textareaFocusRestoreTick])

  useLayoutEffect(() => {
    if (disabled || isRecording || isTranscribing) return
    const frame = requestAnimationFrame(() => restoreTextareaFocus(null))
    return () => cancelAnimationFrame(frame)
  }, [disabled, draftScopeKey, isRecording, isTranscribing, restoreTextareaFocus])

  useEffect(() => {
    const prevScope = prevDraftScopeRef.current
    const nextStored = readInputDraftText(draftScope)
    let nextDraft = nextStored
    if (
      isSameDraftDomain(prevScope, draftScope)
      && prevScope?.ownerKey !== draftScope.ownerKey
      && !nextStored
      && draftRef.current.trim()
    ) {
      nextDraft = draftRef.current
      writeInputDraftText(draftScope, nextDraft)
    }
    prevDraftScopeRef.current = draftScope
    historyRef.current = readInputHistory(draftScope)
    resetHistoryCursor()
    skipDraftPersistRef.current = true
    setDraft(nextDraft)
  }, [draftScope, draftScopeKey, resetHistoryCursor])

  useEffect(() => {
    if (skipDraftPersistRef.current) {
      skipDraftPersistRef.current = false
      return
    }
    writeInputDraftText(draftScope, draft)
  }, [draft, draftScope, draftScopeKey])

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
  }, [selectedPersonaKey, chipExiting, persistSelectedPersona, deactivateMode])

  const formatRecordingTime = (secs: number) => {
    const m = Math.floor(secs / 60)
    const s = secs % 60
    return `${m}:${String(s).padStart(2, '0')}`
  }

  useEffect(() => {
    const id = requestAnimationFrame(() => {
      if (searchMode && selectedPersonaKey !== SEARCH_PERSONA_KEY) {
        persistSelectedPersona(SEARCH_PERSONA_KEY)
      } else if (!searchMode && selectedPersonaKey === SEARCH_PERSONA_KEY) {
        persistSelectedPersona(DEFAULT_PERSONA_KEY)
      }
    })
    return () => cancelAnimationFrame(id)
  }, [persistSelectedPersona, searchMode, selectedPersonaKey])

  // sync persona when appMode changes
  useEffect(() => {
    const id = requestAnimationFrame(() => {
      if (appMode === 'work' && selectedPersonaKey !== WORK_PERSONA_KEY) {
        persistSelectedPersona(WORK_PERSONA_KEY)
      } else if (appMode !== 'work' && selectedPersonaKey === WORK_PERSONA_KEY) {
        persistSelectedPersona(DEFAULT_PERSONA_KEY)
      }
    })
    return () => cancelAnimationFrame(id)
  }, [persistSelectedPersona, appMode, selectedPersonaKey])

  const typewriterTarget = placeholder

  // typewriter: clears text, then types out target one char every 45ms
  const typewriterTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(() => {
    const target = typewriterTarget
    if (!target) {
      setTypewriterText('')
      return
    }
    let i = 0
    setTypewriterText('')
    const tick = () => {
      i++
      if (i > target.length) return
      setTypewriterText(target.slice(0, i))
      typewriterTimerRef.current = setTimeout(tick, 45)
    }
    typewriterTimerRef.current = setTimeout(tick, 45)
    return () => {
      if (typewriterTimerRef.current !== null) {
        clearTimeout(typewriterTimerRef.current)
        typewriterTimerRef.current = null
      }
    }
  }, [typewriterTarget])

  const applyHistoryValue = (value: string, cursor: 'start' | 'end') => {
    skipDraftPersistRef.current = true
    setDraft(value)
    requestAnimationFrame(() => {
      const target = textareaRef.current
      if (!target) return
      const position = cursor === 'start' ? 0 : target.value.length
      target.setSelectionRange(position, position)
    })
  }

  const handleHistoryNavigation = (direction: 'up' | 'down', target: HTMLTextAreaElement): boolean => {
    if (direction === 'up' && target.selectionStart !== 0) return false
    if (direction === 'down' && target.selectionEnd !== target.value.length) return false

    const history = historyRef.current
    if (history.length === 0) return false

    if (direction === 'up') {
      if (historyCursorRef.current < 0) historyDraftRef.current = target.value
      const nextCursor = historyCursorRef.current < 0
        ? 0
        : Math.min(historyCursorRef.current + 1, history.length - 1)
      if (nextCursor === historyCursorRef.current) return false
      historyCursorRef.current = nextCursor
      applyHistoryValue(history[history.length - 1 - nextCursor] ?? '', 'start')
      return true
    }

    if (historyCursorRef.current < 0) return false
    if (historyCursorRef.current === 0) {
      historyCursorRef.current = -1
      applyHistoryValue(historyDraftRef.current, 'end')
      historyDraftRef.current = ''
      return true
    }
    historyCursorRef.current -= 1
    applyHistoryValue(history[history.length - 1 - historyCursorRef.current] ?? '', 'end')
    return true
  }

  const isComposingEvent = (event: KeyboardEvent<HTMLTextAreaElement>['nativeEvent']) => (
    isComposingRef.current || event.isComposing || event.keyCode === 229
  )

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    const isComposing = isComposingEvent(e.nativeEvent)
    if (!isComposing && e.key === 'ArrowUp' && handleHistoryNavigation('up', e.currentTarget)) {
      e.preventDefault()
      return
    }
    if (!isComposing && e.key === 'ArrowDown' && handleHistoryNavigation('down', e.currentTarget)) {
      e.preventDefault()
      return
    }
    if (e.key === 'Enter' && !e.shiftKey && !isComposing) {
      e.preventDefault()
      if (!disabled && (draft.trim() || attachments.length > 0)) {
        e.currentTarget.form?.requestSubmit()
      }
    }
  }

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? [])
    if (files.length > 0) onAttachFiles?.(files)
    e.target.value = ''
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

    const lineCount = countLinesWithinLimit(text, PASTE_LINE_THRESHOLD)
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
      const before = draft.slice(0, start)
      const after = draft.slice(end)
      const nextDraft = before + cleaned + after
      const pos = start + cleaned.length
      pendingTextareaFocusRef.current = { start: pos, end: pos, direction: 'none' }
      trackedSetDraft(nextDraft)
      requestAnimationFrame(() => {
        const target = textareaRef.current
        if (!target) return
        target.selectionStart = target.selectionEnd = pos
      })
    }
  }

  const handleDraftChange = (target: HTMLTextAreaElement) => {
    resetHistoryCursor()
    if (!isComposingRef.current && document.activeElement === target && willWorkInputLayoutChange(target.value, target)) {
      pendingTextareaFocusRef.current = readTextareaSelection(target)
    }
    trackedSetDraft(target.value)
  }

  const handleCompositionStart = () => {
    isComposingRef.current = true
    composingWorkCompactInputRef.current = isWorkChat ? isWorkCompactInput : null
    setIsComposingInput(true)
    pendingTextareaFocusRef.current = null
  }

  const handleCompositionEnd = (target: HTMLTextAreaElement) => {
    isComposingRef.current = false
    composingWorkCompactInputRef.current = null
    setIsComposingInput(false)
    resetHistoryCursor()
    requestTextareaFocusRestore(readTextareaSelection(target))
    trackedSetDraft(target.value)
  }

  const handleFormSubmit = (e: FormEvent<HTMLFormElement>) => {
    const text = (textareaRef.current?.value ?? draft).trim()
    if (text) {
      appendInputHistory(draftScope, text)
      historyRef.current = readInputHistory(draftScope)
      resetHistoryCursor()
    }
    onSubmit(e, selectedPersonaKey, selectedModel ?? undefined)
  }

  return (
    <div
      className="w-full"
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: '8px',
        maxWidth: variant === 'welcome' ? '840px' : isWorkChat ? undefined : '720px',
      }}
    >
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
          <div
            style={{
              flex: 1,
              display: 'flex',
              alignItems: 'center',
              gap: '3px',
              height: '40px',
              overflow: 'hidden',
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

          <button
            type="button"
            onClick={cancelRecording}
            disabled={isTranscribing}
            className="flex h-[33.5px] w-[33.5px] flex-shrink-0 items-center justify-center rounded-lg bg-[var(--c-bg-deep)] text-[var(--c-text-secondary)] transition-[opacity,background] duration-[60ms] hover:bg-[var(--c-bg-deep)] hover:opacity-100 opacity-70 disabled:cursor-not-allowed disabled:opacity-40"
          >
            <X size={14} />
          </button>

          <button
            type="button"
            onClick={stopAndTranscribe}
            disabled={isTranscribing}
            className="flex h-[33.5px] w-[33.5px] flex-shrink-0 items-center justify-center rounded-lg bg-[var(--c-accent-send)] text-[var(--c-accent-send-text)] transition-[background-color,opacity] duration-[60ms] hover:bg-[var(--c-accent-send-hover)] active:opacity-[0.75] active:scale-[0.93] disabled:cursor-not-allowed disabled:opacity-60"
          >
            {isTranscribing
              ? <Loader2 size={14} className="animate-spin" />
              : <Check size={14} />}
          </button>
        </div>
      )}

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
          borderRadius: isWorkChat ? (isWorkCompactInput ? '12px' : '16px') : '20px',
          boxShadow: focused
            ? 'var(--c-input-shadow-focus)'
            : 'var(--c-input-shadow)',
          transition: isWorkChat
            ? 'border-color 0.2s ease, box-shadow 0.2s ease, border-radius 180ms cubic-bezier(0.16, 1, 0.3, 1)'
            : 'border-color 0.2s ease, box-shadow 0.2s ease',
          cursor: 'default',
        }}
        onClick={(e) => {
          const tag = (e.target as HTMLElement).tagName
          if (tag !== 'BUTTON' && tag !== 'TEXTAREA' && tag !== 'INPUT' && tag !== 'SVG' && tag !== 'PATH') {
            textareaRef.current?.focus()
          }
        }}
      >
      <div
        style={{
          display: 'grid',
          gridTemplateRows: showAttachmentGrid ? '1fr' : '0fr',
          transition: 'grid-template-rows 0.3s ease',
          overflow: 'hidden',
        }}
      >
        <div style={{ minHeight: 0, overflow: 'hidden' }}>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '12px', padding: '14px 16px 8px' }}>
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
                  accessToken={accessToken}
                />
              )
            })}
          </div>
        </div>
      </div>
      {isWelcomeInput && (
        <div
          style={{
            display: 'grid',
            gridTemplateRows: showWelcomeAttachmentSpacer ? '1fr' : '0fr',
            transition: 'grid-template-rows 0.3s ease',
            overflow: 'hidden',
          }}
        >
          <div style={{ minHeight: 0, overflow: 'hidden' }}>
            <div style={{ height: '14px' }} />
          </div>
        </div>
      )}
      <form
        onSubmit={handleFormSubmit}
        style={{
          padding: formPadding,
        }}
      >
        {!isWorkCompactInput && (
          <div
            style={{
              position: 'relative',
              marginBottom: textareaWrapperMarginBottom,
              ...(isWorkExpandedInput
                ? { marginLeft: '3.5px', padding: '10px 0 0' }
                : {}),
            }}
          >
            <AutoResizeTextarea
              ref={textareaRef}
              rows={1}
              className="w-full resize-none bg-transparent outline-none placeholder:text-[var(--c-placeholder)] placeholder:font-[360] disabled:cursor-not-allowed"
              value={draft}
              onChange={(e) => handleDraftChange(e.currentTarget)}
              onKeyDown={handleKeyDown}
              onCompositionStart={handleCompositionStart}
              onCompositionEnd={(e) => handleCompositionEnd(e.currentTarget)}
              onPaste={handleTextareaPaste}
              onFocus={() => setFocused(true)}
              onBlur={() => setFocused(false)}
              placeholder={resolvedPlaceholder}
              disabled={disabled}
              minRows={1}
              maxHeight={300}
              style={{
                fontFamily: 'inherit',
                fontSize: '16px',
                fontWeight: 310,
                ...(variant === 'chat' ? { lineHeight: 1.45 as const } : {}),
                color: 'var(--c-text-primary)',
                marginTop: '0px',
                marginBottom: '0px',
                ...(isWorkExpandedInput ? { display: 'block', padding: 0, border: 'none' } : {}),
                letterSpacing: '-0.16px',
              }}
            />
          </div>
        )}

        <div
          className="flex items-center"
          style={{
            gap: '2px',
            minHeight: isWorkChat ? '34.5px' : '32px',
            width: '100%',
            minWidth: 0,
          }}
        >
          <PersonaModelBar
            personas={personas}
            selectedPersonaKey={selectedPersonaKey}
            selectedModel={selectedModel}
            isNonDefaultMode={isNonDefaultMode}
            selectedPersona={selectedPersona}
            onModeSelect={handleModeSelect}
            onDeactivateMode={deactivateMode}
            onModelChange={handleModelChange}
            thinkingEnabled={reasoningMode}
            onThinkingChange={handleReasoningModeChange}
            onOpenSettings={onOpenSettings}
            onFileInputClick={() => fileInputRef.current?.click()}
            accessToken={accessToken}
            variant={variant}
            appMode={appMode}
            threadHasMessages={hasMessages}
            threadMessagesLoading={messagesLoading}
            workThreadId={workThreadId}
            hideWorkFolderPicker={isWorkCompactInput}
            hideModelPicker={isWorkCompactInput}
            onMenuOpenChange={handleMenuOpenChange}
            learningModeEnabled={learningModeEnabled}
            learningModeUpdating={learningModeUpdating}
            onToggleLearningMode={onToggleLearningMode}
          />

          {isEditingQueuedPrompt && (
            <div
              className="flex shrink-0 items-center gap-1"
              style={{
                height: '33.5px',
                padding: '0 4px 0 9px',
                borderRadius: '8px',
                background: 'color-mix(in srgb, var(--c-bg-sub) 82%, transparent)',
                border: '0.5px solid var(--c-border-subtle)',
              }}
            >
              <Pencil size={14} style={{ color: 'var(--c-text-secondary)', flexShrink: 0 }} />
              <span style={{
                fontSize: '14px',
                color: 'var(--c-text-secondary)',
                fontWeight: 375,
                whiteSpace: 'nowrap',
                margin: '0 2px',
              }}>
                {queuedEditLabel}
              </span>
              <button
                type="button"
                onClick={onCancelQueuedEdit}
                className="bg-transparent hover:bg-[rgba(0,0,0,0.05)]"
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  width: '20px',
                  height: '20px',
                  borderRadius: '5px',
                  border: 'none',
                  cursor: 'pointer',
                  padding: 0,
                  flexShrink: 0,
                }}
              >
                <X size={14} strokeWidth={2} style={{ color: 'var(--c-text-secondary)', opacity: 0.7 }} />
              </button>
            </div>
          )}

          {isWorkCompactInput && (
            <>
              <div
                style={{
                  flex: '1 1 auto',
                  minWidth: 0,
                  display: 'flex',
                  alignItems: 'center',
                  padding: '0 8px 0 4px',
                }}
              >
                <AutoResizeTextarea
                  ref={textareaRef}
                  rows={1}
                  className="w-full resize-none bg-transparent outline-none placeholder:text-[var(--c-placeholder)] placeholder:font-[360] disabled:cursor-not-allowed"
                  value={draft}
                  onChange={(e) => handleDraftChange(e.currentTarget)}
                  onKeyDown={handleKeyDown}
                  onCompositionStart={handleCompositionStart}
                  onCompositionEnd={(e) => handleCompositionEnd(e.currentTarget)}
                  onPaste={handleTextareaPaste}
                  onFocus={() => setFocused(true)}
                  onBlur={() => setFocused(false)}
                  placeholder={resolvedPlaceholder}
                  disabled={disabled}
                  minRows={1}
                  maxHeight={300}
                  style={{
                    display: 'block',
                    fontFamily: 'inherit',
                    fontSize: '16px',
                    fontWeight: 310,
                    lineHeight: 1.45 as const,
                    color: 'var(--c-text-primary)',
                    marginTop: '0px',
                    marginBottom: '0px',
                    padding: 0,
                    border: 'none',
                    letterSpacing: '-0.16px',
                  }}
                />
              </div>
              <div style={{ flexShrink: 0, marginRight: '4px', display: 'flex', alignItems: 'center', position: 'relative' }}>
                <ModelPicker
                  accessToken={accessToken}
                  value={selectedModel}
                  onChange={handleModelChange}
                  onAddModel={() => onOpenSettings?.('models')}
                  variant={variant}
                  thinkingEnabled={reasoningMode}
                  onThinkingChange={handleReasoningModeChange}
                />
              </div>
            </>
          )}

          {/* mic + send 共用同一位置，disabled 时显示 spinner */}
          <div
            style={{
              position: 'relative',
              width: '31.5px',
              height: '31.5px',
              flexShrink: 0,
            }}
          >
            {disabled ? (
              <div className="flex h-full w-full items-center justify-center rounded-lg bg-[var(--c-accent-send)]" style={{ opacity: 0.5 }}>
                <Loader2 size={14} className="animate-spin" style={{ color: 'var(--c-accent-send-text)' }} />
              </div>
            ) : isStreaming && canCancel && !isEditingQueuedPrompt ? (
              showSendButton ? (
                <button
                  type="submit"
                  disabled={!draft.trim() && attachments.length === 0}
                  className="flex h-full w-full items-center justify-center rounded-lg bg-[var(--c-accent-send)] text-[var(--c-accent-send-text)] hover:bg-[var(--c-accent-send-hover)] active:opacity-[0.75] active:scale-[0.93] disabled:cursor-not-allowed"
                  style={{
                    position: 'absolute',
                    inset: 0,
                  }}
                >
                  <ArrowUp size={17} />
                </button>
              ) : (
                <button
                  type="button"
                  onClick={onCancel}
                  disabled={cancelSubmitting}
                  className="flex h-full w-full items-center justify-center rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-input)] transition-[opacity,transform,background-color] duration-[140ms] hover:bg-[var(--c-bg-sub)] active:scale-[0.97] active:opacity-[0.82] disabled:cursor-not-allowed disabled:opacity-50"
                  style={{
                    position: 'absolute',
                    inset: 0,
                  }}
                >
                  <span
                    aria-hidden="true"
                    style={{
                      width: '14px',
                      height: '14px',
                      borderRadius: '999px',
                      border: '1.3px solid #1A1A19',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      flexShrink: 0,
                    }}
                  >
                    <span
                      style={{
                        width: '5px',
                        height: '5px',
                        borderRadius: '1px',
                        background: '#1A1A19',
                        flexShrink: 0,
                      }}
                    />
                  </span>
                </button>
              )
            ) : (
              <>
                <button
                  type="button"
                  onClick={startRecording}
                  disabled={isRecording || isTranscribing || !accessToken}
                  className="flex h-full w-full items-center justify-center rounded-lg text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] disabled:cursor-not-allowed disabled:opacity-30"
                  style={{
                    position: 'absolute',
                    inset: 0,
                    opacity: showSendButton ? 0 : 0.65,
                    transform: showSendButton ? 'scale(0.7)' : 'scale(1)',
                    transition: 'opacity 188ms ease, transform 188ms ease',
                    pointerEvents: showSendButton ? 'none' : 'auto',
                  }}
                >
                  <Mic size={19} />
                </button>
                <button
                  type="submit"
                  disabled={(!isEditingQueuedPrompt && isStreaming) || (!draft.trim() && attachments.length === 0)}
                  className="flex h-full w-full items-center justify-center rounded-lg bg-[var(--c-accent-send)] text-[var(--c-accent-send-text)] hover:bg-[var(--c-accent-send-hover)] active:opacity-[0.75] active:scale-[0.93] disabled:cursor-not-allowed"
                  style={{
                    position: 'absolute',
                    inset: 0,
                    transform: showSendButton ? 'scale(1)' : 'scale(0)',
                    opacity: showSendButton ? 1 : 0,
                    transition: 'transform 281ms cubic-bezier(0.34, 1.56, 0.64, 1), opacity 150ms ease, background-color 60ms ease',
                    pointerEvents: showSendButton ? 'auto' : 'none',
                  }}
                >
                  <ArrowUp size={17} />
                </button>
              </>
            )}
          </div>
        </div>
      </form>

      <input
        ref={fileInputRef}
        type="file"
        multiple
        className="hidden"
        onChange={handleFileChange}
      />
      {disabled && (
        <div
          style={{
            position: 'absolute',
            inset: 0,
            borderRadius: isWorkChat ? (isWorkCompactInput ? '12px' : '16px') : '20px',
            background: 'rgba(0,0,0,0.06)',
            overflow: 'hidden',
            pointerEvents: 'none',
            animation: 'freeze-overlay-in 1.8s ease forwards',
          }}
        >
          <div
            style={{
              position: 'absolute',
              top: 0,
              bottom: 0,
              width: '35%',
              background: 'linear-gradient(90deg, transparent, rgba(0,0,0,0.05), transparent)',
              animation: 'input-sweep 1.4s linear infinite',
            }}
          />
        </div>
      )}
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
})
