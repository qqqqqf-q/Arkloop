import { useState, useEffect, useCallback } from 'react'
import {
  type CreditTransaction,
  getMyCredits,
  redeemCode,
} from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import { formatDateTime, getActiveTimeZone } from '@arkloop/shared'

function getCurrentYearMonth(timeZone: string): { year: number; month: number } {
  const parts = formatDateTime(new Date(), { timeZone, includeZone: false }).slice(0, 7).split('-')
  return { year: Number(parts[0]), month: Number(parts[1]) }
}

function getMonthLabel(month: number, locale: string): string {
  return new Intl.DateTimeFormat(locale === 'zh' ? 'zh-CN' : 'en-US', {
    month: 'long',
    timeZone: 'UTC',
  }).format(new Date(Date.UTC(2000, month - 1, 1)))
}

function formatTransactionDate(value: string): string {
  return formatDateTime(value, { includeZone: false }).slice(0, 10)
}

export function CreditsContent({ accessToken, onCreditsChanged }: { accessToken: string; onCreditsChanged?: (balance: number) => void }) {
  const { t, locale } = useLocale()
  const [balance, setBalance] = useState<number | null>(null)
  const [balanceLoading, setBalanceLoading] = useState(true)
  const [redeemInput, setRedeemInput] = useState('')
  const [redeemLoading, setRedeemLoading] = useState(false)
  const [redeemMsg, setRedeemMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [transactions, setTransactions] = useState<CreditTransaction[] | null>(null)
  const [monthlyTransactions, setMonthlyTransactions] = useState<CreditTransaction[] | null>(null)
  const [txLoading, setTxLoading] = useState(false)
  const [txError, setTxError] = useState('')
  const timeZone = getActiveTimeZone()
  const currentYearMonth = getCurrentYearMonth(timeZone)
  const [filterYear, setFilterYear] = useState(currentYearMonth.year)
  const [filterMonth, setFilterMonth] = useState(currentYearMonth.month)

  useEffect(() => {
    void (async () => {
      try {
        const data = await getMyCredits(accessToken)
        setBalance(data.balance)
        setTransactions(data.transactions)
        onCreditsChanged?.(data.balance)
      } catch {
        setBalance(null)
      } finally {
        setBalanceLoading(false)
      }
    })()
  }, [accessToken, onCreditsChanged])

  const handleRedeem = useCallback(async () => {
    const code = redeemInput.trim()
    if (!code) return
    setRedeemLoading(true)
    setRedeemMsg(null)
    try {
      const res = await redeemCode(accessToken, code)
      setRedeemMsg({ ok: true, text: t.creditsRedeemSuccess(res.value) })
      setRedeemInput('')
      const updated = await getMyCredits(accessToken)
      setBalance(updated.balance)
      setTransactions(updated.transactions)
      onCreditsChanged?.(updated.balance)
    } catch {
      setRedeemMsg({ ok: false, text: t.creditsRedeemError(code) })
    } finally {
      setRedeemLoading(false)
    }
  }, [accessToken, onCreditsChanged, redeemInput, t])

  const handleQueryUsage = useCallback(async () => {
    setTxLoading(true)
    setTxError('')
    try {
      const from = `${filterYear}-${String(filterMonth).padStart(2, '0')}-01`
      const nextMonth = filterMonth === 12 ? 1 : filterMonth + 1
      const nextYear = filterMonth === 12 ? filterYear + 1 : filterYear
      const to = `${nextYear}-${String(nextMonth).padStart(2, '0')}-01`
      const data = await getMyCredits(accessToken, from, to)
      setMonthlyTransactions(data.transactions)
    } catch {
      setTxError(t.requestFailed)
    } finally {
      setTxLoading(false)
    }
  }, [accessToken, filterYear, filterMonth, t])

  return (
    <div className="flex flex-col gap-6">
      {/* balance */}
      <div className="flex flex-col gap-2">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.creditsBalance}</span>
        <div
          className="flex h-12 w-[240px] items-center rounded-lg px-4"
          style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
        >
          {balanceLoading ? (
            <span className="text-sm text-[var(--c-text-tertiary)]">...</span>
          ) : (
            <span className="text-xl font-semibold tabular-nums text-[var(--c-text-heading)]">
              {balance ?? '-'}
              <span className="ml-1.5 text-xs font-normal text-[var(--c-text-tertiary)]">
                {t.creditsBalanceUnit}
              </span>
            </span>
          )}
        </div>
      </div>

      {/* redeem */}
      <div className="flex flex-col gap-2">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.creditsRedeem}</span>
        <div className="flex gap-2">
          <input
            type="text"
            value={redeemInput}
            onChange={(e) => setRedeemInput(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') void handleRedeem() }}
            placeholder={t.creditsRedeemPlaceholder}
            className="h-9 w-[240px] rounded-lg px-3 text-sm text-[var(--c-text-heading)] outline-none placeholder:text-[var(--c-text-tertiary)]"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            disabled={redeemLoading}
          />
          <button
            onClick={() => void handleRedeem()}
            disabled={redeemLoading || !redeemInput.trim()}
            className="flex h-9 items-center rounded-lg px-3 text-sm font-medium text-[var(--c-text-heading)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-50"
            style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
          >
            {t.creditsRedeemBtn}
          </button>
        </div>
        {redeemMsg && (
          <p
            className="text-xs"
            style={{ color: redeemMsg.ok ? 'var(--c-status-success-text)' : 'var(--c-status-error-text)' }}
          >
            {redeemMsg.text}
          </p>
        )}
      </div>

      {/* usage */}
      <div className="flex flex-col gap-4">
        <span className="text-sm font-medium text-[var(--c-text-heading)]">{t.creditsUsage}</span>

        {/* recent */}
        <div className="flex flex-col gap-2">
          <span className="text-xs font-medium text-[var(--c-text-tertiary)]">{t.creditsHistoryRecent}</span>
          <CreditTransactionTable transactions={transactions} loading={balanceLoading} t={t} />
        </div>

        {/* monthly query */}
        <div className="flex flex-col gap-2">
          <span className="text-xs font-medium text-[var(--c-text-tertiary)]">{t.creditsHistoryMonthly}</span>
          <div className="flex items-center gap-2">
            <select
              value={filterYear}
              onChange={(e) => setFilterYear(Number(e.target.value))}
              className="h-8 rounded-lg px-2 text-sm text-[var(--c-text-heading)] outline-none"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            >
              {Array.from({ length: 3 }, (_, i) => currentYearMonth.year - i).map(y => (
                <option key={y} value={y}>{y}</option>
              ))}
            </select>
            <select
              value={filterMonth}
              onChange={(e) => setFilterMonth(Number(e.target.value))}
              className="h-8 rounded-lg px-2 text-sm text-[var(--c-text-heading)] outline-none"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            >
              {Array.from({ length: 12 }, (_, i) => i + 1).map(m => (
                <option key={m} value={m}>{getMonthLabel(m, locale)}</option>
              ))}
            </select>
            <button
              onClick={() => void handleQueryUsage()}
              disabled={txLoading}
              className="flex h-8 items-center rounded-lg px-3 text-sm font-medium text-[var(--c-text-heading)] transition-colors hover:bg-[var(--c-bg-deep)] disabled:opacity-50"
              style={{ border: '0.5px solid var(--c-border-subtle)', background: 'var(--c-bg-page)' }}
            >
              {t.creditsUsageQuery}
            </button>
          </div>
          {txError && (
            <p className="text-xs text-[var(--c-status-error-text)]">{txError}</p>
          )}
          {monthlyTransactions !== null && (
            <CreditTransactionTable transactions={monthlyTransactions} loading={txLoading} t={t} />
          )}
        </div>
      </div>
    </div>
  )
}

type TableLocale = {
  creditsHistoryDetails: string
  creditsHistoryDate: string
  creditsHistoryCreditChange: string
  creditsHistoryEmpty: string
  creditsTxTypeLabel: (type: string) => string
}

function CreditTransactionTable({
  transactions,
  loading,
  t,
}: {
  transactions: CreditTransaction[] | null
  loading: boolean
  t: TableLocale
}) {
  if (loading) {
    return <p className="text-xs text-[var(--c-text-tertiary)]">...</p>
  }
  if (!transactions || transactions.length === 0) {
    return <p className="text-xs text-[var(--c-text-tertiary)]">{t.creditsHistoryEmpty}</p>
  }

  return (
    <div
      className="overflow-y-auto rounded-xl"
      style={{ border: '0.5px solid var(--c-border-subtle)', maxHeight: '320px' }}
    >
      <table className="w-full text-sm">
        <thead className="sticky top-0" style={{ background: 'var(--c-bg-page)', zIndex: 1 }}>
          <tr style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}>
            <th className="px-4 py-2 text-left text-xs font-medium text-[var(--c-text-tertiary)]">
              {t.creditsHistoryDetails}
            </th>
            <th className="px-4 py-2 text-left text-xs font-medium text-[var(--c-text-tertiary)] whitespace-nowrap">
              {t.creditsHistoryDate}
            </th>
            <th className="px-4 py-2 text-right text-xs font-medium text-[var(--c-text-tertiary)] whitespace-nowrap">
              {t.creditsHistoryCreditChange}
            </th>
          </tr>
        </thead>
        <tbody>
          {transactions.map((tx) => {
            const detail = tx.thread_title ?? tx.note ?? t.creditsTxTypeLabel(tx.type)
            const dateStr = formatTransactionDate(tx.created_at)
            const isPositive = tx.amount >= 0
            return (
              <tr
                key={tx.id}
                style={{ borderBottom: '0.5px solid var(--c-border-subtle)' }}
              >
                <td
                  className="max-w-[240px] truncate px-4 py-2 text-[var(--c-text-heading)]"
                  title={detail}
                >
                  {detail}
                </td>
                <td className="whitespace-nowrap px-4 py-2 text-xs text-[var(--c-text-tertiary)]">
                  {dateStr}
                </td>
                <td
                  className="whitespace-nowrap px-4 py-2 text-right font-medium tabular-nums"
                  style={{ color: isPositive ? 'var(--c-status-success-text)' : 'var(--c-status-error-text)' }}
                >
                  {isPositive ? '+' : ''}{tx.amount}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
