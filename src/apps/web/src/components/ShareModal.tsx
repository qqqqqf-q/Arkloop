import { useState, useEffect, useCallback } from 'react'
import { AnimatePresence, motion } from 'framer-motion'
import { X, Link, Lock, Globe, Trash2, Plus, Eye, EyeOff } from 'lucide-react'
import { createThreadShare, listThreadShares, deleteThreadShare, isApiError, type ShareResponse } from '../api'
import { useLocale } from '../contexts/LocaleContext'
import { CopyIconButton } from './CopyIconButton'

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
  const [shares, setShares] = useState<ShareResponse[]>([])
  const [showCreateForm, setShowCreateForm] = useState(false)

  const [accessType, setAccessType] = useState<'public' | 'password'>('public')
  const [password, setPassword] = useState('')
  const [liveUpdate, setLiveUpdate] = useState(false)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const [deletingId, setDeletingId] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setLoading(true)
    setShowCreateForm(false)
    void (async () => {
      try {
        const list = await listThreadShares(accessToken, threadId)
        setShares(list)
        if (list.length === 0) setShowCreateForm(true)
      } catch {
        setShares([])
        setShowCreateForm(true)
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
        liveUpdate,
      )
      setShares(prev => [share, ...prev])
      setPassword('')
      setAccessType('public')
      setLiveUpdate(false)
      setShowCreateForm(false)
    } catch (err) {
      if (isApiError(err)) {
        setError(err.message)
      }
    } finally {
      setCreating(false)
    }
  }, [accessToken, threadId, accessType, password, liveUpdate])

  const handleDelete = useCallback(async (shareId: string) => {
    setDeletingId(shareId)
    try {
      await deleteThreadShare(accessToken, threadId, shareId)
      setShares(prev => prev.filter(s => s.id !== shareId))
    } catch (err) {
      if (isApiError(err)) {
        setError(err.message)
      }
    } finally {
      setDeletingId(null)
    }
  }, [accessToken, threadId])

  const handleCopy = useCallback((share: ShareResponse) => {
    const url = `${window.location.origin}/s/${share.token}`
    void navigator.clipboard.writeText(url)
  }, [])

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
      style={{ background: 'rgba(0,0,0,0.12)', backdropFilter: 'blur(2px)', WebkitBackdropFilter: 'blur(2px)' }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div
        className="modal-enter relative flex w-full max-w-md flex-col overflow-hidden rounded-2xl p-6"
        style={{ background: 'var(--c-bg-page)', border: '0.5px solid var(--c-border-subtle)', maxHeight: '80vh' }}
      >
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

        {loading ? (
          <div className="py-8 text-center text-sm" style={{ color: 'var(--c-text-muted)' }}>
            {t.loading}
          </div>
        ) : (
          <>
            {shares.length > 0 && (
              <div className="min-h-0 flex-1 overflow-y-auto">
                <div className="flex flex-col gap-2">
                  {shares.map(share => (
                    <ShareItem
                      key={share.id}
                      share={share}
                      deleting={deletingId === share.id}
                      onCopy={() => handleCopy(share)}
                      onDelete={() => void handleDelete(share.id)}
                      t={t}
                    />
                  ))}
                </div>
              </div>
            )}

            <div className="shrink-0">
              <AnimatePresence>
                {showCreateForm && (
                  <motion.div
                    initial={{ opacity: 0, height: 0 }}
                    animate={{ opacity: 1, height: 'auto' }}
                    exit={{ opacity: 0, height: 0 }}
                    transition={{ duration: 0.18, ease: 'easeOut' }}
                    style={{ overflow: 'hidden' }}
                  >
                    <div
                      className="mt-3 flex flex-col rounded-xl p-4"
                      style={{ background: 'var(--c-bg-sub)', border: '0.5px solid var(--c-border-subtle)' }}
                    >
                      <div className="mb-3 flex flex-col gap-1.5">
                        <label className="flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2 transition-colors hover:bg-[var(--c-bg-deep)]">
                          <input
                            type="radio"
                            name="share_access_type"
                            checked={accessType === 'public'}
                            onChange={() => setAccessType('public')}
                            className="accent-[var(--c-btn-bg)]"
                          />
                          <Globe size={15} style={{ color: 'var(--c-text-icon)' }} />
                          <span className="text-sm" style={{ color: 'var(--c-text-primary)' }}>
                            {t.sharePublic}
                          </span>
                        </label>
                        <label className="flex cursor-pointer items-center gap-3 rounded-lg px-3 py-2 transition-colors hover:bg-[var(--c-bg-deep)]">
                          <input
                            type="radio"
                            name="share_access_type"
                            checked={accessType === 'password'}
                            onChange={() => setAccessType('password')}
                            className="accent-[var(--c-btn-bg)]"
                          />
                          <Lock size={15} style={{ color: 'var(--c-text-icon)' }} />
                          <span className="text-sm" style={{ color: 'var(--c-text-primary)' }}>
                            {t.sharePassword}
                          </span>
                        </label>
                      </div>

                      <AnimatePresence>
                        {accessType === 'password' && (
                          <motion.div
                            initial={{ opacity: 0, height: 0, marginBottom: 0 }}
                            animate={{ opacity: 1, height: 'auto', marginBottom: 12 }}
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
                                background: 'var(--c-bg-deep)',
                                border: '0.5px solid var(--c-border-subtle)',
                                color: 'var(--c-text-primary)',
                              }}
                              onKeyDown={(e) => { if (e.key === 'Enter') void handleCreate() }}
                            />
                          </motion.div>
                        )}
                      </AnimatePresence>

                      <label className="mb-3 flex cursor-pointer items-center justify-between rounded-lg px-3 py-2 transition-colors hover:bg-[var(--c-bg-deep)]">
                        <span className="text-sm" style={{ color: 'var(--c-text-primary)' }}>
                          {t.shareLiveUpdate}
                        </span>
                        <button
                          type="button"
                          role="switch"
                          aria-checked={liveUpdate}
                          onClick={() => setLiveUpdate(v => !v)}
                          className="relative h-5 w-9 rounded-full transition-colors"
                          style={{ background: liveUpdate ? 'var(--c-btn-bg)' : 'var(--c-bg-deep)' }}
                        >
                          <span
                            className="absolute top-0.5 left-0.5 h-4 w-4 rounded-full bg-white transition-transform"
                            style={{ transform: liveUpdate ? 'translateX(16px)' : 'translateX(0)' }}
                          />
                        </button>
                      </label>

                      {error && (
                        <p className="mb-3 text-xs" style={{ color: 'var(--c-destructive, #ef4444)' }}>{error}</p>
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
                  </motion.div>
                )}
              </AnimatePresence>

              {!showCreateForm && (
                <button
                  onClick={() => { setError(null); setShowCreateForm(true) }}
                  className="mt-3 flex w-full items-center justify-center gap-1.5 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors hover:bg-[var(--c-bg-sub)]"
                  style={{ color: 'var(--c-text-primary)', border: '0.5px dashed var(--c-border-subtle)' }}
                >
                  <Plus size={15} />
                  {t.shareCreateNew}
                </button>
              )}
            </div>
          </>
        )}
      </div>
    </div>
  )
}

