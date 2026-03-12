import { AuthPage as SharedAuthPage, type AuthApi } from '@arkloop/shared'
import { login, getCaptchaConfig, sendEmailOTP, verifyEmailOTP } from '../api'
import { useLocale } from '../contexts/LocaleContext'

const api: AuthApi = { login, getCaptchaConfig, sendEmailOTP, verifyEmailOTP }

type Props = {
  onLoggedIn: (accessToken: string) => void
}

export function AuthPage({ onLoggedIn }: Props) {
  const { t, locale } = useLocale()
  return (
    <SharedAuthPage
      onLoggedIn={onLoggedIn}
      brandLabel="Console"
      locale={locale}
      t={t}
      api={api}
    />
  )
}
