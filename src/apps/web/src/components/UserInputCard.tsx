import { useCallback, useEffect, useMemo, useState } from 'react'
import { ArrowLeft, ArrowRight, Check, X } from 'lucide-react'
import { useLocale } from '../contexts/LocaleContext'
import type {
  FieldSchema,
  FieldValue,
  UserInputRequest,
  UserInputResponse,
} from '../userInputTypes'
import {
  isEnumField,
  isOneOfField,
  isArrayEnumField,
  isArrayAnyOfField,
  isBooleanField,
  isTextField,
  isNumberField,
} from '../userInputTypes'

function getDefaultValue(field: FieldSchema): FieldValue | undefined {
  if ('default' in field && field.default !== undefined) {
    return field.default as FieldValue
  }
  return undefined
}

function buildInitialValues(schema: UserInputRequest['requestedSchema']): Record<string, FieldValue> {
  const initial: Record<string, FieldValue> = {}
  for (const [key, field] of Object.entries(schema.properties)) {
    const def = getDefaultValue(field)
    if (def !== undefined) {
      initial[key] = def
    }
  }
  return initial
}

interface Props {
  request: UserInputRequest
  onSubmit: (response: UserInputResponse) => void
  onDismiss: () => void
  disabled?: boolean
}

export default function UserInputCard({ request, onSubmit, onDismiss, disabled }: Props) {
  const { t } = useLocale()
  const fields = useMemo(() => {
    const order = request.requestedSchema._fieldOrder
    if (order) {
      return order
        .filter(key => key in request.requestedSchema.properties)
        .map(key => [key, request.requestedSchema.properties[key]] as [string, FieldSchema])
    }
    return Object.entries(request.requestedSchema.properties)
  }, [request])
  const requiredSet = useMemo(() => {
    const req = request.requestedSchema.required
    return new Set(Array.isArray(req) ? req : [])
  }, [request])
  const [values, setValues] = useState<Record<string, FieldValue>>(() => buildInitialValues(request.requestedSchema))
  const [submitting, setSubmitting] = useState(false)
  const [cardHovered, setCardHovered] = useState(false)
  const [page, setPage] = useState(0)

  const useWizard = fields.length > 1
  const isLastPage = page === fields.length - 1

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

  const currentFieldValid = useMemo(() => {
    if (!useWizard) return true
    const [key] = fields[page]
    if (!requiredSet.has(key)) return true
    const v = values[key]
    return v !== undefined && v !== '' && !(Array.isArray(v) && v.length === 0)
  }, [useWizard, fields, page, values, requiredSet])

  const doSubmit = useCallback(() => {
    if (!allValid || submitting || disabled) return
    setSubmitting(true)
    onSubmit({ type: 'user_input_response', request_id: request.request_id, answers: values })
  }, [allValid, submitting, disabled, onSubmit, request.request_id, values])

  // 单选快速提交: 合并当前值和新选择值后直接提交，避免 stale closure
  const quickSubmit = useCallback((key: string, val: FieldValue) => {
    if (submitting || disabled) return
    const merged = { ...values, [key]: val }
    setValues(merged)
    setSubmitting(true)
    onSubmit({ type: 'user_input_response', request_id: request.request_id, answers: merged })
  }, [submitting, disabled, values, onSubmit, request.request_id])

  const handleSelectAdvance = useCallback((key: string, val: FieldValue) => {
    if (submitting || disabled) return
    const merged = { ...values, [key]: val }
    setValues(merged)
    if (!isLastPage) {
      setPage(p => p + 1)
    } else {
      let valid = true
      for (const reqKey of requiredSet) {
        const v = merged[reqKey]
        if (v === undefined || v === '' || (Array.isArray(v) && v.length === 0)) { valid = false; break }
      }
      if (valid) {
        setSubmitting(true)
        onSubmit({ type: 'user_input_response', request_id: request.request_id, answers: merged })
      }
    }
  }, [submitting, disabled, values, isLastPage, requiredSet, onSubmit, request.request_id])

  const goNext = useCallback(() => {
    if (currentFieldValid && !isLastPage) setPage(p => p + 1)
  }, [currentFieldValid, isLastPage])

  const goBack = useCallback(() => {
    if (page > 0) setPage(p => p - 1)
  }, [page])

  const handleDismiss = useCallback(() => {
    if (submitting || disabled) return
    onDismiss()
  }, [submitting, disabled, onDismiss])

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') { e.preventDefault(); handleDismiss() }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [handleDismiss])

  // 只有单个 select/oneOf 字段时，点选即提交
  const isSingleSelect = fields.length === 1 && (isEnumField(fields[0][1]) || isOneOfField(fields[0][1]))
  const isCurrentSelect = useWizard && (isEnumField(fields[page][1]) || isOneOfField(fields[page][1]))
  const visibleFields = useWizard ? [fields[page]] : fields

  return (
    <div
      className="flex flex-col w-full"
      style={{
        background: 'var(--c-bg-input)',
        borderWidth: '0.5px',
        borderStyle: 'solid',
        borderColor: cardHovered ? 'var(--c-input-border-color-hover)' : 'var(--c-input-border-color)',
        borderRadius: '20px',
        boxShadow: cardHovered ? 'var(--c-input-shadow-hover)' : 'var(--c-input-shadow)',
        transition: 'border-color 0.2s ease, box-shadow 0.2s ease',
        padding: '18px 22px 16px',
      }}
      onMouseEnter={() => setCardHovered(true)}
      onMouseLeave={() => setCardHovered(false)}
    >
      {/* Header */}
      <div className="flex items-start justify-between gap-3 mb-4">
        <h2 className="text-[17px] font-normal leading-snug m-0 flex-1" style={{ color: 'var(--c-text-secondary)' }}>
          {useWizard && page > 0 ? (fields[page][1] as FieldSchema).title ?? request.message : request.message}
        </h2>
        {useWizard && (
          <span className="text-[12px] font-medium flex-shrink-0 mt-1" style={{ color: 'var(--c-text-muted)' }}>
            {page + 1} / {fields.length}
          </span>
        )}
        <button
          type="button"
          onClick={handleDismiss}
          disabled={submitting || !!disabled}
          aria-label={t.userInput.dismiss}
          className="flex h-6 w-6 items-center justify-center rounded-md border-none bg-transparent cursor-pointer disabled:opacity-30 transition-[background-color] duration-[60ms] hover:bg-[var(--c-bg-deep)] flex-shrink-0 mt-0.5"
          style={{ color: 'var(--c-text-muted)' }}
        >
          <X size={13} />
        </button>
      </div>

      {/* Fields */}
      <div className="flex flex-col gap-4">
        {visibleFields.map(([key, field]) => {
          const displayField = useWizard && page > 0 ? { ...field, title: undefined } as typeof field : field
          return (
            <FieldRenderer
              key={key}
              fieldKey={key}
              field={displayField}
              value={values[key]}
              required={requiredSet.has(key)}
              disabled={submitting || !!disabled}
              onChange={val => setValue(key, val)}
              onQuickSubmit={
                isSingleSelect ? (val: FieldValue) => quickSubmit(key, val) :
                isCurrentSelect ? (val: FieldValue) => handleSelectAdvance(key, val) :
                undefined
              }
            />
          )
        })}
      </div>

      {/* Footer */}
      {useWizard ? (
        <div className="flex items-center pt-3 mt-3" style={{ borderTop: '0.5px solid var(--c-border-subtle)' }}>
          <div className="flex-1 flex justify-start">
            {page > 0 && (
              <button
                type="button"
                onClick={goBack}
                disabled={submitting || !!disabled}
                className="flex h-7 items-center gap-1 rounded-lg px-2.5 border-none bg-transparent cursor-pointer transition-[background-color] duration-[60ms] disabled:opacity-30 text-[13px] font-medium hover:bg-[var(--c-bg-deep)]"
                style={{ color: 'var(--c-text-secondary)' }}
              >
                <ArrowLeft size={13} />
                {t.userInput.back}
              </button>
            )}
          </div>
          <div className="flex gap-1.5">
            {fields.map((_, i) => (
              <div
                key={i}
                style={{
                  width: 6, height: 6, borderRadius: '50%',
                  background: i === page ? 'var(--c-text-primary)' : i < page ? 'var(--c-text-muted)' : 'var(--c-border-subtle)',
                  transition: 'background 150ms ease',
                }}
              />
            ))}
          </div>
          <div className="flex-1 flex justify-end">
            {!isCurrentSelect && (
              isLastPage ? (
                <button
                  type="button"
                  onClick={doSubmit}
                  disabled={!allValid || submitting || !!disabled}
                  className="flex h-7 items-center gap-1.5 rounded-lg px-3 border-none cursor-pointer transition-[background-color,color] duration-[60ms] disabled:opacity-30 text-[13px] font-medium"
                  style={{
                    background: allValid && !submitting ? 'var(--c-text-primary)' : 'var(--c-bg-deep)',
                    color: allValid && !submitting ? 'var(--c-bg-page)' : 'var(--c-text-muted)',
                  }}
                >
                  {submitting ? t.userInput.submitting : t.userInput.submit}
                  {!submitting && <ArrowRight size={13} />}
                </button>
              ) : (
                <button
                  type="button"
                  onClick={goNext}
                  disabled={!currentFieldValid || submitting || !!disabled}
                  className="flex h-7 items-center gap-1.5 rounded-lg px-3 border-none cursor-pointer transition-[background-color,color] duration-[60ms] disabled:opacity-30 text-[13px] font-medium"
                  style={{
                    background: currentFieldValid && !submitting ? 'var(--c-text-primary)' : 'var(--c-bg-deep)',
                    color: currentFieldValid && !submitting ? 'var(--c-bg-page)' : 'var(--c-text-muted)',
                  }}
                >
                  {t.userInput.next}
                  <ArrowRight size={13} />
                </button>
              )
            )}
          </div>
        </div>
      ) : !isSingleSelect ? (
        <div className="flex items-center justify-end pt-3 mt-3" style={{ borderTop: '0.5px solid var(--c-border-subtle)' }}>
          <div className="flex items-center gap-1.5">
            <button
              type="button"
              onClick={doSubmit}
              aria-label={t.userInput.submit}
              disabled={!allValid || submitting || !!disabled}
              className="flex h-7 items-center gap-1.5 rounded-lg px-3 border-none cursor-pointer transition-[background-color,color] duration-[60ms] disabled:opacity-30 text-[13px] font-medium"
              style={{
                background: allValid && !submitting ? 'var(--c-text-primary)' : 'var(--c-bg-deep)',
                color: allValid && !submitting ? 'var(--c-bg-page)' : 'var(--c-text-muted)',
              }}
            >
              {submitting ? t.userInput.submitting : t.userInput.submit}
              {!submitting && <ArrowRight size={13} />}
            </button>
            <button
              type="button"
              onClick={handleDismiss}
              disabled={submitting || !!disabled}
              className="rounded-lg px-3 py-1.5 text-[13px] border-none bg-transparent cursor-pointer transition-[background-color] duration-[60ms] disabled:opacity-40 hover:bg-[var(--c-bg-deep)]"
              style={{ color: 'var(--c-text-secondary)' }}
            >
              {t.userInput.dismiss}
            </button>
          </div>
        </div>
      ) : null}
    </div>
  )
}

