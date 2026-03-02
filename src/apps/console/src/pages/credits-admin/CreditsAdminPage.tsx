import { useState, useCallback } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Plus, Minus, RotateCcw } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { bulkAdjustCredits, resetAllCredits } from '../../api/admin-users'

type PendingOp =
  | { type: 'add'; amount: number; note: string }
  | { type: 'deduct'; amount: number; note: string }
  | { type: 'reset'; note: string }
  | null

export function CreditsAdminPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { t } = useLocale()
  const tc = t.pages.creditsAdmin
  const { addToast } = useToast()

  const [pending, setPending] = useState<PendingOp>(null)
  const [submitting, setSubmitting] = useState(false)

  const handleConfirm = useCallback(async () => {
    if (!pending) return
    setSubmitting(true)
    try {
      if (pending.type === 'add') {
        await bulkAdjustCredits(pending.amount, pending.note, accessToken)
        addToast(tc.toastAddOk, 'success')
      } else if (pending.type === 'deduct') {
        await bulkAdjustCredits(-pending.amount, pending.note, accessToken)
        addToast(tc.toastDeductOk, 'success')
      } else {
        await resetAllCredits(pending.note, accessToken)
        addToast(tc.toastResetOk, 'success')
      }
      setPending(null)
    } catch (err) {
      addToast(isApiError(err) ? err.message : tc.toastFailed, 'error')
    } finally {
      setSubmitting(false)
    }
  }, [pending, accessToken, addToast, tc])

  const confirmMessage = (() => {
    if (!pending) return ''
    if (pending.type === 'add') return tc.confirmAdd(pending.amount)
    if (pending.type === 'deduct') return tc.confirmDeduct(pending.amount)
    return tc.confirmReset
  })()

  return (
    <div className="flex flex-col gap-6 p-6">
      <PageHeader title={tc.title} />

      <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
        <AmountCard
          icon={<Plus size={18} />}
          title={tc.addCard}
          desc={tc.addDesc}
          fieldAmountLabel={tc.fieldAmount}
          fieldNoteLabel={tc.fieldNote}
          fieldNotePlaceholder={tc.fieldNotePlaceholder}
          onSubmit={(amount, note) => setPending({ type: 'add', amount, note })}
          submitLabel={tc.submit}
          variant="success"
        />

        <AmountCard
          icon={<Minus size={18} />}
          title={tc.deductCard}
          desc={tc.deductDesc}
          fieldAmountLabel={tc.fieldAmount}
          fieldNoteLabel={tc.fieldNote}
          fieldNotePlaceholder={tc.fieldNotePlaceholder}
          onSubmit={(amount, note) => setPending({ type: 'deduct', amount, note })}
          submitLabel={tc.submit}
          variant="warning"
        />

        <ResetCard
          icon={<RotateCcw size={18} />}
          title={tc.resetCard}
          desc={tc.resetDesc}
          fieldNoteLabel={tc.fieldNote}
          fieldNotePlaceholder={tc.fieldNotePlaceholder}
          onSubmit={(note) => setPending({ type: 'reset', note })}
          submitLabel={tc.submit}
        />
      </div>

      <ConfirmDialog
        open={pending !== null}
        onClose={() => { if (!submitting) setPending(null) }}
        onConfirm={() => void handleConfirm()}
        title={tc.title}
        message={confirmMessage}
        confirmLabel={submitting ? tc.submitting : tc.submit}
        loading={submitting}
      />
    </div>
  )
}

type AmountCardProps = {
  icon: React.ReactNode
  title: string
  desc: string
  fieldAmountLabel: string
  fieldNoteLabel: string
  fieldNotePlaceholder: string
  onSubmit: (amount: number, note: string) => void
  submitLabel: string
  variant: 'success' | 'warning'
}

function AmountCard({
  icon,
  title,
  desc,
  fieldAmountLabel,
  fieldNoteLabel,
  fieldNotePlaceholder,
  onSubmit,
  submitLabel,
  variant,
}: AmountCardProps) {
  const [amount, setAmount] = useState('')
  const [note, setNote] = useState('')

  const amountNum = parseInt(amount, 10)
  const valid = !isNaN(amountNum) && amountNum > 0

  const btnCls =
    variant === 'success'
      ? 'bg-green-600 hover:bg-green-700'
      : 'bg-yellow-500 hover:bg-yellow-600'

  return (
    <div className="flex flex-col gap-4 rounded-xl border border-[var(--c-border)] bg-[var(--c-bg-card)] p-5">
      <div className="flex items-center gap-2 text-[var(--c-text-primary)]">
        {icon}
        <span className="font-semibold">{title}</span>
      </div>
      <p className="text-xs text-[var(--c-text-muted)]">{desc}</p>
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <label className="text-xs font-medium text-[var(--c-text-secondary)]">{fieldAmountLabel}</label>
          <input
            type="number"
            min={1}
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-accent)] focus:ring-1 focus:ring-[var(--c-accent)]"
          />
        </div>
        <div className="flex flex-col gap-1">
          <label className="text-xs font-medium text-[var(--c-text-secondary)]">{fieldNoteLabel}</label>
          <input
            type="text"
            placeholder={fieldNotePlaceholder}
            value={note}
            onChange={(e) => setNote(e.target.value)}
            className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-accent)] focus:ring-1 focus:ring-[var(--c-accent)]"
          />
        </div>
      </div>
      <button
        onClick={() => { if (valid) onSubmit(amountNum, note) }}
        disabled={!valid}
        className={`mt-auto rounded-lg px-4 py-1.5 text-sm font-medium text-white transition-colors disabled:opacity-40 ${btnCls}`}
      >
        {submitLabel}
      </button>
    </div>
  )
}

type ResetCardProps = {
  icon: React.ReactNode
  title: string
  desc: string
  fieldNoteLabel: string
  fieldNotePlaceholder: string
  onSubmit: (note: string) => void
  submitLabel: string
}

function ResetCard({
  icon,
  title,
  desc,
  fieldNoteLabel,
  fieldNotePlaceholder,
  onSubmit,
  submitLabel,
}: ResetCardProps) {
  const [note, setNote] = useState('')

  return (
    <div className="flex flex-col gap-4 rounded-xl border border-red-500/30 bg-[var(--c-bg-card)] p-5">
      <div className="flex items-center gap-2 text-red-500">
        {icon}
        <span className="font-semibold">{title}</span>
      </div>
      <p className="text-xs text-[var(--c-text-muted)]">{desc}</p>
      <div className="flex flex-col gap-1">
        <label className="text-xs font-medium text-[var(--c-text-secondary)]">{fieldNoteLabel}</label>
        <input
          type="text"
          placeholder={fieldNotePlaceholder}
          value={note}
          onChange={(e) => setNote(e.target.value)}
          className="rounded-lg border border-[var(--c-border)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-accent)] focus:ring-1 focus:ring-[var(--c-accent)]"
        />
      </div>
      <button
        onClick={() => onSubmit(note)}
        className="mt-auto rounded-lg bg-red-600 px-4 py-1.5 text-sm font-medium text-white transition-colors hover:bg-red-700"
      >
        {submitLabel}
      </button>
    </div>
  )
}
