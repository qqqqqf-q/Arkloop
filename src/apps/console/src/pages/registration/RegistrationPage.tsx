import { useState, useCallback, useEffect } from 'react'
import { useOutletContext } from 'react-router-dom'
import { Loader2, Save } from 'lucide-react'
import type { ConsoleOutletContext } from '../../layouts/ConsoleLayout'
import { PageHeader } from '../../components/PageHeader'
import { Badge } from '../../components/Badge'
import { useToast } from '../../components/useToast'
import { isApiError } from '../../api'
import { useLocale } from '../../contexts/LocaleContext'
import {
  getFeatureFlag,
  createFeatureFlag,
  updateFeatureFlagDefault,
} from '../../api/feature-flags'
import {
  getPlatformSetting,
  setPlatformSetting,
} from '../../api/platform-settings'

const FLAG_KEY = 'registration.open'

const DEFAULTS = {
  'credit.initial_grant': '1000',
  'credit.invite_reward': '500',
} as const

type SettingKey = keyof typeof DEFAULTS

export function RegistrationPage() {
  const { accessToken } = useOutletContext<ConsoleOutletContext>()
  const { addToast } = useToast()
  const { t } = useLocale()
  const tc = t.pages.registration

  const [loading, setLoading] = useState(true)
  const [toggling, setToggling] = useState(false)
  const [openRegistration, setOpenRegistration] = useState<boolean | null>(null)

  // 平台设置
  const [initialGrant, setInitialGrant] = useState('')
  const [inviteReward, setInviteReward] = useState('')
  const [savedInitialGrant, setSavedInitialGrant] = useState('')
  const [savedInviteReward, setSavedInviteReward] = useState('')
  const [savingSettings, setSavingSettings] = useState(false)

  const loadMode = useCallback(async () => {
    setLoading(true)
    try {
      const flag = await getFeatureFlag(FLAG_KEY, accessToken).catch((err) => {
        if (isApiError(err) && err.status === 404) return null
        throw err
      })
      setOpenRegistration(flag?.default_value ?? false)

      // 加载平台设置
      const loadSetting = async (key: SettingKey) => {
        try {
          const s = await getPlatformSetting(key, accessToken)
          return s.value
        } catch (err) {
          if (isApiError(err) && err.status === 404) return DEFAULTS[key]
          throw err
        }
      }
      const [grant, reward] = await Promise.all([
        loadSetting('credit.initial_grant'),
        loadSetting('credit.invite_reward'),
      ])
      setInitialGrant(grant)
      setSavedInitialGrant(grant)
      setInviteReward(reward)
      setSavedInviteReward(reward)
    } catch {
      addToast(tc.toastLoadFailed, 'error')
    } finally {
      setLoading(false)
    }
  }, [accessToken, addToast, tc])

  useEffect(() => { void loadMode() }, [loadMode])

  const handleToggle = useCallback(async () => {
    const nextValue = !(openRegistration ?? false)
    setToggling(true)
    try {
      try {
        await updateFeatureFlagDefault(FLAG_KEY, { default_value: nextValue }, accessToken)
      } catch (err) {
        if (isApiError(err) && err.status === 404) {
          await createFeatureFlag({ key: FLAG_KEY, default_value: nextValue }, accessToken)
        } else {
          throw err
        }
      }
      setOpenRegistration(nextValue)
      addToast(tc.toastUpdated, 'success')
    } catch {
      addToast(tc.toastUpdateFailed, 'error')
    } finally {
      setToggling(false)
    }
  }, [openRegistration, accessToken, addToast, tc])

  const settingsChanged = initialGrant !== savedInitialGrant || inviteReward !== savedInviteReward

  const handleSaveSettings = useCallback(async () => {
    const grantNum = parseInt(initialGrant, 10)
    const rewardNum = parseInt(inviteReward, 10)
    if (isNaN(grantNum) || grantNum < 0) {
      addToast(tc.settingsErrPositive, 'error')
      return
    }
    if (isNaN(rewardNum) || rewardNum < 0) {
      addToast(tc.settingsErrPositive, 'error')
      return
    }

    setSavingSettings(true)
    try {
      const tasks: Promise<unknown>[] = []
      if (initialGrant !== savedInitialGrant) {
        tasks.push(setPlatformSetting('credit.initial_grant', String(grantNum), accessToken))
      }
      if (inviteReward !== savedInviteReward) {
        tasks.push(setPlatformSetting('credit.invite_reward', String(rewardNum), accessToken))
      }
      await Promise.all(tasks)
      setSavedInitialGrant(String(grantNum))
      setSavedInviteReward(String(rewardNum))
      setInitialGrant(String(grantNum))
      setInviteReward(String(rewardNum))
      addToast(tc.toastSettingsSaved, 'success')
    } catch {
      addToast(tc.toastSettingsFailed, 'error')
    } finally {
      setSavingSettings(false)
    }
  }, [initialGrant, inviteReward, savedInitialGrant, savedInviteReward, accessToken, addToast, tc])

  const isOpen = openRegistration ?? false

  return (
    <div className="flex h-full flex-col overflow-hidden">
      <PageHeader title={tc.title} />
      <div className="flex-1 overflow-y-auto p-6">
        {loading ? (
          <div className="flex items-center justify-center py-16">
            <Loader2 size={20} className="animate-spin text-[var(--c-text-muted)]" />
          </div>
        ) : (
          <div className="mx-auto max-w-xl space-y-6">
            {/* 注册模式 */}
            <div className="rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5">
              <div className="flex items-center justify-between">
                <div className="space-y-1">
                  <h3 className="text-sm font-medium text-[var(--c-text-primary)]">
                    {tc.modeLabel}
                  </h3>
                  <p className="text-xs text-[var(--c-text-muted)]">
                    {isOpen ? tc.modeOpenDesc : tc.modeInviteDesc}
                  </p>
                </div>
                <Badge variant={isOpen ? 'success' : 'warning'}>
                  {isOpen ? tc.modeOpen : tc.modeInvite}
                </Badge>
              </div>
              <div className="mt-4 border-t border-[var(--c-border-console)] pt-4">
                <button
                  onClick={handleToggle}
                  disabled={toggling}
                  className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
                >
                  {toggling && <Loader2 size={12} className="animate-spin" />}
                  {isOpen ? tc.switchToInvite : tc.switchToOpen}
                </button>
              </div>
            </div>

            {/* 邀请码说明 */}
            <div className="rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5">
              <h3 className="text-sm font-medium text-[var(--c-text-primary)]">
                {tc.inviteCodeTitle}
              </h3>
              <p className="mt-1 text-xs leading-relaxed text-[var(--c-text-muted)]">
                {isOpen ? tc.inviteCodeOpenHint : tc.inviteCodeInviteHint}
              </p>
            </div>

            {/* 推荐奖励设置 */}
            <div className="rounded-lg border border-[var(--c-border-console)] bg-[var(--c-bg-card)] p-5">
              <h3 className="text-sm font-medium text-[var(--c-text-primary)]">
                {tc.referralTitle}
              </h3>
              <div className="mt-4 space-y-4">
                <div>
                  <label className="mb-1 block text-xs font-medium text-[var(--c-text-secondary)]">
                    {tc.initialGrantLabel}
                  </label>
                  <input
                    type="number"
                    min="0"
                    value={initialGrant}
                    onChange={(e) => setInitialGrant(e.target.value)}
                    className="w-full rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-border-focus)]"
                  />
                </div>
                <div>
                  <label className="mb-1 block text-xs font-medium text-[var(--c-text-secondary)]">
                    {tc.inviteRewardLabel}
                  </label>
                  <input
                    type="number"
                    min="0"
                    value={inviteReward}
                    onChange={(e) => setInviteReward(e.target.value)}
                    className="w-full rounded-md border border-[var(--c-border-console)] bg-[var(--c-bg-input)] px-3 py-1.5 text-sm text-[var(--c-text-primary)] outline-none focus:border-[var(--c-border-focus)]"
                  />
                </div>
                <button
                  onClick={handleSaveSettings}
                  disabled={savingSettings || !settingsChanged}
                  className="inline-flex items-center gap-1.5 rounded-md border border-[var(--c-border-console)] px-3 py-1.5 text-xs font-medium text-[var(--c-text-secondary)] transition-colors hover:bg-[var(--c-bg-sub)] disabled:opacity-50"
                >
                  {savingSettings ? <Loader2 size={12} className="animate-spin" /> : <Save size={12} />}
                  {tc.saveSettings}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
