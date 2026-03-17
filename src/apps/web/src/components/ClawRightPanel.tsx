import { useEffect, useState } from 'react'
import {
  ChevronDown,
  Check,
  Globe,
  Monitor,
} from 'lucide-react'

import { useLocale } from '../contexts/LocaleContext'
import { isDesktop } from '@arkloop/shared/desktop'
import { readThreadClawFolder } from '../storage'

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

export type ClawRightPanelProps = {
  accessToken?: string
  projectId?: string
  steps?: ProgressStep[]
  connectors?: Connector[]
  onForbidden?: () => void
  readFiles?: string[]
  threadId?: string
}

function AnimatedHeight({
  open,
  children,
}: {
  open: boolean
  children: React.ReactNode
}) {
  return (
    <div
      style={{
        display: 'grid',
        gridTemplateRows: open ? '1fr' : '0fr',
        transition: 'grid-template-rows 220ms cubic-bezier(0.16,1,0.3,1)',
      }}
    >
      <div style={{ overflow: 'hidden' }}>{children}</div>
    </div>
  )
}

function Card({
  title,
  badge,
  defaultOpen = true,
  children,
}: {
  title: string
  badge?: string
  defaultOpen?: boolean
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)

  return (
    <div
      style={{
        borderRadius: 12,
        border: '0.5px solid var(--c-claw-card-border)',
        background: 'var(--c-bg-page)',
        overflow: 'hidden',
      }}
    >
      <button
        onClick={() => setOpen((value) => !value)}
        style={{
          display: 'flex',
          width: '100%',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '14px 16px',
          background: 'transparent',
          border: 'none',
          cursor: 'pointer',
        }}
      >
        <span
          style={{
            fontSize: '14px',
            fontWeight: 600,
            color: 'var(--c-text-primary)',
            letterSpacing: '-0.2px',
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {title}
        </span>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexShrink: 0 }}>
          {badge ? (
            <span style={{ fontSize: '12px', color: 'var(--c-text-muted)', textTransform: 'capitalize' }}>
              {badge}
            </span>
          ) : null}
          <span
            style={{
              color: 'var(--c-text-muted)',
              display: 'flex',
              transition: 'transform 200ms ease',
              transform: open ? 'rotate(0deg)' : 'rotate(-90deg)',
            }}
          >
            <ChevronDown size={16} />
          </span>
        </div>
      </button>
      <AnimatedHeight open={open}>
        <div style={{ padding: '0 16px 16px' }}>{children}</div>
      </AnimatedHeight>
    </div>
  )
}

function ProgressPanel({ steps }: { steps: ProgressStep[] }) {
  const { t } = useLocale()

  if (steps.length === 0) {
    return (
      <p style={{ fontSize: '13px', color: 'var(--c-text-muted)', margin: 0, lineHeight: 1.5 }}>
        {t.claw.progressEmpty}
      </p>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column' }}>
      {steps.map((step, index) => (
        <div key={step.id} style={{ display: 'flex', gap: 10 }}>
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              width: 20,
              flexShrink: 0,
            }}
          >
            {step.status === 'done' ? (
              <div
                style={{
                  width: 20,
                  height: 20,
                  borderRadius: '50%',
                  border: '1.5px solid var(--c-claw-step-done)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <Check size={11} color="var(--c-claw-step-done)" strokeWidth={2.5} />
              </div>
            ) : (
              <div
                style={{
                  width: 20,
                  height: 20,
                  borderRadius: '50%',
                  border: '1.5px solid var(--c-claw-step-pending)',
                }}
              />
            )}
            {index < steps.length - 1 ? (
              <div
                style={{
                  width: 1.5,
                  flex: 1,
                  minHeight: 12,
                  background: 'var(--c-claw-step-line)',
                }}
              />
            ) : null}
          </div>
          <span
            style={{
              fontSize: '13px',
              color: step.status === 'done' ? 'var(--c-text-secondary)' : 'var(--c-text-muted)',
              lineHeight: '20px',
              paddingBottom: index < steps.length - 1 ? 8 : 0,
            }}
          >
            {step.label}
          </span>
        </div>
      ))}
    </div>
  )
}

function fileExtension(name: string): string {
  const ext = name.split('.').pop()?.trim().toLowerCase()
  return ext || 'file'
}

function DocIcon({ ext }: { ext?: string }) {
  const tag = ext?.toUpperCase() ?? ''
  return (
    <div
      style={{
        width: 22,
        height: 26,
        borderRadius: 3,
        border: '0.5px solid var(--c-claw-card-border)',
        background: 'var(--c-claw-file-bg)',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'flex-end',
        padding: '0 0 2px',
        flexShrink: 0,
        position: 'relative',
      }}
    >
      <div
        style={{
          position: 'absolute',
          top: 0,
          right: 0,
          width: 6,
          height: 6,
          background: 'var(--c-bg-sub)',
          borderRadius: '0 0 0 1.5px',
        }}
      />
      <div style={{ display: 'flex', flexDirection: 'column', gap: 1.5, alignItems: 'center', marginTop: 5 }}>
        <div style={{ width: 10, height: 0.5, background: 'var(--c-claw-step-pending)', borderRadius: 0.5 }} />
        <div style={{ width: 10, height: 0.5, background: 'var(--c-claw-step-pending)', borderRadius: 0.5 }} />
      </div>
      {tag ? (
        <span
          style={{
            fontSize: '6px',
            fontWeight: 700,
            color: 'var(--c-text-muted)',
            letterSpacing: '0.2px',
            marginTop: 1,
            lineHeight: 1,
          }}
        >
          {tag}
        </span>
      ) : null}
    </div>
  )
}

function ConnectorIcon({ icon }: { icon: Connector['icon'] }) {
  const style = { color: 'var(--c-text-muted)' }
  switch (icon) {
    case 'globe':
      return <Globe size={15} style={style} />
    case 'monitor':
      return <Monitor size={15} style={style} />
    default:
      return <Globe size={15} style={style} />
  }
}

function ContextPanel({ connectors }: { connectors: Connector[] }) {
  const { t } = useLocale()

  if (connectors.length === 0) {
    return (
      <p style={{ fontSize: '13px', color: 'var(--c-text-muted)', margin: 0, lineHeight: 1.5 }}>
        {t.claw.contextEmpty}
      </p>
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <span style={{ fontSize: '12px', color: 'var(--c-text-muted)', marginBottom: 2 }}>{t.claw.toolsCalled}</span>
      {connectors.map((connector) => (
        <div
          key={connector.name}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 10,
            padding: '6px 2px',
            borderRadius: 6,
          }}
        >
          <div
            style={{
              width: 26,
              height: 26,
              borderRadius: 6,
              border: '0.5px solid var(--c-claw-card-border)',
              background: 'var(--c-claw-file-bg)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              flexShrink: 0,
            }}
          >
            <ConnectorIcon icon={connector.icon} />
          </div>
          <span style={{ fontSize: '13px', color: 'var(--c-text-secondary)' }}>{connector.name}</span>
        </div>
      ))}
    </div>
  )
}

