import { useRef, useEffect, useCallback, useMemo, useState, forwardRef, useImperativeHandle, useLayoutEffect } from 'react'
import { ArrowUp, Mic, X, Check, Loader2, Pencil } from 'lucide-react'
import type { FormEvent, KeyboardEvent, ClipboardEvent as ReactClipboardEvent, ReactNode } from 'react'
import { type UploadedThreadAttachment } from '../api'
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
  SELECTED_MODEL_CHANGED_EVENT,
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
  SlashCommandPopup,
  hasTransferFiles,
} from './chat-input'
import type { SlashCommandGroup, SlashCommandItem } from './chat-input'
import { useAudioRecorder } from './chat-input/useAudioRecorder'
import { useAttachments } from './chat-input/useAttachments'
import { PersonaModelBar } from './chat-input/PersonaModelBar'
import { ModelPicker } from './ModelPicker'
import { AutoResizeTextarea, measureTextareaHeight } from '@arkloop/shared'
import { useLatest } from '../hooks/useLatest'
import { useInputPerfDebug } from '../hooks/useInputPerfDebug'
import { ActionIconButton } from './ActionIconButton'
import { SHORTCUTS, matchesShortcut } from '../shortcuts'

export type ChatInputHandle = {
  clear: () => void
  setValue: (text: string) => void
  getValue: () => string
  focus: () => void
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
  onToggleLearningMode?: (currentMode: boolean) => Promise<void>
}

type TextareaSelection = {
  start: number
  end: number
  direction: 'forward' | 'backward' | 'none'
}

type SlashCaretRect = {
  x: number
  top: number
}

type SlashCommandRange = {
  start: number
  end: number
  query: string
}

const SLASH_POPUP_WIDTH = 300
const SLASH_POPUP_VIEWPORT_MARGIN = 8
const SETUP_COMMAND_HEAD_COLOR = 'rgb(159, 186, 231)'
const SETUP_COMMAND_TEXT_COLOR = 'rgb(64, 117, 208)'
const SETUP_COMMAND_HOVER_BG = 'rgb(231, 239, 251)'
const INLINE_SLASH_COMMAND_PATTERN = /\/[A-Za-z][\w-]*(?=\s|$)/g
const SETUP_COMMAND_PATTERN = /\/setup(?=\s|$)/g

function getSlashCommandRange(value: string, cursor: number): SlashCommandRange | null {
  if (cursor < 1) return null
  const start = value.lastIndexOf('/', cursor - 1)
  if (start < 0) return null
  const query = value.slice(start + 1, cursor)
  if (/[\s\n]/.test(query)) return null
  return { start, end: cursor, query }
}

function getInlineTokenDeletionRange(value: string, cursor: number): { start: number; end: number } | null {
  INLINE_SLASH_COMMAND_PATTERN.lastIndex = 0
  let match: RegExpExecArray | null
  while ((match = INLINE_SLASH_COMMAND_PATTERN.exec(value)) !== null) {
    const start = match.index
    const commandEnd = start + match[0].length
    const end = value[commandEnd] === ' ' ? commandEnd + 1 : commandEnd
    if (cursor > start && cursor <= end) {
      INLINE_SLASH_COMMAND_PATTERN.lastIndex = 0
      return { start, end }
    }
  }
  INLINE_SLASH_COMMAND_PATTERN.lastIndex = 0
  return null
}

function getInlineTokenTextRange(value: string, cursor: number): { start: number; end: number } | null {
  INLINE_SLASH_COMMAND_PATTERN.lastIndex = 0
  let match: RegExpExecArray | null
  while ((match = INLINE_SLASH_COMMAND_PATTERN.exec(value)) !== null) {
    const start = match.index
    const end = start + match[0].length
    if (cursor >= start && cursor <= end) {
      INLINE_SLASH_COMMAND_PATTERN.lastIndex = 0
      return { start, end }
    }
  }
  INLINE_SLASH_COMMAND_PATTERN.lastIndex = 0
  return null
}

