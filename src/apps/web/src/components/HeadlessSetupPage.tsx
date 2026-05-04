import { useMemo } from 'react'
import { BootstrapPage, type BootstrapApi } from '@arkloop/shared'
import { getDesktopAccessToken, isLocalMode } from '@arkloop/shared/desktop'
import { setLocalOwnerPassword } from '../api'
import { useLocale } from '../contexts/LocaleContext'

type Props = {
  onLoggedIn: (accessToken: string) => void
}

export function HeadlessSetupPage({ onLoggedIn }: Props) {
  const { t, locale } = useLocale()
  const desktopToken = getDesktopAccessToken()?.trim() ?? ''
  const token = new URLSearchParams(window.location.search).get('ark_web_local_token')?.trim() || 'local'

  const api = useMemo<BootstrapApi>(() => ({
    verifyToken: async () => ({
      valid: isLocalMode() && desktopToken !== '',
      expires_at: '',
    }),
    setup: async (req) => {
      const resp = await setLocalOwnerPassword({
        username: req.username,
        password: req.password,
      }, desktopToken)
      return {
        user_id: '',
        access_token: resp.access_token,
        token_type: resp.token_type,
      }
    },
  }), [desktopToken])

  return (
    <BootstrapPage
      onLoggedIn={onLoggedIn}
      t={t}
      locale={locale}
      tokenOverride={token}
      api={api}
      successPath="/"
    />
  )
}
