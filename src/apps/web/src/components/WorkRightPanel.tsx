export type StepStatus = 'done' | 'active' | 'pending'

export type ProgressStep = {
  id: string
  label: string
  status: StepStatus
}

export type Connector = {
  name: string
  icon: 'globe' | 'monitor'
}

export type WorkRightPanelProps = {
  accessToken?: string
  projectId?: string
  steps?: ProgressStep[]
  connectors?: Connector[]
  onForbidden?: () => void
  readFiles?: string[]
  threadId?: string
  workFolder?: string | null
}

export function WorkRightPanel({ workFolder }: WorkRightPanelProps) {
  if (!workFolder?.trim()) return null

  return (
    <div style={{ height: '100%', color: 'var(--c-text-secondary)', fontSize: 13 }}>
      {workFolder}
    </div>
  )
}