type ShareItemProps = {
  share: ShareResponse
  deleting: boolean
  onCopy: () => void
  onDelete: () => void
  t: ReturnType<typeof useLocale>['t']
}

function ShareItem({ share, deleting, onCopy, onDelete, t }: ShareItemProps) {
  const [showPassword, setShowPassword] = useState(false)

  return (
    <div
      className="flex flex-col gap-2 rounded-xl px-3 py-2.5"
      style={{ background: 'var(--c-bg-sub)', border: '0.5px solid var(--c-border-subtle)' }}
    >
      <div className="flex items-center gap-2">
        <Link size={14} style={{ color: 'var(--c-text-icon)', flexShrink: 0 }} />
        <span className="flex-1 truncate text-sm" style={{ color: 'var(--c-text-primary)' }}>
          {window.location.origin}/s/{share.token}
        </span>
        <CopyIconButton
          onCopy={onCopy}
          size={12}
          tooltip={t.shareCopyLink}
          className="flex items-center rounded-md px-2 py-1 text-xs font-medium transition-colors hover:bg-[var(--c-bg-deep)]"
          style={{ color: 'var(--c-text-primary)' }}
        />
      </div>

      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <span className="flex items-center gap-1 text-xs" style={{ color: 'var(--c-text-muted)' }}>
            {share.access_type === 'password' ? <Lock size={11} /> : <Globe size={11} />}
            {share.access_type === 'password' ? t.sharePassword : t.sharePublic}
          </span>
          {share.access_type === 'password' && share.password && (
            <button
              onClick={() => setShowPassword(v => !v)}
              className="flex items-center gap-1 text-xs transition-colors hover:opacity-70"
              style={{ color: 'var(--c-text-muted)' }}
            >
              {showPassword ? <EyeOff size={11} /> : <Eye size={11} />}
              <span className="font-mono">
                {showPassword ? share.password : '\u2022'.repeat(share.password.length)}
              </span>
            </button>
          )}
          <span className="text-xs" style={{ color: share.live_update ? 'var(--c-success, #22c55e)' : 'var(--c-text-muted)' }}>
            {share.live_update
              ? t.shareLiveUpdate
              : `${t.shareFrozen} \u00b7 ${t.shareTurnCount(share.snapshot_turn_count)}`}
          </span>
        </div>
        <button
          onClick={onDelete}
          disabled={deleting}
          className="flex items-center gap-1 rounded-md px-1.5 py-1 text-xs transition-colors hover:bg-red-500/10 disabled:opacity-50"
          style={{ color: 'var(--c-destructive, #ef4444)' }}
        >
          {deleting ? <SpinnerIcon /> : <Trash2 size={12} />}
        </button>
      </div>
    </div>
  )
}
