import { BootstrapPage as SharedBootstrapPage } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'

type Props = {
  onLoggedIn: (accessToken: string) => void
}

export function BootstrapPage({ onLoggedIn }: Props) {
  const { t, locale } = useLocale()
  return <SharedBootstrapPage onLoggedIn={onLoggedIn} t={t} locale={locale} />
}
