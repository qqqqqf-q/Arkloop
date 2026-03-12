import { ErrorCallout as SharedErrorCallout, type AppError } from '@arkloop/shared'
import { useLocale } from '../contexts/LocaleContext'

export function ErrorCallout({ error }: { error: AppError }) {
  const { locale, t } = useLocale()
  return <SharedErrorCallout error={error} locale={locale} requestFailedText={t.requestFailed} />
}

export type { AppError }
