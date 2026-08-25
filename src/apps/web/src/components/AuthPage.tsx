import { AuthPage as SharedAuthPage, type AuthApi } from '@arkloop/shared'
import { login, resolveIdentity } from '../api'
import { useLocale } from '../contexts/LocaleContext'

const api: AuthApi = {
  login,
  resolveIdentity,
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