function nearestInlineTokenBoundary(value: string, cursor: number): number | null {
  const range = getInlineTokenTextRange(value, cursor)
  if (!range || cursor === range.start || cursor === range.end) return null
  return cursor - range.start < range.end - cursor ? range.start : range.end
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

function getTextareaCaretClientRect(textarea: HTMLTextAreaElement): SlashCaretRect {
  const style = window.getComputedStyle(textarea)
  const rect = textarea.getBoundingClientRect()
  const mirror = document.createElement('div')
  const marker = document.createElement('span')
  const selectionEnd = textarea.selectionEnd

  mirror.style.position = 'fixed'
  mirror.style.left = `${rect.left}px`
  mirror.style.top = `${rect.top}px`
  mirror.style.width = `${textarea.clientWidth}px`
  mirror.style.minHeight = `${textarea.clientHeight}px`
  mirror.style.visibility = 'hidden'
  mirror.style.pointerEvents = 'none'
  mirror.style.whiteSpace = 'pre-wrap'
  mirror.style.overflowWrap = 'break-word'
  mirror.style.boxSizing = style.boxSizing
  mirror.style.font = readTextareaFont(style)
  mirror.style.letterSpacing = style.letterSpacing
  mirror.style.lineHeight = style.lineHeight
  mirror.style.padding = style.padding
  mirror.style.border = style.border
  mirror.style.overflow = 'hidden'

  mirror.append(document.createTextNode(textarea.value.slice(0, selectionEnd)))
  marker.textContent = '\u200b'
  mirror.append(marker)
  mirror.append(document.createTextNode(textarea.value.slice(selectionEnd) || '\u200b'))
  document.body.append(mirror)
  const markerRect = marker.getBoundingClientRect()
  mirror.remove()

  return {
    x: markerRect.right,
    top: rect.top,
  }
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
  planMode = false,
  onTogglePlanMode,
  learningModeEnabled = false,
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
    focus: () => {
      textareaRef.current?.focus({ preventScroll: true })
    },
  }))

  const { wrapOnChange } = useInputPerfDebug()
  const trackedSetDraft = useMemo(() => wrapOnChange(setDraft), [wrapOnChange])

  const valueRef = useLatest(draft)
  const onChangeRef = useLatest(setDraft)
  const accessTokenRef = useLatest(accessToken)
  const onAsrErrorRef = useLatest(onAsrError)
  const onVoiceNotConfiguredRef = useLatest<(() => void) | undefined>(() => onOpenSettings?.('voice'))

  const { t } = useLocale()

  const [selectedPersonaKey, setSelectedPersonaKey] = useState(readSelectedPersonaKeyFromStorage)
  const [focused, setFocused] = useState(false)
  const [childMenuOpen, setChildMenuOpen] = useState(false)
  const [collapsingGrid, setCollapsingGrid] = useState(false)
  const [pastedModalAttachment, setPastedModalAttachment] = useState<Attachment | null>(null)
  const [typewriterText, setTypewriterText] = useState('')
  const [workCompactInputWraps, setWorkCompactInputWraps] = useState(false)
  const [textareaFocusRestoreTick, setTextareaFocusRestoreTick] = useState(0)
  const [selectedModel, setSelectedModel] = useState<string | null>(readSelectedModelFromStorage)
  const [slashOpen, setSlashOpen] = useState(false)
  const [slashQuery, setSlashQuery] = useState('')
  const [slashPosition, setSlashPosition] = useState({ left: SLASH_POPUP_VIEWPORT_MARGIN, bottom: 0 })
  const [slashSelectedIndex, setSlashSelectedIndex] = useState(0)
  const [slashRange, setSlashRange] = useState<SlashCommandRange | null>(null)
  const [setupTextHovered, setSetupTextHovered] = useState(false)
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

  useEffect(() => {
    const syncSelectedModel = () => {
      setSelectedModel(readSelectedModelFromStorage())
    }
    window.addEventListener(SELECTED_MODEL_CHANGED_EVENT, syncSelectedModel)
    return () => window.removeEventListener(SELECTED_MODEL_CHANGED_EVENT, syncSelectedModel)
  }, [])

  const handleReasoningModeChange = useCallback((mode: string) => {
    setReasoningMode(mode)
    if (workThreadId) {
      writeThreadReasoningMode(workThreadId, mode)
      return
    }
    writeSelectedReasoningMode(mode)
  }, [workThreadId])

  const handleMenuOpenChange = useCallback((open: boolean) => {
    setChildMenuOpen(open)
    const el = textareaRef.current
    if (!el) return
    if (open) {
      el.blur()
    }
  }, [])

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
  const canUseSlashCommands = appMode === 'work' && !!onTogglePlanMode
  const slashCommandGroups = useMemo<SlashCommandGroup[]>(() => {
    if (!canUseSlashCommands) return []
    return [
      {
        label: t.slashCommands.commandsLabel,
        items: [{
          id: 'setup',
          label: 'setup',
          description: t.slashCommands.setupDesc,
        }],
      },
      {
        label: t.slashCommands.modesLabel,
        items: [{
          id: 'plan',
          label: t.planMode,
          description: t.slashCommands.planDesc,
        }],
      },
    ]
  }, [
    canUseSlashCommands,
    t.planMode,
    t.slashCommands.commandsLabel,
    t.slashCommands.modesLabel,
    t.slashCommands.planDesc,
    t.slashCommands.setupDesc,
  ])
  const slashVisibleGroups = useMemo<SlashCommandGroup[]>(() => {
    const query = slashQuery.trim().toLowerCase()
    return slashCommandGroups
      .map((group) => ({
        ...group,
        items: group.items.filter((item) => {
          if (!query) return true
          return item.id.toLowerCase().startsWith(query) || item.label.toLowerCase().startsWith(query)
        }),
      }))
      .filter((group) => group.items.length > 0)
  }, [slashCommandGroups, slashQuery])
  const slashVisibleItems = useMemo(
    () => slashVisibleGroups.flatMap((group) => group.items),
    [slashVisibleGroups],
  )
  const shouldHighlightSetupCommand = SETUP_COMMAND_PATTERN.test(draft)
  SETUP_COMMAND_PATTERN.lastIndex = 0
  const setupHighlightStyle = useMemo(() => ({
    position: 'absolute' as const,
    inset: 0,
    pointerEvents: 'none' as const,
    overflow: 'visible',
    whiteSpace: 'pre-wrap' as const,
    overflowWrap: 'break-word' as const,
    fontFamily: 'inherit',
    fontSize: '16px',
    fontWeight: 310,
    lineHeight: variant === 'chat' ? 1.45 : undefined,
    letterSpacing: '-0.16px',
    color: 'var(--c-text-primary)',
    zIndex: 1,
  }), [variant])

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

  const updateSlashState = useCallback(() => {
    const textarea = textareaRef.current
    if (!textarea || disabled || document.activeElement !== textarea || slashCommandGroups.length === 0) {
      setSlashOpen(false)
      return
    }

    const cursor = textarea.selectionEnd
    const value = textarea.value
    const range = getSlashCommandRange(value, cursor)
    if (!range) {
      setSlashOpen(false)
      setSlashRange(null)
      return
    }

    const normalizedQuery = range.query.trim().toLowerCase()
    const exactCommand = normalizedQuery.length > 0 && slashCommandGroups.some((group) => (
      group.items.some((item) => (
        item.id.toLowerCase() === normalizedQuery || item.label.toLowerCase() === normalizedQuery
      ))
    ))
    if (exactCommand) {
      setSlashOpen(false)
      setSlashRange(null)
      return
    }

    const visibleCount = slashCommandGroups.reduce((count, group) => (
      count + group.items.filter((item) => {
        if (!normalizedQuery) return true
        return item.id.toLowerCase().startsWith(normalizedQuery) || item.label.toLowerCase().startsWith(normalizedQuery)
      }).length
    ), 0)

    if (visibleCount === 0) {
      setSlashOpen(false)
      setSlashRange(null)
      return
    }

    const caret = getTextareaCaretClientRect(textarea)
    const maxLeft = window.innerWidth - SLASH_POPUP_WIDTH - SLASH_POPUP_VIEWPORT_MARGIN
    setSlashPosition({
      left: Math.max(SLASH_POPUP_VIEWPORT_MARGIN, Math.min(caret.x, maxLeft)),
      bottom: caret.top,
    })
    setSlashRange(range)
    setSlashQuery(range.query)
    setSlashSelectedIndex((index) => Math.min(index, visibleCount - 1))
    setSlashOpen(true)
  }, [disabled, slashCommandGroups])

  const selectSlashItem = useCallback((item: SlashCommandItem) => {
    const textarea = textareaRef.current
    if (!textarea) return
    const cursor = textarea.selectionEnd
    const value = draftRef.current
    const range = slashRange ?? getSlashCommandRange(value, cursor)
    if (!range) return
    const before = value.slice(0, range.start)
    const after = value.slice(range.end)
    const leadingSpace = item.id === 'setup' && before.length > 0 && !/\s$/.test(before) ? ' ' : ''
    const insert = item.id === 'setup' ? `${leadingSpace}/setup ` : ''
    const nextDraft = before + insert + after.replace(/^\s+/, '')
    const nextCursor = before.length + insert.length

    resetHistoryCursor()
    setSlashOpen(false)
    setSlashRange(null)
    setSlashSelectedIndex(0)
    trackedSetDraft(nextDraft)
    if (item.id === 'plan' && !planMode) void onTogglePlanMode?.(planMode)

    requestAnimationFrame(() => {
      const target = textareaRef.current
      if (!target) return
      target.focus({ preventScroll: true })
      target.setSelectionRange(nextCursor, nextCursor)
    })
  }, [draftRef, onTogglePlanMode, planMode, resetHistoryCursor, slashRange, trackedSetDraft])

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

  useEffect(() => {
    updateSlashState()
  }, [draft, updateSlashState])

  useEffect(() => {
    if (!slashOpen) return
    const handleResize = () => updateSlashState()
    const handleScroll = () => updateSlashState()
    window.addEventListener('resize', handleResize)
    window.addEventListener('scroll', handleScroll, true)
    return () => {
      window.removeEventListener('resize', handleResize)
      window.removeEventListener('scroll', handleScroll, true)
    }
  }, [slashOpen, updateSlashState])

  useEffect(() => {
    if (!slashOpen) return
    const handleMouseDown = (event: MouseEvent) => {
      const target = event.target as Node
      if (textareaRef.current?.contains(target)) return
      if ((target as Element).closest?.('[data-slash-popup]')) return
      setSlashOpen(false)
    }
    document.addEventListener('mousedown', handleMouseDown)
    return () => document.removeEventListener('mousedown', handleMouseDown)
  }, [slashOpen])

  useEffect(() => {
    if (!slashOpen) return
    if (slashVisibleItems.length === 0) {
      setSlashOpen(false)
      return
    }
    setSlashSelectedIndex((index) => Math.min(index, slashVisibleItems.length - 1))
  }, [slashOpen, slashVisibleItems.length])

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

  useLayoutEffect(() => {
    if (appMode === 'work' && selectedPersonaKey !== WORK_PERSONA_KEY) {
      persistSelectedPersona(WORK_PERSONA_KEY)
    } else if (appMode !== 'work' && selectedPersonaKey === WORK_PERSONA_KEY) {
      persistSelectedPersona(DEFAULT_PERSONA_KEY)
    }
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

  const moveInlineTokenCursor = (target: HTMLTextAreaElement, cursor: number) => {
    target.setSelectionRange(cursor, cursor)
    requestAnimationFrame(updateSlashState)
  }

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    const isComposing = isComposingEvent(e.nativeEvent)
    const target = e.currentTarget
    if (!isComposing && appMode === 'work' && onTogglePlanMode && matchesShortcut(e.nativeEvent, SHORTCUTS.togglePlanMode)) {
      e.preventDefault()
      setSlashOpen(false)
      void onTogglePlanMode(planMode)
      return
    }
    const collapsedSelection = target.selectionStart === target.selectionEnd
    if (!isComposing && collapsedSelection) {
      const inlineTokenRange = getInlineTokenTextRange(target.value, target.selectionStart)
      if (inlineTokenRange) {
        if (e.key === 'ArrowLeft' && target.selectionStart === inlineTokenRange.end) {
          e.preventDefault()
          moveInlineTokenCursor(target, inlineTokenRange.start)
          return
        }
        if (e.key === 'ArrowRight' && target.selectionStart === inlineTokenRange.start) {
          e.preventDefault()
          moveInlineTokenCursor(target, inlineTokenRange.end)
          return
        }
        const boundary = nearestInlineTokenBoundary(target.value, target.selectionStart)
        if (boundary !== null) {
          moveInlineTokenCursor(target, boundary)
        }
      }
    }
    if (!isComposing && e.key === 'Backspace') {
      if (collapsedSelection) {
        const deletionRange = getInlineTokenDeletionRange(target.value, target.selectionStart)
        if (deletionRange) {
          e.preventDefault()
          const nextDraft = target.value.slice(0, deletionRange.start) + target.value.slice(deletionRange.end)
          resetHistoryCursor()
          trackedSetDraft(nextDraft)
          setSlashOpen(false)
          setSlashRange(null)
          requestAnimationFrame(() => {
            const textarea = textareaRef.current
            if (!textarea) return
            textarea.setSelectionRange(deletionRange.start, deletionRange.start)
          })
          return
        }
      }
    }
    if (!isComposing && slashOpen) {
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSlashSelectedIndex((index) => (
          slashVisibleItems.length === 0 ? 0 : (index - 1 + slashVisibleItems.length) % slashVisibleItems.length
        ))
        return
      }
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSlashSelectedIndex((index) => (
          slashVisibleItems.length === 0 ? 0 : (index + 1) % slashVisibleItems.length
        ))
        return
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        e.preventDefault()
        const item = slashVisibleItems[slashSelectedIndex]
        if (item) selectSlashItem(item)
        return
      }
      if (e.key === 'Escape') {
        e.preventDefault()
        setSlashOpen(false)
        return
      }
    }
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

  const handleTextareaFocus = () => {
    setFocused(true)
    requestAnimationFrame(updateSlashState)
  }

  const handleTextareaBlur = () => {
    setFocused(false)
    window.setTimeout(() => setSlashOpen(false), 150)
  }

  const handleTextareaCursorChange = (target?: HTMLTextAreaElement) => {
    requestAnimationFrame(() => {
      const textarea = target ?? textareaRef.current
      if (!textarea) return
      if (textarea.selectionStart === textarea.selectionEnd) {
        const boundary = nearestInlineTokenBoundary(textarea.value, textarea.selectionStart)
        if (boundary !== null) {
          textarea.setSelectionRange(boundary, boundary)
        }
      }
      updateSlashState()
    })
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

  const renderSetupHighlightedText = () => {
    if (!shouldHighlightSetupCommand) return null
    const nodes: ReactNode[] = []
    let lastIndex = 0
    let match: RegExpExecArray | null
    SETUP_COMMAND_PATTERN.lastIndex = 0
    while ((match = SETUP_COMMAND_PATTERN.exec(draft)) !== null) {
      if (match.index > lastIndex) {
        nodes.push(draft.slice(lastIndex, match.index))
      }
      nodes.push(
        <span
          key={`setup-${match.index}`}
          style={{
            position: 'relative',
          }}
        >
          {setupTextHovered && (
            <span
              aria-hidden="true"
              style={{
                position: 'absolute',
                left: '-5px',
                right: '-4px',
                top: '-1px',
                bottom: '-2px',
                borderRadius: '5px',
                background: SETUP_COMMAND_HOVER_BG,
                zIndex: 0,
              }}
            />
          )}
          <span style={{ color: SETUP_COMMAND_HEAD_COLOR, position: 'relative', zIndex: 1 }}>/</span>
          <span style={{ color: SETUP_COMMAND_TEXT_COLOR, position: 'relative', zIndex: 1 }}>setup</span>
        </span>,
      )
      lastIndex = match.index + match[0].length
    }
    if (lastIndex < draft.length) {
      nodes.push(draft.slice(lastIndex))
    }
    SETUP_COMMAND_PATTERN.lastIndex = 0
    return nodes
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
          (focused || childMenuOpen) && 'is-focused',
        ].filter(Boolean).join(' ')}
        style={{
          borderWidth: '0.5px',
          borderStyle: 'solid',
          borderColor: (focused || childMenuOpen)
            ? 'var(--c-input-border-color-focus)'
            : 'var(--c-input-border-color)',
          borderRadius: isWorkChat ? (isWorkCompactInput ? '12px' : '16px') : '20px',
          boxShadow: (focused || childMenuOpen)
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
        <div
          className="flex items-center"
          style={{
            gap: '2px',
            minHeight: isWorkChat ? '34.5px' : '32px',
            width: '100%',
            minWidth: 0,
            flexWrap: isWorkCompactInput ? 'nowrap' : 'wrap',
          }}
        >
          <PersonaModelBar
            selectedModel={selectedModel}
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
            planMode={planMode}
            onTogglePlanMode={onTogglePlanMode}
            learningModeEnabled={learningModeEnabled}
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

          <div
            onMouseEnter={() => setSetupTextHovered(true)}
            onMouseLeave={() => setSetupTextHovered(false)}
            style={{
              position: 'relative',
              minWidth: 0,
              ...(isWorkCompactInput
                ? {
                    flex: '1 1 auto',
                    display: 'flex',
                    alignItems: 'center',
                    padding: '0 8px 0 4px',
                  }
                : {
                    order: -1,
                    flex: '0 0 100%',
                    width: '100%',
                    marginBottom: textareaWrapperMarginBottom,
                    ...(isWorkExpandedInput
                      ? { marginLeft: '3.5px', padding: '10px 0 0' }
                      : {}),
                  }),
            }}
          >
            {shouldHighlightSetupCommand && (
              <div aria-hidden="true" style={setupHighlightStyle}>
                {renderSetupHighlightedText()}
              </div>
            )}
            <AutoResizeTextarea
              ref={textareaRef}
              rows={1}
              className="w-full resize-none bg-transparent outline-none placeholder:text-[var(--c-placeholder)] placeholder:font-[360] disabled:cursor-not-allowed"
              value={draft}
              onChange={(e) => handleDraftChange(e.currentTarget)}
              onKeyDown={handleKeyDown}
              onKeyUp={(e) => handleTextareaCursorChange(e.currentTarget)}
              onClick={(e) => handleTextareaCursorChange(e.currentTarget)}
              onCompositionStart={handleCompositionStart}
              onCompositionEnd={(e) => handleCompositionEnd(e.currentTarget)}
              onPaste={handleTextareaPaste}
              onFocus={handleTextareaFocus}
              onBlur={handleTextareaBlur}
              placeholder={resolvedPlaceholder}
              disabled={disabled}
              minRows={1}
              maxHeight={300}
              style={{
                fontFamily: 'inherit',
                fontSize: '16px',
                fontWeight: 310,
                ...(variant === 'chat' ? { lineHeight: 1.45 as const } : {}),
                color: shouldHighlightSetupCommand ? 'transparent' : 'var(--c-text-primary)',
                caretColor: 'var(--c-text-primary)',
                marginTop: '0px',
                marginBottom: '0px',
                position: 'relative',
                zIndex: 2,
                ...(isWorkChat ? { display: 'block', padding: 0, border: 'none' } : {}),
                ...(isWorkCompactInput ? { flex: '1 1 auto', minWidth: 0 } : {}),
                letterSpacing: '-0.16px',
              }}
            />
          </div>

          {isWorkCompactInput && (
            <div style={{ flexShrink: 0, marginRight: '4px', display: 'flex', alignItems: 'center', position: 'relative' }}>
              <ModelPicker
                accessToken={accessToken}
                value={selectedModel}
                onChange={handleModelChange}
                onAddModel={() => onOpenSettings?.('models')}
                variant={variant}
                thinkingEnabled={reasoningMode}
                onThinkingChange={handleReasoningModeChange}
                onOpenChange={handleMenuOpenChange}
              />
            </div>
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
                <ActionIconButton
                  type="submit"
                  disabled={!draft.trim() && attachments.length === 0}
                  tooltip={t.sendAction}
                  shortcut={SHORTCUTS.sendMessage.binding}
                  className="flex h-full w-full items-center justify-center rounded-lg bg-[var(--c-accent-send)] text-[var(--c-accent-send-text)] hover:bg-[var(--c-accent-send-hover)] active:opacity-[0.75] active:scale-[0.93] disabled:cursor-not-allowed"
                  wrapperStyle={{ position: 'absolute', inset: 0, width: '100%', height: '100%' }}
                  style={{
                    position: 'absolute',
                    inset: 0,
                  }}
                >
                  <ArrowUp size={17} />
                </ActionIconButton>
              ) : (
                <ActionIconButton
                  type="button"
                  onClick={onCancel}
                  disabled={cancelSubmitting}
                  tooltip={t.stopAction}
                  className="flex h-full w-full items-center justify-center rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-input)] transition-[opacity,transform,background-color] duration-[140ms] hover:bg-[var(--c-bg-sub)] active:scale-[0.97] active:opacity-[0.82] disabled:cursor-not-allowed disabled:opacity-50"
                  wrapperStyle={{ position: 'absolute', inset: 0, width: '100%', height: '100%' }}
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
                      border: '1.3px solid var(--c-text-primary)',
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
                        background: 'var(--c-text-primary)',
                        flexShrink: 0,
                      }}
                    />
                  </span>
                </ActionIconButton>
              )
            ) : (
              <>
                <ActionIconButton
                  type="button"
                  onClick={startRecording}
                  disabled={isRecording || isTranscribing || !accessToken}
                  tooltip={t.recordVoiceAction}
                  className="flex h-full w-full items-center justify-center rounded-lg text-[var(--c-text-secondary)] hover:bg-[var(--c-bg-deep)] disabled:cursor-not-allowed disabled:opacity-30"
                  wrapperStyle={{
                    position: 'absolute',
                    inset: 0,
                    width: '100%',
                    height: '100%',
                    pointerEvents: showSendButton ? 'none' : 'auto',
                  }}
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
                </ActionIconButton>
                <ActionIconButton
                  type="submit"
                  disabled={(!isEditingQueuedPrompt && isStreaming) || (!draft.trim() && attachments.length === 0)}
                  tooltip={t.sendAction}
                  shortcut={SHORTCUTS.sendMessage.binding}
                  className="flex h-full w-full items-center justify-center rounded-lg bg-[var(--c-accent-send)] text-[var(--c-accent-send-text)] hover:bg-[var(--c-accent-send-hover)] active:opacity-[0.75] active:scale-[0.93] disabled:cursor-not-allowed"
                  wrapperStyle={{
                    position: 'absolute',
                    inset: 0,
                    width: '100%',
                    height: '100%',
                    pointerEvents: showSendButton ? 'auto' : 'none',
                  }}
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
                </ActionIconButton>
              </>
            )}
          </div>
        </div>
      </form>

      {slashOpen && slashVisibleGroups.length > 0 && (
        <SlashCommandPopup
          groups={slashVisibleGroups}
          selectedIndex={slashSelectedIndex}
          position={slashPosition}
          onSelect={selectSlashItem}
          onMouseEnter={setSlashSelectedIndex}
        />
      )}

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
