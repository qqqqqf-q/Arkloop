import { Loader2 } from 'lucide-react'
import { Modal } from '@arkloop/shared'
import { SettingsLabel } from '../settings/_SettingsLabel'
import { SettingsInput, settingsInputCls } from '../settings/_SettingsInput'
import { SettingsSelect } from '../settings/_SettingsSelect'
import {
  type FormState,
  type Transport,
  type HostRequirement,
  normalizeHostRequirement,
  type MCPCopy,
} from './types'

const TRANSPORT_OPTIONS = [
  { value: 'http_sse', label: 'HTTP SSE' },
  { value: 'streamable_http', label: 'Streamable HTTP' },
  { value: 'stdio', label: 'stdio' },
]

const HOST_OPTIONS = [
  { value: 'remote_http', label: 'Remote HTTP' },
  { value: 'cloud_worker', label: 'Cloud Worker' },
  { value: 'desktop_local', label: 'Desktop Local' },
  { value: 'desktop_sidecar', label: 'Desktop Sidecar' },
]

type Props = {
  open: boolean
  editing: boolean
  form: FormState
  setField: <K extends keyof FormState>(key: K, value: FormState[K]) => void
  formError: string
  saving: boolean
  onSave: () => void
  onClose: () => void
  copy: MCPCopy
}

export function MCPFormModal({
  open,
  editing,
  form,
  setField,
  formError,
  saving,
  onSave,
  onClose,
  copy,
}: Props) {
  const title = editing ? copy.formTitleEdit : copy.formTitleCreate
  const textareaCls = settingsInputCls('sm')

  return (
    <Modal open={open} onClose={onClose} title={title} width="520px">
      <div className="flex flex-col gap-4">
        {/* Name */}
        <div>
          <SettingsLabel>{copy.fieldName}</SettingsLabel>
          <SettingsInput
            value={form.displayName}
            onChange={(e) => setField('displayName', e.target.value)}
            placeholder={copy.fieldName}
          />
        </div>

        {/* Transport + Host Requirement */}
        <div className="grid grid-cols-2 gap-4">
          <div>
            <SettingsLabel>{copy.fieldTransport}</SettingsLabel>
            <SettingsSelect
              value={form.transport}
              options={TRANSPORT_OPTIONS}
              onChange={(v) => {
                const transport = v as Transport
                setField('transport', transport)
                setField('hostRequirement', normalizeHostRequirement(transport, form.hostRequirement))
              }}
            />
          </div>
          <div>
            <SettingsLabel>{copy.fieldHost}</SettingsLabel>
            <SettingsSelect
              value={form.hostRequirement}
              options={HOST_OPTIONS}
              onChange={(v) => setField('hostRequirement', v as HostRequirement)}
            />
          </div>
        </div>

        {/* Conditional Fields Based on Transport */}
        {form.transport === 'stdio' ? (
          <>
            <div>
              <SettingsLabel>{copy.fieldCommand}</SettingsLabel>
              <SettingsInput
                value={form.command}
                onChange={(e) => setField('command', e.target.value)}
                placeholder="/path/to/command"
              />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <SettingsLabel>{copy.fieldArgs}</SettingsLabel>
                <SettingsInput
                  value={form.args}
                  onChange={(e) => setField('args', e.target.value)}
                  placeholder="arg1, arg2, arg3"
                />
              </div>
              <div>
                <SettingsLabel>{copy.fieldCwd}</SettingsLabel>
                <SettingsInput
                  value={form.cwd}
                  onChange={(e) => setField('cwd', e.target.value)}
                  placeholder="/working/dir"
                />
              </div>
            </div>
            <div>
              <SettingsLabel>{copy.fieldEnv}</SettingsLabel>
              <textarea
                value={form.envJson}
                onChange={(e) => setField('envJson', e.target.value)}
                className={`${textareaCls} min-h-20`}
                placeholder='{"KEY": "value"}'
              />
            </div>
          </>
        ) : (
          <div>
            <SettingsLabel>{copy.fieldURL}</SettingsLabel>
            <SettingsInput
              value={form.url}
              onChange={(e) => setField('url', e.target.value)}
              placeholder="https://api.example.com"
            />
          </div>
        )}

        {/* Headers JSON */}
        <div>
          <SettingsLabel>{copy.fieldHeaders}</SettingsLabel>
          <textarea
            value={form.headersJson}
            onChange={(e) => setField('headersJson', e.target.value)}
            className={`${textareaCls} min-h-20`}
            placeholder='{"X-Custom-Header": "value"}'
          />
        </div>

        {/* Bearer Token + Timeout */}
        <div className="grid grid-cols-2 gap-4">
          <div>
            <SettingsLabel>{copy.fieldToken}</SettingsLabel>
            <SettingsInput
              value={form.bearerToken}
              onChange={(e) => setField('bearerToken', e.target.value)}
              placeholder="your-token-here"
            />
          </div>
          <div>
            <SettingsLabel>{copy.fieldTimeout}</SettingsLabel>
            <SettingsInput
              type="number"
              value={form.timeoutMs}
              onChange={(e) => setField('timeoutMs', e.target.value)}
              placeholder="30000"
            />
          </div>
        </div>

        {/* Form Error */}
        {formError && (
          <p className="text-xs" style={{ color: 'var(--c-status-error-text)' }}>
            {formError}
          </p>
        )}

        {/* Bottom Buttons */}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            onClick={onClose}
            disabled={saving}
            className="rounded-lg px-4 py-1.5 text-sm text-[var(--c-text-secondary)] transition-colors duration-150 hover:bg-[var(--c-bg-sub)]"
            style={{ border: '0.5px solid var(--c-border-subtle)' }}
          >
            {copy.cancel}
          </button>
          <button
            type="button"
            onClick={onSave}
            disabled={saving}
            className="flex items-center justify-center gap-1.5 rounded-lg px-4 py-1.5 text-sm font-medium transition-[filter] duration-150 hover:[filter:brightness(1.12)] active:[filter:brightness(0.95)] disabled:opacity-50"
            style={{ background: 'var(--c-btn-bg)', color: 'var(--c-btn-text)' }}
          >
            {saving && <Loader2 size={14} className="animate-spin" />}
            {saving ? copy.saving : editing ? copy.save : copy.create}
          </button>
        </div>
      </div>
    </Modal>
  )
}
