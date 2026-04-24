import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { WorkRightPanel } from '../components/WorkRightPanel'

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

  it('不渲染 Work Mode 右侧信息卡片', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <WorkRightPanel
          steps={[{ id: '1', label: '步骤一', status: 'done' }]}
          readFiles={['src/main.go']}
          connectors={[{ name: 'Web 搜索', icon: 'globe' }]}
        />,
      )
    })

    expect(container.textContent).toBe('')

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })
})
