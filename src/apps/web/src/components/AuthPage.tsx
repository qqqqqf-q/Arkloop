import { AuthPage as SharedAuthPage, type AuthApi } from '@arkloop/shared'
import {
  login,
  register,
  resolveIdentity,
  sendResolvedEmailOTP,
  verifyResolvedEmailOTP,
  getCaptchaConfig,
} from '../api'
import { useLocale } from '../contexts/LocaleContext'

const api: AuthApi = {
  login,
  getCaptchaConfig,
  resolveIdentity,
  register,
  sendResolvedEmailOTP,
  verifyResolvedEmailOTP,
}

type Props = { onLoggedIn: (accessToken: string) => void }

export function AuthPage({ onLoggedIn }: Props) {
  const { t, locale } = useLocale()

  return (
    <SharedAuthPage
      onLoggedIn={onLoggedIn}
      brandLabel="Arkloop"
      locale={locale}
      t={t}
      api={api}
    />
  )
}
