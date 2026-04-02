import { useRef, useEffect, useCallback, useMemo, useState } from 'react'
import { ArrowUp, Mic, X, Check, Loader2 } from 'lucide-react'
import type { FormEvent, KeyboardEvent, ClipboardEvent as ReactClipboardEvent } from 'react'
import { listSelectablePersonas, type SelectablePersona, type UploadedThreadAttachment } from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { PastedContentModal } from './PastedContentModal'
import type { SettingsTab } from './SettingsModal'
import {
  DEFAULT_PERSONA_KEY,
  SEARCH_PERSONA_KEY,
  readSelectedPersonaKeyFromStorage,
  writeSelectedPersonaKeyToStorage,
  readSelectedModelFromStorage,
  writeSelectedModelToStorage,
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
import { AutoResizeTextarea } from '@arkloop/shared'

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
  appMode?: AppMode
  hasMessages?: boolean
  clawThreadId?: string
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
  appMode,
  hasMessages,
  clawThreadId,
}: Props) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const valueRef = useRef(value)
  const onChangeRef = useRef(onChange)
  const accessTokenRef = useRef(accessToken)
  const onAsrErrorRef = useRef(onAsrError)
  const onVoiceNotConfiguredRef = useRef<(() => void) | undefined>(() => onOpenSettings?.('voice' as never))
  useEffect(() => { valueRef.current = value }, [value])
  useEffect(() => { onChangeRef.current = onChange }, [onChange])
  useEffect(() => { accessTokenRef.current = accessToken }, [accessToken])
  useEffect(() => { onAsrErrorRef.current = onAsrError }, [onAsrError])
  useEffect(() => { onVoiceNotConfiguredRef.current = () => onOpenSettings?.('voice' as never) }, [onOpenSettings])

  const { t } = useLocale()

  const [selectablePersonas, setSelectablePersonas] = useState<SelectablePersona[]>([])
  const [selectedPersonaKey, setSelectedPersonaKey] = useState(readSelectedPersonaKeyFromStorage)
  const [focused, setFocused] = useState(false)
  const [collapsingGrid, setCollapsingGrid] = useState(false)
  const [pastedModalAttachment, setPastedModalAttachment] = useState<Attachment | null>(null)
  const [chipExiting, setChipExiting] = useState(false)
  const [selectedModel, setSelectedModel] = useState<string | null>(readSelectedModelFromStorage)
  const [submittedText, setSubmittedText] = useState<string | null>(null)

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
  }, [])

  const isNonDefaultMode = selectedPersonaKey !== DEFAULT_PERSONA_KEY
  const showSendButton = value.trim().length > 0 || attachments.length > 0

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

  const handleKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      if (!disabled && value.trim()) {
        setSubmittedText(value)
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
    <div
      className="w-full"
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: '8px',
        maxWidth: variant === 'welcome' ? '840px' : '720px',
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
      <form
        onSubmit={(e) => onSubmit(e, selectedPersonaKey, selectedModel ?? undefined)}
        style={{
          padding: variant === 'welcome' ? '10px 14px 14px 22px' : '6px 12px 11px 20px',
        }}
      >
        <div
          style={{
            position: 'relative',
            marginBottom: variant === 'welcome' ? '12px' : '9px',
          }}
        >
          <AutoResizeTextarea
            ref={textareaRef}
            rows={1}
            className="w-full resize-none bg-transparent outline-none placeholder:text-[var(--c-placeholder)] placeholder:font-[360] disabled:cursor-not-allowed"
            value={submittedText ?? value}
            onChange={(e) => { if (submittedText === null) onChange(e.target.value) }}
            onKeyDown={handleKeyDown}
            onPaste={handleTextareaPaste}
            onFocus={() => setFocused(true)}
            onBlur={() => setFocused(false)}
            placeholder={placeholder}
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
              letterSpacing: '-0.16px',
            }}
          />
        </div>

        <div className="flex items-center" style={{ gap: '2px', minHeight: '32px' }}>
          <PersonaModelBar
            personas={personas}
            selectedPersonaKey={selectedPersonaKey}
            selectedModel={selectedModel}
            chipExiting={chipExiting}
            isNonDefaultMode={isNonDefaultMode}
            selectedPersona={selectedPersona}
            onModeSelect={handleModeSelect}
            onDeactivateMode={deactivateMode}
            onModelChange={handleModelChange}
            onOpenSettings={onOpenSettings}
            onFileInputClick={() => fileInputRef.current?.click()}
            accessToken={accessToken}
            variant={variant}
            appMode={appMode}
            threadHasMessages={hasMessages}
            clawThreadId={clawThreadId}
          />

          {/* mic + send 共用同一位置，disabled 时显示 spinner */}
          <div style={{ position: 'relative', width: '31.5px', height: '31.5px', flexShrink: 0 }}>
            {disabled ? (
              <div className="flex h-full w-full items-center justify-center rounded-lg bg-[var(--c-accent-send)]" style={{ opacity: 0.5 }}>
                <Loader2 size={14} className="animate-spin" style={{ color: 'var(--c-accent-send-text)' }} />
              </div>
            ) : isStreaming && canCancel ? (
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
                    width: '16px',
                    height: '16px',
                    borderRadius: '999px',
                    border: '1.5px solid #1A1A19',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    flexShrink: 0,
                  }}
                >
                  <span
                    style={{
                      width: '6px',
                      height: '6px',
                      borderRadius: '1px',
                      background: '#1A1A19',
                      flexShrink: 0,
                    }}
                  />
                </span>
              </button>
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
                  disabled={isStreaming || (!value.trim() && attachments.length === 0)}
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
            borderRadius: '20px',
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
}