// --- FieldRenderer ---

interface FieldRendererProps {
  fieldKey: string
  field: FieldSchema
  value: FieldValue | undefined
  required: boolean
  disabled: boolean
  onChange: (val: FieldValue) => void
  onQuickSubmit?: (val: FieldValue) => void
}

function FieldRenderer({ fieldKey, field, value, disabled, onChange, onQuickSubmit }: FieldRendererProps) {
  if (isEnumField(field)) {
    return <SelectField field={field} value={value as string | undefined} disabled={disabled} onChange={onChange} onQuickSubmit={onQuickSubmit} />
  }
  if (isOneOfField(field)) {
    return <OneOfSelectField field={field} value={value as string | undefined} disabled={disabled} onChange={onChange} onQuickSubmit={onQuickSubmit} />
  }
  if (isArrayEnumField(field)) {
    return <MultiSelectEnumField field={field} value={(value as string[]) ?? []} disabled={disabled} onChange={onChange} />
  }
  if (isArrayAnyOfField(field)) {
    return <MultiSelectAnyOfField field={field} value={(value as string[]) ?? []} disabled={disabled} onChange={onChange} />
  }
  if (isBooleanField(field)) {
    return <BooleanField field={field} value={value as boolean | undefined} disabled={disabled} onChange={onChange} />
  }
  if (isNumberField(field)) {
    return <NumberField fieldKey={fieldKey} field={field} value={value as number | undefined} disabled={disabled} onChange={onChange} />
  }
  if (isTextField(field)) {
    return <TextField fieldKey={fieldKey} field={field} value={(value as string) ?? ''} disabled={disabled} onChange={onChange} />
  }
  return null
}

