import { useState, useEffect, useCallback } from 'react'
import { AnimatePresence, motion } from 'framer-motion'
import { X, Copy, Check, Link, Lock, Globe, Trash2 } from 'lucide-react'
import { createThreadShare, getThreadShare, deleteThreadShare, isApiError, type ShareResponse } from '../api'
import { useLocale } from '../contexts/LocaleContext'

function SpinnerIcon() {
  return (
    <svg className="animate-spin" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" aria-hidden="true">
      <path d="M21 12a9 9 0 1 1-6.219-8.56" />
    </svg>
  )
}

type Props = {
  accessToken: string
  threadId: string
  open: boolean
  onClose: () => void
}

export function ShareModal({ accessToken, threadId, open, onClose }: Props) {
  const { t } = useLocale()
  const [loading, setLoading] = useState(true)
  const [existing, setExisting] = useState<ShareResponse | null>(null)
  const [accessType, setAccessType] = useState<'public' | 'password'>('public')
  const [password, setPassword] = useState('')
  const [creating, setCreating] = useState(false)
  const [revoking, setRevoking] = useState(false)
  const [copied, setCopied] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setLoading(true)
    void (async () => {
      try {
        const share = await getThreadShare(accessToken, threadId)
        setExisting(share)
      } catch (err) {
        if (isApiError(err) && err.status === 404) {
          setExisting(null)
        }
      } finally {
        setLoading(false)
      }
    })()
  }, [accessToken, threadId, open])

  const handleCreate = useCallback(async () => {
    if (accessType === 'password' && !password.trim()) return
    setCreating(true)
    setError(null)
    try {
      const share = await createThreadShare(
        accessToken,
        threadId,
        accessType,
        accessType === 'password' ? password : undefined,
      )
      setExisting(share)
      setPassword('')
    } catch (err) {
      if (isApiError(err)) {
        setError(err.message)
      }
    } finally {
      setCreating(false)
    }
  }, [accessToken, threadId, accessType, password])

  const handleRevoke = useCallback(async () => {
    setRevoking(true)
    setError(null)
    try {
      await deleteThreadShare(accessToken, threadId)
      setExisting(null)
    } catch (err) {
      if (isApiError(err)) {
        setError(err.message)
      }
    } finally {
      setRevoking(false)
    }
  }, [accessToken, threadId])

  const handleCopy = useCallback(() => {
    if (!existing) return
    const url = `${window.location.origin}/s/${existing.token}`
    void navigator.clipboard.writeText(url)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }, [existing])

  // Escape 关闭
  useEffect(() => {
    if (!open) return
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  }, [onClose, open])

  if (!open) return null

  return (
    <div
      className="overlay-fade-in fixed inset-0 z-50 flex items-center justify-center"
      style={{ background: 'rgba(0,0,0,0.5)' }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="modal-enter relative w-full max-w-md rounded-2xl p-6"
        style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)' }}
      >
        {/* Header */}
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-base font-semibold" style={{ color: 'var(--c-text-primary)' }}>
            {t.shareTitle}
          </h2>
          <button
            onClick={onClose}
            className="flex h-7 w-7 items-center justify-center rounded-lg transition-colors hover:bg-[var(--c-bg-deep)]"
            style={{ color: 'var(--c-text-muted)' }}
          >
            <X size={16} />
          </button>
        </div>

        <div style={{ minHeight: '144px' }}>
        {loading ? (
          <div className="py-8 text-center text-sm" style={{ color: 'var(--c-text-muted)' }}>
            {t.loading}
          </div>
        ) : existing ? (
          /* 已有分享链接 */
          <div className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <span className="text-xs font-medium" style={{ color: 'var(--c-text-secondary)' }}>
                {t.shareCurrentLink}
              </span>
              <div
                className="flex items-center gap-2 rounded-lg px-3 py-2"
                style={{ background: 'var(--c-bg-sub)', border: '0.5px solid var(--c-border-subtle)' }}
              >
                <Link size={14} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
                <span
                  className="flex-1 truncate text-sm"
                  style={{ color: 'var(--c-text-primary)' }}
                >
                  {window.location.origin}/s/{existing.token}
                </span>
                <button
                  onClick={handleCopy}
                  className="flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors hover:bg-[var(--c-bg-deep)]"
                  style={{ color: copied ? 'var(--c-success, #22c55e)' : 'var(--c-text-primary)' }}
                >
                  {copied ? <Check size={12} /> : <Copy size={12} />}
                  {copied ? t.shareCopied : t.shareCopyLink}
                </button>
              </div>
              <div className="flex items-center gap-1 text-xs" style={{ color: 'var(--c-text-muted)' }}>
                {existing.access_type === 'password' ? <Lock size={11} /> : <Globe size={11} />}
                {existing.access_type === 'password' ? t.sharePassword : t.sharePublic}
              </div>
            </div>

            <button
              onClick={handleRevoke}
              disabled={revoking}
              className="flex items-center justify-center gap-1.5 rounded-lg px-3 py-2 text-sm font-medium transition-colors hover:bg-red-500/10 disabled:opacity-50"
              style={{ color: 'var(--c-destructive, #ef4444)' }}
            >
              <Trash2 size={14} />
              {revoking ? t.shareRevoking : t.shareRevoke}
            </button>
          </div>
        ) : (
          /* 创建分享 */
          <div className="flex flex-col">
            {/* 访问类型选择 */}
            <div className="mb-4 flex flex-col gap-2">
              <label className="flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2.5 transition-colors hover:bg-[var(--c-bg-sub)]">
                <input
                  type="radio"
                  name="access_type"
                  checked={accessType === 'public'}
                  onChange={() => setAccessType('public')}
                  className="accent-[var(--c-btn-bg)]"
                />
                <Globe size={16} style={{ color: 'var(--c-text-icon)' }} />
                <span className="text-sm" style={{ color: 'var(--c-text-primary)' }}>
                  {t.sharePublic}
                </span>
              </label>
              <label className="flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2.5 transition-colors hover:bg-[var(--c-bg-sub)]">
                <input
                  type="radio"
                  name="access_type"
                  checked={accessType === 'password'}
                  onChange={() => setAccessType('password')}
                  className="accent-[var(--c-btn-bg)]"
                />
                <Lock size={16} style={{ color: 'var(--c-text-icon)' }} />
                <span className="text-sm" style={{ color: 'var(--c-text-primary)' }}>
                  {t.sharePassword}
                </span>
              </label>
            </div>

            <AnimatePresence>
              {accessType === 'password' && (
                <motion.div
                  initial={{ opacity: 0, height: 0, marginBottom: 0 }}
                  animate={{ opacity: 1, height: 'auto', marginBottom: 16 }}
                  exit={{ opacity: 0, height: 0, marginBottom: 0 }}
                  transition={{ duration: 0.18, ease: 'easeOut' }}
                  style={{ overflow: 'hidden' }}
                >
                  <input
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder={t.sharePasswordPlaceholder}
                    autoComplete="off"
                    autoFocus
                    className="w-full rounded-lg px-3 py-2 text-sm outline-none"
                    style={{
                      background: 'var(--c-bg-sub)',
                      border: '0.5px solid var(--c-border-subtle)',
                      color: 'var(--c-text-primary)',
                    }}
                    onKeyDown={(e) => { if (e.key === 'Enter') void handleCreate() }}
                  />
                </motion.div>
              )}
            </AnimatePresence>

            {error && (
              <p className="mb-4 text-xs" style={{ color: 'var(--c-destructive, #ef4444)' }}>{error}</p>
            )}

            <button
              onClick={() => void handleCreate()}
              disabled={creating || (accessType === 'password' && !password.trim())}
              className="flex w-full items-center justify-center gap-1.5 rounded-lg px-3 py-2.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
              style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
            >
              {creating && <SpinnerIcon />}
              {creating ? t.shareCreating : t.shareCreate}
            </button>
          </div>
        )}
        </div>
      </div>
    </div>
  )
}
