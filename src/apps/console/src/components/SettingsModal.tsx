import { SettingsModal } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'
import type { MeResponse } from '../api'

type Props = {
  me: MeResponse | null
  onClose: () => void
  onLogout: () => void
}

export function ConsoleSettingsModal({ me, onClose, onLogout }: Props) {
  const { t, locale, setLocale } = useLocale()
  return (
    <SettingsModal
      me={me}
      onClose={onClose}
      onLogout={onLogout}
      brandLabel="Console"
      roleLabel={t.platformAdmin}
      locale={locale}
      setLocale={setLocale}
      t={t}
    />
  )
}
