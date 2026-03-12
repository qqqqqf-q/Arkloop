import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ClawRightPanel } from '../components/ClawRightPanel'
import { LocaleProvider } from '../contexts/LocaleContext'
import { ApiError, getProjectWorkspace, listProjectWorkspaceFiles } from '../api'

vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return {
    ...actual,
    getProjectWorkspace: vi.fn(),
    listProjectWorkspaceFiles: vi.fn(),
  }
})

vi.mock('../components/WorkspaceResource', () => ({
  WorkspaceResource: ({ file, projectId }: { file: { filename: string }; projectId?: string }) => (
    <div data-testid="workspace-resource-mock">{projectId}:{file.filename}</div>
  ),
}))

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

describe('ClawRightPanel', () => {
  const mockedGetProjectWorkspace = vi.mocked(getProjectWorkspace)
  const mockedListProjectWorkspaceFiles = vi.mocked(listProjectWorkspaceFiles)
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  })

  afterEach(() => {
    vi.restoreAllMocks()
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('无 projectId 时显示空状态', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ClawRightPanel accessToken="token" />
        </LocaleProvider>,
      )
    })

    expect(container.textContent).toContain('设置工作目录以查看文件。')
    expect(mockedGetProjectWorkspace).not.toHaveBeenCalled()
    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('应加载根目录、展开目录并预览文件', async () => {
    mockedGetProjectWorkspace.mockResolvedValue({
      project_id: 'project-1',
      workspace_ref: 'wsref_test',
      owner_user_id: 'user-1',
      status: 'idle',
      last_used_at: '2026-03-11T00:00:00Z',
    })
    mockedListProjectWorkspaceFiles
      .mockResolvedValueOnce({
        workspace_ref: 'wsref_test',
        path: '/',
        items: [
          { name: 'src', path: '/src', type: 'dir', has_children: true },
          { name: 'README.md', path: '/README.md', type: 'file', mime_type: 'text/markdown' },
        ],
      })
      .mockResolvedValueOnce({
        workspace_ref: 'wsref_test',
        path: '/src',
        items: [{ name: 'main.go', path: '/src/main.go', type: 'file', mime_type: 'text/plain' }],
      })

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ClawRightPanel accessToken="token" projectId="project-1" />
        </LocaleProvider>,
      )
    })
    await act(async () => {
      await flushMicrotasks()
    })

    expect(mockedGetProjectWorkspace).toHaveBeenCalledWith('token', 'project-1')
    expect(mockedListProjectWorkspaceFiles).toHaveBeenCalledWith('token', 'project-1', '/')

    const dirButton = container.querySelector('[data-testid="claw-file-entry-/src"]') as HTMLButtonElement | null
    expect(dirButton).not.toBeNull()

    await act(async () => {
      dirButton?.click()
      await flushMicrotasks()
    })

    expect(mockedListProjectWorkspaceFiles).toHaveBeenLastCalledWith('token', 'project-1', '/src')

    const fileButton = container.querySelector('[data-testid="claw-file-entry-/src/main.go"]') as HTMLButtonElement | null
    expect(fileButton).not.toBeNull()

    await act(async () => {
      fileButton?.click()
      await flushMicrotasks()
    })

    expect(container.textContent).toContain('project-1:main.go')
    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('读取失败时显示错误状态', async () => {
    mockedGetProjectWorkspace.mockRejectedValue(new Error('boom'))

    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ClawRightPanel accessToken="token" projectId="project-1" />
        </LocaleProvider>,
      )
    })
    await act(async () => {
      await flushMicrotasks()
    })

    expect(container.textContent).toContain('暂时无法读取工作目录。')
    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('遇到 403 时回退到 chat', async () => {
    mockedGetProjectWorkspace.mockRejectedValue(new ApiError({ status: 403, message: 'forbidden' }))

    const onForbidden = vi.fn()
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <ClawRightPanel accessToken="token" projectId="project-1" onForbidden={onForbidden} />
        </LocaleProvider>,
      )
    })
    await act(async () => {
      await flushMicrotasks()
    })

    expect(onForbidden).toHaveBeenCalledTimes(1)

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })
})
