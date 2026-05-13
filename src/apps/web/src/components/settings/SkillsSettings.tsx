import { useLocale } from '../../contexts/LocaleContext'
import { SkillsSettingsContent } from '../SkillsSettingsContent'
import { SettingsPage } from './_SettingsLayout'

type Props = {
  accessToken: string
  onTrySkill?: (prompt: string) => void
}

export function SkillsSettings({ accessToken, onTrySkill }: Props) {
  const { t } = useLocale()
  return (
    <SettingsPage title={t.desktopSettings.skillsTitle} className="max-w-[760px]">
      <SkillsSettingsContent accessToken={accessToken} onTrySkill={onTrySkill} />
    </SettingsPage>
  )
}
