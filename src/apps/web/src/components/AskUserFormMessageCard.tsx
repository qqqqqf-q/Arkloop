import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { ChevronDown, ChevronUp } from 'lucide-react'
import type { CSSProperties } from 'react'
import { PillToggle } from '@arkloop/shared'
import type { AgentAskUserFormContent } from '../agent-ui'
import type { FieldSchema, FieldValue } from '../userInputTypes'
import {
  isEnumField,
  isOneOfField,
  isArrayEnumField,
  isArrayAnyOfField,
  isBooleanField,
  isTextField,
  isNumberField,
} from '../userInputTypes'
import { useLocale } from '../contexts/LocaleContext'

interface Props {
  content: AgentAskUserFormContent
  activeRunId: string | null
  onSubmit: (requestId: string, answers: Record<string, FieldValue>) => Promise<void>
  onDismiss: (requestId: string) => Promise<void>
}

function formatFieldValue(value: unknown, t: { yes: string; no: string }): string {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'boolean') return value ? t.yes : t.no
  if (Array.isArray(value)) return value.map(String).join(', ')
  return String(value)
}

function SubmittedAnswersView({ content }: { content: AgentAskUserFormContent }) {
  const [expanded, setExpanded] = useState(false)
  const { t } = useLocale()
  const answers = content.answers ?? {}
  const keys = content.schema._fieldOrder ?? Object.keys(answers)

  return (
    <div
      className="flex flex-col w-full"
      style={{
        background: 'var(--c-bg-input)',
        borderWidth: '0.5px',
        borderStyle: 'solid',
        borderColor: 'var(--c-border-subtle)',
        borderRadius: '20px',
        padding: '18px 22px 16px',
        opacity: 0.85,
      }}
    >
      <div className="flex items-center justify-between gap-3 mb-2">
        <h2 className="text-[15px] font-normal leading-snug m-0 flex-1" style={{ color: 'var(--c-text-secondary)' }}>
          {content.message}
        </h2>
      </div>

      {keys.length > 0 && (
        <button
          type="button"
          onClick={() => setExpanded(!expanded)}
          className="flex items-center gap-1.5 text-[12px] font-medium border-none bg-transparent cursor-pointer mt-1 mb-2"
          style={{ color: 'var(--c-text-muted)' }}
        >
          {expanded ? <ChevronUp size={12} /> : <ChevronDown size={12} />}
          {expanded ? t.userInput.hideAnswers : t.userInput.showAnswers(keys.length)}
        </button>
      )}

      {expanded && (
        <div className="flex flex-col gap-2 mt-1">
          {keys.map((key: string) => {
            const fieldSchema = content.schema.properties[key]
            const title = (fieldSchema && typeof fieldSchema === 'object' && 'title' in fieldSchema)
              ? (fieldSchema.title as string) ?? key
              : key
            const value = answers[key]
            return (
              <div key={key} className="flex items-start gap-3 px-2 py-1.5 rounded-lg" style={{ background: 'var(--c-bg-deep)' }}>
                <span className="text-[13px] font-medium flex-shrink-0" style={{ color: 'var(--c-text-secondary)', minWidth: '80px' }}>
                  {title}
                </span>
                <span className="text-[13px] font-light flex-1" style={{ color: 'var(--c-text-primary)' }}>
                  {formatFieldValue(value, t.userInput)}
                </span>
              </div>
            )
          })}
          {content.submittedAt && (
            <div className="text-[11px] mt-1" style={{ color: 'var(--c-text-muted)' }}>
              {new Date(content.submittedAt).toLocaleString()}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

// --- Editable form fields ---

function FieldLabel({ title, description }: { title?: string; description?: string }) {
  if (!title && !description) return null
  return (
    <div className="mb-1">
      {title && <span className="text-[14px] font-medium" style={{ color: 'var(--c-text-primary)' }}>{title}</span>}
      {description && (
        <span className={title ? 'ml-2 text-[12px]' : 'text-[12.5px]'} style={{ color: 'var(--c-text-muted)' }}>{description}</span>
      )}
    </div>
  )
}

// --- PopoverSelect (portal-based, shared by SelectField / OneOfSelectField) ---

function PopoverSelect({
  value,
  placeholder,
  options,
  disabled,
  onChange,
}: {
  value: string | undefined
  placeholder: string
  options: Array<{ value: string; label: string }>
  disabled: boolean
  onChange: (val: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [menuStyle, setMenuStyle] = useState<CSSProperties>({})
  const triggerRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (
        menuRef.current?.contains(e.target as Node) ||
        triggerRef.current?.contains(e.target as Node)
      ) return
      setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  // Close on scroll to avoid misalignment (the form is in a scrollable container)
  useEffect(() => {
    if (!open) return
    const handler = () => setOpen(false)
    window.addEventListener('scroll', handler, true)
    return () => window.removeEventListener('scroll', handler, true)
  }, [open])

  const handleOpen = () => {
    if (disabled) return
    if (!open && triggerRef.current) {
      const rect = triggerRef.current.getBoundingClientRect()
      const viewportHeight = window.innerHeight
      const viewportWidth = window.innerWidth
      const margin = 8
      const menuGap = 4
      const preferredMaxHeight = 220
      const minUsefulHeight = 88
      const estimatedMenuHeight = Math.min(preferredMaxHeight, options.length * 37 + 8)
      const spaceBelow = viewportHeight - rect.bottom - margin - menuGap
      const spaceAbove = rect.top - margin - menuGap
      const openAbove = spaceBelow < Math.min(estimatedMenuHeight, 150) && spaceAbove > spaceBelow
      const availableHeight = Math.max(minUsefulHeight, openAbove ? spaceAbove : spaceBelow)
      const maxHeight = Math.min(preferredMaxHeight, availableHeight)
      const left = Math.max(margin, Math.min(rect.left, viewportWidth - rect.width - margin))
      setMenuStyle({
        position: 'fixed',
        top: openAbove ? rect.top - menuGap - maxHeight : rect.bottom + menuGap,
        left,
        width: rect.width,
        maxHeight,
        zIndex: 9999,
      })
    }
    setOpen((v) => !v)
  }

  const selectOption = useCallback((opt: string) => {
    onChange(opt)
    setOpen(false)
  }, [onChange])

  const displayLabel = value
    ? (options.find(o => o.value === value)?.label ?? value)
    : placeholder

  const menu = open ? (
    <div
      ref={menuRef}
      style={{
        ...menuStyle,
        background: 'var(--c-bg-page)',
        border: '0.5px solid var(--c-border)',
        borderRadius: '8px',
        boxShadow: '0 4px 12px rgba(0,0,0,0.15)',
        overflowY: 'auto',
      }}
    >
      {options.map((opt) => {
        const selected = value === opt.value
        return (
          <div
            key={opt.value}
            role="option"
            aria-selected={selected}
            onClick={() => selectOption(opt.value)}
            className="flex items-center px-3 py-2 text-[14px] cursor-pointer transition-[background-color] duration-[60ms]"
            style={{
              background: selected ? 'var(--c-bg-sub)' : 'transparent',
              color: 'var(--c-text-primary)',
            }}
            onMouseEnter={(e) => { if (!selected) e.currentTarget.style.background = 'var(--c-bg-deep)' }}
            onMouseLeave={(e) => { if (!selected) e.currentTarget.style.background = 'transparent' }}
          >
            {opt.label}
          </div>
        )
      })}
    </div>
  ) : null

  return (
    <div>
      <button
        ref={triggerRef}
        type="button"
        onClick={handleOpen}
        disabled={disabled}
        className="flex items-center justify-between w-full rounded-lg px-3 py-2 text-[14px] font-light outline-none cursor-pointer disabled:opacity-40"
        style={{
          background: 'var(--c-bg-deep)',
          color: value ? 'var(--c-text-primary)' : 'var(--c-text-muted)',
          border: '0.5px solid var(--c-border-subtle)',
          minHeight: '36px',
        }}
      >
        <span>{displayLabel}</span>
        <ChevronDown size={14} style={{ color: 'var(--c-text-tertiary)', transform: open ? 'rotate(180deg)' : 'none', transition: 'transform 150ms ease' }} />
      </button>
      {menu && createPortal(menu, document.body)}
    </div>
  )
}

function SelectField({
  field, value, required, disabled, onChange,
}: {
  field: { title?: string; description?: string; enum: string[]; enumNames?: string[] }
  value: string | undefined
  required: boolean
  disabled: boolean
  onChange: (val: string) => void
}) {
  const { t } = useLocale()
  const options = useMemo(() =>
    field.enum.map((v, i) => ({ value: v, label: field.enumNames?.[i] ?? v })),
    [field.enum, field.enumNames],
  )
  return (
    <div>
      <FieldLabel title={field.title} description={field.description} />
      <PopoverSelect
        value={value}
        placeholder={required ? t.userInput.selectPlaceholder : t.userInput.optionalPlaceholder}
        options={options}
        disabled={disabled}
        onChange={onChange}
      />
    </div>
  )
}

function OneOfSelectField({
  field, value, required, disabled, onChange,
}: {
  field: { title?: string; description?: string; oneOf: Array<{ const: string; title: string }> }
  value: string | undefined
  required: boolean
  disabled: boolean
  onChange: (val: string) => void
}) {
  const { t } = useLocale()
  const options = useMemo(() =>
    field.oneOf.map(o => ({ value: o.const, label: o.title })),
    [field.oneOf],
  )
  return (
    <div>
      <FieldLabel title={field.title} description={field.description} />
      <PopoverSelect
        value={value}
        placeholder={required ? t.userInput.selectPlaceholder : t.userInput.optionalPlaceholder}
        options={options}
        disabled={disabled}
        onChange={onChange}
      />
    </div>
  )
}

function MultiSelectField({
  field, value, disabled, onChange,
}: {
  field: import('../userInputTypes').ArrayEnumFieldSchema | import('../userInputTypes').ArrayAnyOfFieldSchema
  value: string[]
  disabled: boolean
  onChange: (val: string[]) => void
}) {
  const toggle = useCallback((opt: string) => {
    onChange(value.includes(opt) ? value.filter(x => x !== opt) : [...value, opt])
  }, [value, onChange])

  const options = isArrayEnumField(field)
    ? field.items.enum.map(v => ({ value: v, label: v }))
    : field.items.anyOf.map(o => ({ value: o.const, label: o.title }))

  return (
    <div>
      <FieldLabel title={field.title} description={field.description} />
      <div className="flex flex-col gap-0.5">
        {options.map(opt => (
          <label
            key={opt.value}
            className="flex items-center gap-2.5 rounded-lg px-2.5 py-2 cursor-pointer transition-[background-color] duration-[60ms]"
            style={{ opacity: disabled ? 0.5 : 1 }}
            onMouseEnter={(e) => e.currentTarget.style.background = 'var(--c-bg-deep)'}
            onMouseLeave={(e) => e.currentTarget.style.background = 'transparent'}
          >
            <input
              type="checkbox"
              checked={value.includes(opt.value)}
              onChange={() => toggle(opt.value)}
              disabled={disabled}
              className="rounded"
              style={{ accentColor: 'var(--c-text-primary)' }}
            />
            <span className="text-[14px] font-light flex-1" style={{ color: 'var(--c-text-primary)' }}>{opt.label}</span>
          </label>
        ))}
      </div>
    </div>
  )
}

function BooleanField({
  field, value, disabled, onChange,
}: {
  field: { title?: string; description?: string }
  value: boolean | undefined
  disabled: boolean
  onChange: (val: boolean) => void
}) {
  const [hovered, setHovered] = useState(false)
  return (
    <div>
      <label
        className="flex items-center justify-between gap-3 cursor-pointer rounded-lg px-2 py-2.5 transition-[background-color] duration-[60ms]"
        style={{ opacity: disabled ? 0.5 : 1 }}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      >
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-[14.5px] font-light truncate" style={{ color: 'var(--c-text-primary)' }}>{field.title}</span>
          {field.description && (
            <span className="text-[12px] truncate" style={{ color: 'var(--c-text-muted)' }}>{field.description}</span>
          )}
        </div>
        <PillToggle
          checked={value ?? false}
          onChange={() => onChange(!value)}
          disabled={disabled}
          forceHover={hovered}
          size="sm"
        />
      </label>
    </div>
  )
}

function TextInputField({
  fieldKey, field, value, disabled, onChange,
}: {
  fieldKey: string
  field: { title?: string; description?: string; maxLength?: number }
  value: string
  disabled: boolean
  onChange: (val: string) => void
}) {
  return (
    <div>
      <FieldLabel title={field.title} description={field.description} />
      <input
        id={`ask-form-${fieldKey}`}
        type="text"
        value={value}
        onChange={e => onChange(e.target.value)}
        maxLength={field.maxLength}
        disabled={disabled}
        className="w-full rounded-lg px-3 py-2 text-[14px] font-light outline-none"
        style={{
          background: 'var(--c-bg-deep)',
          color: 'var(--c-text-primary)',
          border: '0.5px solid var(--c-border-subtle)',
          caretColor: 'var(--c-text-primary)',
        }}
      />
    </div>
  )
}

function NumberInputField({
  fieldKey, field, value, disabled, onChange,
}: {
  fieldKey: string
  field: { title?: string; description?: string; minimum?: number; maximum?: number; type: 'number' | 'integer' }
  value: number | undefined
  disabled: boolean
  onChange: (val: number) => void
}) {
  return (
    <div>
      <FieldLabel title={field.title} description={field.description} />
      <input
        id={`ask-form-${fieldKey}`}
        type="number"
        value={value ?? ''}
        onChange={e => {
          const v = field.type === 'integer' ? parseInt(e.target.value, 10) : parseFloat(e.target.value)
          if (!isNaN(v)) onChange(v)
        }}
        min={field.minimum}
        max={field.maximum}
        step={field.type === 'integer' ? 1 : 'any'}
        disabled={disabled}
        className="w-full rounded-lg px-3 py-2 text-[14px] font-light outline-none"
        style={{
          background: 'var(--c-bg-deep)',
          color: 'var(--c-text-primary)',
          border: '0.5px solid var(--c-border-subtle)',
          caretColor: 'var(--c-text-primary)',
        }}
      />
    </div>
  )
}

function getDefaultValue(field: FieldSchema): FieldValue | undefined {
  if ('default' in field && field.default !== undefined) {
    return field.default as FieldValue
  }
  return undefined
}

function EditableFormView({
  content,
  disabled,
  onSubmit,
  onDismiss,
}: {
  content: AgentAskUserFormContent
  disabled: boolean
  onSubmit: (answers: Record<string, FieldValue>) => void
  onDismiss: () => void
}) {
  const { t } = useLocale()
  const fields = useMemo(() => {
    const order = content.schema._fieldOrder
    const props = content.schema.properties as Record<string, FieldSchema>
    if (order) {
      return order
        .filter(key => key in props)
        .map(key => [key, props[key]] as [string, FieldSchema])
    }
    return Object.entries(props)
  }, [content.schema])

  const requiredSet = useMemo(() => {
    return new Set(content.schema.required ?? [])
  }, [content.schema.required])

  const [values, setValues] = useState<Record<string, FieldValue>>(() => {
    const initial: Record<string, FieldValue> = {}
    for (const [key, field] of Object.entries(content.schema.properties as Record<string, FieldSchema>)) {
      const def = getDefaultValue(field)
      if (def !== undefined) initial[key] = def
    }
    return initial
  })

  const [submitting, setSubmitting] = useState(false)

  const setValue = useCallback((key: string, val: FieldValue) => {
    setValues(prev => ({ ...prev, [key]: val }))
  }, [])

  const allValid = useMemo(() => {
    for (const key of requiredSet) {
      const v = values[key]
      if (v === undefined || v === '' || (Array.isArray(v) && v.length === 0)) return false
    }
    return true
  }, [values, requiredSet])

  const doSubmit = useCallback(() => {
    if (!allValid || submitting || disabled) return
    setSubmitting(true)
    onSubmit(values)
  }, [allValid, submitting, disabled, onSubmit, values])

  const handleDismiss = useCallback(() => {
    if (submitting || disabled) return
    onDismiss()
  }, [submitting, disabled, onDismiss])

  return (
    <div
      className="flex flex-col w-full"
      style={{
        background: 'var(--c-bg-input)',
        borderWidth: '0.5px',
        borderStyle: 'solid',
        borderColor: 'var(--c-border-subtle)',
        borderRadius: '20px',
        padding: '18px 22px 16px',
      }}
    >
      <h2 className="text-[15px] font-normal leading-snug m-0 mb-4" style={{ color: 'var(--c-text-secondary)' }}>
        {content.message}
      </h2>

      <div className="flex flex-col gap-4 max-h-[50vh] overflow-y-auto pr-1">
        {fields.map(([key, field]) => {
          if (isEnumField(field)) {
            return (
              <SelectField
                key={key}
                field={field}
                value={values[key] as string | undefined}
                required={requiredSet.has(key)}
                disabled={submitting || disabled}
                onChange={val => setValue(key, val)}
              />
            )
          }
          if (isOneOfField(field)) {
            return (
              <OneOfSelectField
                key={key}
                field={field}
                value={values[key] as string | undefined}
                required={requiredSet.has(key)}
                disabled={submitting || disabled}
                onChange={val => setValue(key, val)}
              />
            )
          }
          if (isArrayEnumField(field) || isArrayAnyOfField(field)) {
            return (
              <MultiSelectField
                key={key}
                field={field}
                value={(values[key] as string[]) ?? []}
                disabled={submitting || disabled}
                onChange={val => setValue(key, val)}
              />
            )
          }
          if (isBooleanField(field)) {
            return (
              <BooleanField
                key={key}
                field={field}
                value={values[key] as boolean | undefined}
                disabled={submitting || disabled}
                onChange={val => setValue(key, val)}
              />
            )
          }
          if (isNumberField(field)) {
            return (
              <NumberInputField
                key={key}
                fieldKey={key}
                field={field}
                value={values[key] as number | undefined}
                disabled={submitting || disabled}
                onChange={val => setValue(key, val)}
              />
            )
          }
          if (isTextField(field)) {
            return (
              <TextInputField
                key={key}
                fieldKey={key}
                field={field}
                value={(values[key] as string) ?? ''}
                disabled={submitting || disabled}
                onChange={val => setValue(key, val)}
              />
            )
          }
          return null
        })}
      </div>

      <div className="flex items-center justify-end gap-1.5 pt-3 mt-3" style={{ borderTop: '0.5px solid var(--c-border-subtle)' }}>
        <button
          type="button"
          onClick={handleDismiss}
          disabled={submitting || disabled}
          className="rounded-lg px-3 py-1.5 text-[13px] border-none bg-transparent cursor-pointer transition-[background-color] duration-[60ms] disabled:opacity-40 hover:bg-[var(--c-bg-deep)]"
          style={{ color: 'var(--c-text-secondary)' }}
        >
          {t.userInput.dismiss}
        </button>
        <button
          type="button"
          onClick={doSubmit}
          disabled={!allValid || submitting || disabled}
          className="flex h-7 items-center gap-1.5 rounded-lg px-3 border-none cursor-pointer transition-[background-color,color] duration-[60ms] disabled:opacity-30 text-[13px] font-medium"
          style={{
            background: allValid && !submitting ? 'var(--c-text-primary)' : 'var(--c-bg-deep)',
            color: allValid && !submitting ? 'var(--c-bg-page)' : 'var(--c-text-muted)',
          }}
        >
          {submitting ? t.userInput.submitting : t.userInput.submit}
        </button>
      </div>
    </div>
  )
}

export default function AskUserFormMessageCard({ content, activeRunId, onSubmit, onDismiss }: Props) {
  const isPending = content.status === 'pending'
  const isEditable = isPending && activeRunId === content.runId

  const handleSubmit = useCallback(async (answers: Record<string, FieldValue>) => {
    await onSubmit(content.requestId, answers)
  }, [onSubmit, content.requestId])

  const handleDismiss = useCallback(async () => {
    await onDismiss(content.requestId)
  }, [onDismiss, content.requestId])

  if (isEditable) {
    return (
      <EditableFormView
        content={content}
        disabled={!activeRunId}
        onSubmit={handleSubmit}
        onDismiss={handleDismiss}
      />
    )
  }

  return <SubmittedAnswersView content={content} />
}
