import { SkillsSettingsContent } from '../SkillsSettingsContent'

type Props = {
  accessToken: string
  onTrySkill?: (prompt: string) => void
}

export function SkillsSettings({ accessToken, onTrySkill }: Props) {
  return <SkillsSettingsContent accessToken={accessToken} onTrySkill={onTrySkill} />
}