function WorkingFolderPanel({ readFiles }: { readFiles: string[] }) {
  const { t } = useLocale()

  if (readFiles.length === 0) {
    return (
      <p style={{ fontSize: '13px', color: 'var(--c-text-muted)', margin: 0, lineHeight: 1.5 }}>
        {t.claw.workingFolderEmpty}
      </p>
    )
  }

  return (
    <div
      style={{
        maxHeight: '50vh',
        overflowY: 'auto',
        display: 'flex',
        flexDirection: 'column',
        gap: 2,
      }}
    >
      {readFiles.map((filename, idx) => (
        <div
          key={`${filename}-${idx}`}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            padding: '5px 4px',
            borderRadius: 6,
          }}
        >
          <DocIcon ext={fileExtension(filename)} />
          <span
            style={{
              fontSize: '12px',
              color: 'var(--c-text-secondary)',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              flex: 1,
            }}
          >
            {filename}
          </span>
        </div>
      ))}
    </div>
  )
}

const clawPanelWidth = 300

export function ClawRightPanel({
  steps = [],
  connectors = [],
  readFiles = [],
  threadId,
}: ClawRightPanelProps) {
  const { t } = useLocale()
  const doneCount = steps.filter((step) => step.status === 'done').length

  const [localFolder, setLocalFolder] = useState<string | null>(() =>
    threadId ? readThreadClawFolder(threadId) : null,
  )

  useEffect(() => {
    setLocalFolder(threadId ? readThreadClawFolder(threadId) : null)
  }, [threadId])

  useEffect(() => {
    const handler = () => setLocalFolder(threadId ? readThreadClawFolder(threadId) : null)
    window.addEventListener('arkloop:claw-folder-changed', handler)
    return () => window.removeEventListener('arkloop:claw-folder-changed', handler)
  }, [threadId])

  const folderDisplayName = isDesktop() && localFolder
    ? localFolder.split(/[/\\]/).filter(Boolean).pop() ?? localFolder
    : null

  const workingFolderTitle = folderDisplayName ?? t.claw.workingFolder

  return (
    <div
      style={{
        width: clawPanelWidth,
        height: '100%',
        flexShrink: 0,
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        background: 'var(--c-bg-page)',
      }}
    >
      <div
        style={{
          flex: 1,
          overflowY: 'auto',
          padding: '8px 10px',
          display: 'flex',
          flexDirection: 'column',
          gap: 8,
        }}
      >
        <Card title={t.claw.progress} badge={steps.length > 0 ? `${doneCount} of ${steps.length}` : undefined} defaultOpen>
          <ProgressPanel steps={steps} />
        </Card>

        <Card title={workingFolderTitle} defaultOpen>
          <WorkingFolderPanel readFiles={readFiles} />
        </Card>

        <Card title={t.claw.context} defaultOpen>
          <ContextPanel connectors={connectors} />
        </Card>
      </div>
    </div>
  )
}
