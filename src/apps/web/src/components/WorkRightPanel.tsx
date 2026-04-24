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
}

export function WorkRightPanel(_props: WorkRightPanelProps) {
  return null
}