// --- FieldLabel ---

function FieldLabel({ title, description }: { title?: string; description?: string }) {
  if (!title && !description) return null
  return (
    <div className="mb-2">
      {title && <span className="text-[14px] font-medium" style={{ color: 'var(--c-text-primary)' }}>{title}</span>}
      {description && (
        <span className={title ? 'ml-2 text-[12px]' : 'text-[12.5px]'} style={{ color: 'var(--c-text-muted)' }}>{description}</span>
      )}
    </div>
  )
}

// --- Select (enum) ---

interface SelectFieldProps {
  field: { title?: string; description?: string; enum: string[]; enumNames?: string[] }
  value: string | undefined
  disabled: boolean
  onChange: (val: string) => void
  onQuickSubmit?: (val: string) => void
}

function SelectField({ field, value, disabled, onChange, onQuickSubmit }: SelectFieldProps) {
  const [hoveredIdx, setHoveredIdx] = useState<number | null>(null)

  const handleClick = useCallback((v: string) => {
    onChange(v)
    if (onQuickSubmit) onQuickSubmit(v)
  }, [onChange, onQuickSubmit])

  return (
    <div>
      <FieldLabel title={field.title} description={field.description} />
      <div className="flex flex-col" style={{ maxHeight: '280px', overflowY: 'auto' }}>
        {field.enum.map((opt, idx) => {
          const label = field.enumNames?.[idx] ?? opt
          const selected = value === opt
          const isHovered = hoveredIdx === idx
          return (
            <div key={opt}>
              <OptionRow
                index={idx}
                label={label}
                selected={selected}
                disabled={disabled}
                isHovered={isHovered}
                onHover={() => setHoveredIdx(idx)}
                onHoverEnd={() => setHoveredIdx(null)}
                onClick={() => handleClick(opt)}
              />
              {idx < field.enum.length - 1 && (
                <div style={{ height: '0.5px', background: 'var(--c-border-subtle)', opacity: hoveredIdx !== idx && hoveredIdx !== idx + 1 ? 1 : 0, transition: 'opacity 60ms ease' }} />
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

// --- Select (oneOf) ---

interface OneOfSelectFieldProps {
  field: { title?: string; description?: string; oneOf: Array<{ const: string; title: string }> }
  value: string | undefined
  disabled: boolean
  onChange: (val: string) => void
  onQuickSubmit?: (val: string) => void
}

function OneOfSelectField({ field, value, disabled, onChange, onQuickSubmit }: OneOfSelectFieldProps) {
  const [hoveredIdx, setHoveredIdx] = useState<number | null>(null)

  const handleClick = useCallback((v: string) => {
    onChange(v)
    if (onQuickSubmit) onQuickSubmit(v)
  }, [onChange, onQuickSubmit])

  return (
    <div>
      <FieldLabel title={field.title} description={field.description} />
      <div className="flex flex-col" style={{ maxHeight: '280px', overflowY: 'auto' }}>
        {field.oneOf.map((opt, idx) => {
          const selected = value === opt.const
          const isHovered = hoveredIdx === idx
          return (
            <div key={opt.const}>
              <OptionRow
                index={idx}
                label={opt.title}
                selected={selected}
                disabled={disabled}
                isHovered={isHovered}
                onHover={() => setHoveredIdx(idx)}
                onHoverEnd={() => setHoveredIdx(null)}
                onClick={() => handleClick(opt.const)}
              />
              {idx < field.oneOf.length - 1 && (
                <div style={{ height: '0.5px', background: 'var(--c-border-subtle)', opacity: hoveredIdx !== idx && hoveredIdx !== idx + 1 ? 1 : 0, transition: 'opacity 60ms ease' }} />
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

// --- Multiselect (enum) ---

interface MultiSelectEnumFieldProps {
  field: { title?: string; description?: string; items: { enum: string[] } }
  value: string[]
  disabled: boolean
  onChange: (val: string[]) => void
}

function MultiSelectEnumField({ field, value, disabled, onChange }: MultiSelectEnumFieldProps) {
  const toggle = useCallback((v: string) => {
    onChange(value.includes(v) ? value.filter(x => x !== v) : [...value, v])
  }, [value, onChange])

  return (
    <div>
      <FieldLabel title={field.title} description={field.description} />
      <div className="flex flex-col gap-0.5" style={{ maxHeight: '280px', overflowY: 'auto' }}>
        {field.items.enum.map(opt => (
          <CheckboxRow key={opt} label={opt} checked={value.includes(opt)} disabled={disabled} onClick={() => toggle(opt)} />
        ))}
      </div>
    </div>
  )
}

// --- Multiselect (anyOf) ---

interface MultiSelectAnyOfFieldProps {
  field: { title?: string; description?: string; items: { anyOf: Array<{ const: string; title: string }> } }
  value: string[]
  disabled: boolean
  onChange: (val: string[]) => void
}

function MultiSelectAnyOfField({ field, value, disabled, onChange }: MultiSelectAnyOfFieldProps) {
  const toggle = useCallback((v: string) => {
    onChange(value.includes(v) ? value.filter(x => x !== v) : [...value, v])
  }, [value, onChange])

  return (
    <div>
      <FieldLabel title={field.title} description={field.description} />
      <div className="flex flex-col gap-0.5" style={{ maxHeight: '280px', overflowY: 'auto' }}>
        {field.items.anyOf.map(opt => (
          <CheckboxRow key={opt.const} label={opt.title} checked={value.includes(opt.const)} disabled={disabled} onClick={() => toggle(opt.const)} />
        ))}
      </div>
    </div>
  )
}

// --- Boolean ---

interface BooleanFieldProps {
  field: { title?: string; description?: string }
  value: boolean | undefined
  disabled: boolean
  onChange: (val: boolean) => void
}

function BooleanField({ field, value, disabled, onChange }: BooleanFieldProps) {
  const checked = value ?? false
  return (
    <div>
      <div
        role="button"
        tabIndex={0}
        onClick={() => !disabled && onChange(!checked)}
        onKeyDown={e => { if (!disabled && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); onChange(!checked) } }}
        className="flex items-center gap-3 cursor-pointer rounded-lg px-2 py-2.5 transition-[background-color] duration-[60ms] hover:bg-[var(--c-bg-deep)]"
        style={{ opacity: disabled ? 0.5 : 1 }}
      >
        <div
          className="flex-shrink-0 flex items-center justify-center rounded-md"
          style={{
            width: '22px', height: '22px',
            background: checked ? 'var(--c-text-primary)' : 'var(--c-bg-deep)',
            color: checked ? 'var(--c-bg-page)' : 'transparent',
            transition: 'background 60ms ease, color 60ms ease',
          }}
        >
          <Check size={13} />
        </div>
        <span className="text-[14.5px] font-light" style={{ color: 'var(--c-text-primary)' }}>
          {field.title}
        </span>
        {field.description && (
          <span className="text-[12px]" style={{ color: 'var(--c-text-muted)' }}>{field.description}</span>
        )}
      </div>
    </div>
  )
}

// --- Text ---

interface TextFieldProps {
  fieldKey: string
  field: { title?: string; description?: string; maxLength?: number }
  value: string
  disabled: boolean
  onChange: (val: string) => void
}

function TextField({ fieldKey, field, value, disabled, onChange }: TextFieldProps) {
  return (
    <div>
      <FieldLabel title={field.title} description={field.description} />
      <input
        id={`field-${fieldKey}`}
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

// --- Number ---

interface NumberFieldProps {
  fieldKey: string
  field: { title?: string; description?: string; minimum?: number; maximum?: number; type: 'number' | 'integer' }
  value: number | undefined
  disabled: boolean
  onChange: (val: number) => void
}

function NumberField({ fieldKey, field, value, disabled, onChange }: NumberFieldProps) {
  return (
    <div>
      <FieldLabel title={field.title} description={field.description} />
      <input
        id={`field-${fieldKey}`}
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

// --- OptionRow (select 单选行) ---

interface OptionRowProps {
  index: number
  label: string
  selected: boolean
  disabled: boolean
  isHovered: boolean
  onHover: () => void
  onHoverEnd: () => void
  onClick: () => void
}

function OptionRow({ index, label, selected, disabled, isHovered, onHover, onHoverEnd, onClick }: OptionRowProps) {
  const badgeBg = selected ? 'var(--c-text-primary)' : isHovered && !disabled ? 'var(--c-border-subtle)' : 'var(--c-bg-deep)'
  const badgeColor = selected ? 'var(--c-bg-page)' : isHovered && !disabled ? 'var(--c-text-secondary)' : 'var(--c-text-muted)'

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={() => !disabled && onClick()}
      onKeyDown={e => { if (!disabled && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); onClick() } }}
      onMouseEnter={onHover}
      onMouseLeave={onHoverEnd}
      className="flex items-center gap-3 cursor-pointer"
      style={{
        background: isHovered && !disabled ? 'var(--c-bg-deep)' : 'transparent',
        borderRadius: '10px',
        opacity: disabled ? 0.5 : 1,
        transition: 'background 60ms ease',
        padding: '13px 8px',
      }}
    >
      <div
        className="flex-shrink-0 flex items-center justify-center rounded-md text-[12px] font-medium"
        style={{ width: '26px', height: '26px', background: badgeBg, color: badgeColor, transition: 'background 60ms ease, color 60ms ease' }}
      >
        {index + 1}
      </div>
      <span className="flex-1 text-[14.5px] font-light" style={{ color: 'var(--c-text-primary)' }}>
        {label}
      </span>
      <ArrowRight
        size={13}
        style={{ flexShrink: 0, color: 'var(--c-text-tertiary)', opacity: isHovered && !disabled ? 1 : 0, transition: 'opacity 80ms ease' }}
      />
    </div>
  )
}

// --- CheckboxRow (multiselect 多选行) ---

interface CheckboxRowProps {
  label: string
  checked: boolean
  disabled: boolean
  onClick: () => void
}

function CheckboxRow({ label, checked, disabled, onClick }: CheckboxRowProps) {
  return (
    <div
      role="checkbox"
      aria-checked={checked}
      tabIndex={0}
      onClick={() => !disabled && onClick()}
      onKeyDown={e => { if (!disabled && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); onClick() } }}
      className="flex items-center gap-3 cursor-pointer rounded-lg px-2 py-2.5 transition-[background-color] duration-[60ms] hover:bg-[var(--c-bg-deep)]"
      style={{ opacity: disabled ? 0.5 : 1 }}
    >
      <div
        className="flex-shrink-0 flex items-center justify-center rounded-md"
        style={{
          width: '22px', height: '22px',
          background: checked ? 'var(--c-text-primary)' : 'var(--c-bg-deep)',
          color: checked ? 'var(--c-bg-page)' : 'transparent',
          border: checked ? 'none' : '0.5px solid var(--c-border-subtle)',
          transition: 'background 60ms ease, color 60ms ease',
        }}
      >
        <Check size={12} />
      </div>
      <span className="flex-1 text-[14.5px] font-light" style={{ color: 'var(--c-text-primary)' }}>
        {label}
      </span>
    </div>
  )
}
