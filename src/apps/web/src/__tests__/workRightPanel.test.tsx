import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { WorkRightPanel } from '../components/WorkRightPanel'
import { LocaleProvider } from '../contexts/LocaleContext'

function flushMicrotasks(): Promise<void> {
  return Promise.resolve()
    .then(() => Promise.resolve())
    .then(() => Promise.resolve())
}

describe('WorkRightPanel', () => {
  const actEnvironment = globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }
  const originalActEnvironment = actEnvironment.IS_REACT_ACT_ENVIRONMENT

  beforeEach(() => {
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = true
  })

  afterEach(() => {
    if (originalActEnvironment === undefined) {
      delete actEnvironment.IS_REACT_ACT_ENVIRONMENT
    } else {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = originalActEnvironment
    }
  })

  it('readFiles 为空时工作目录卡片显示空状态文案', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <WorkRightPanel />
        </LocaleProvider>,
      )
    })
    await act(async () => {
      await flushMicrotasks()
    })

    expect(container.textContent).toContain('设置工作目录以查看文件。')

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('展示进度步骤与已读文件列表', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <WorkRightPanel
            steps={[
              { id: '1', label: '步骤一', status: 'done' },
              { id: '2', label: '步骤二', status: 'pending' },
            ]}
            readFiles={['src/main.go', 'README.md']}
          />
        </LocaleProvider>,
      )
    })
    await act(async () => {
      await flushMicrotasks()
    })

    expect(container.textContent).toContain('步骤一')
    expect(container.textContent).toContain('步骤二')
    expect(container.textContent).toContain('main.go')
    expect(container.textContent).toContain('README.md')

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('上下文连接器非空时列出名称', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocaleProvider>
          <WorkRightPanel connectors={[{ name: 'Web 搜索', icon: 'globe' }]} />
        </LocaleProvider>,
      )
    })
    await act(async () => {
      await flushMicrotasks()
    })

    expect(container.textContent).toContain('Web 搜索')

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })
})
