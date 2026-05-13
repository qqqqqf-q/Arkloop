import { act, type ComponentProps } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { LocaleProvider } from '../contexts/LocaleContext'
import { RightPanel, type RightPanelTab } from './RightPanel'

function LocalizedRightPanel(props: ComponentProps<typeof RightPanel>) {
  return (
    <LocaleProvider>
      <RightPanel {...props} />
    </LocaleProvider>
  )
}

function createDragEvent(type: string): DragEvent {
  const event = new Event(type, { bubbles: true, cancelable: true }) as DragEvent
  Object.defineProperty(event, 'dataTransfer', {
    value: {
      effectAllowed: '',
      dropEffect: '',
      setData: vi.fn(),
      getData: vi.fn(),
    },
  })
  return event
}

function setClientX(event: DragEvent, clientX: number): DragEvent {
  Object.defineProperty(event, 'clientX', { configurable: true, value: clientX })
  return event
}

describe('RightPanel', () => {
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

  it('只渲染当前选中的 tab 内容', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const onSelectTab = vi.fn()
    const tabs: RightPanelTab[] = [
      { id: 'files', kind: 'files', title: 'Files', content: <div>file tree</div> },
      { id: 'doc', kind: 'resource', title: 'random-doc', content: <div>doc body</div> },
    ]

    await act(async () => {
      root.render(<LocalizedRightPanel tabs={tabs} activeTabId="files" onSelectTab={onSelectTab} />)
    })

    expect(container.textContent).toContain('file tree')
    expect(container.textContent).not.toContain('doc body')

    await act(async () => {
      container.querySelectorAll('button')[1]?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(onSelectTab).toHaveBeenCalledWith('doc')

    await act(async () => {
      root.render(<LocalizedRightPanel tabs={tabs} activeTabId="doc" onSelectTab={onSelectTab} />)
    })

    expect(container.textContent).not.toContain('file tree')
    expect(container.textContent).toContain('doc body')

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('关闭按钮不触发 tab 选择', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const onSelectTab = vi.fn()
    const onCloseTab = vi.fn()

    await act(async () => {
      root.render(
        <LocalizedRightPanel
          tabs={[{ id: 'doc', kind: 'resource', title: 'random-doc', content: <div>doc body</div> }]}
          activeTabId="doc"
          onSelectTab={onSelectTab}
          onCloseTab={onCloseTab}
        />,
      )
    })

    await act(async () => {
      container.querySelector('[aria-label="Close random-doc"]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })

    expect(onCloseTab).toHaveBeenCalledWith('doc')
    expect(onSelectTab).not.toHaveBeenCalled()
    expect(container.querySelector('[aria-label="Close random-doc"]')?.className).toContain('absolute')

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('支持只显示图标的工具 tab', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <LocalizedRightPanel
          tabs={[{ id: 'files', kind: 'files', title: 'Files', hideTitle: true, content: <div>file tree</div> }]}
          activeTabId="files"
          onSelectTab={() => {}}
        />,
      )
    })

    expect(container.querySelector('.right-panel-tab__title')).toBeNull()
    expect(container.textContent).toContain('file tree')

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('tab 栏支持滚轮横向滚动', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const tabs: RightPanelTab[] = Array.from({ length: 8 }, (_, index) => ({
      id: `tab-${index}`,
      kind: 'resource',
      title: `Tab ${index}`,
      content: <div>{`body ${index}`}</div>,
    }))

    await act(async () => {
      root.render(<LocalizedRightPanel tabs={tabs} activeTabId="tab-0" onSelectTab={() => {}} />)
    })

    const track = container.querySelector<HTMLDivElement>('.right-panel-tabbar__track')
    expect(track).not.toBeNull()
    Object.defineProperty(track, 'scrollWidth', { configurable: true, value: 1000 })
    Object.defineProperty(track, 'clientWidth', { configurable: true, value: 160 })
    track!.scrollLeft = 0

    await act(async () => {
      track!.dispatchEvent(new WheelEvent('wheel', { bubbles: true, cancelable: true, deltaY: 80 }))
    })

    expect(track!.scrollLeft).toBe(80)
    expect(track!.className).toContain('right-panel-tabbar__track--fade-left')
    expect(track!.className).toContain('right-panel-tabbar__track--fade-right')

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })

  it('tab 支持拖拽重排', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    const tabs: RightPanelTab[] = [
      { id: 'a', kind: 'resource', title: 'A', content: <div>A body</div> },
      { id: 'b', kind: 'resource', title: 'B', content: <div>B body</div> },
      { id: 'c', kind: 'resource', title: 'C', content: <div>C body</div> },
    ]

    await act(async () => {
      root.render(<LocalizedRightPanel tabs={tabs} activeTabId="a" onSelectTab={() => {}} />)
    })

    const tabButtons = () => Array.from(container.querySelectorAll<HTMLButtonElement>('.right-panel-tab'))
    expect(tabButtons().map((button) => button.textContent)).toEqual(['A', 'B', 'C'])
    vi.spyOn(tabButtons()[2]!, 'getBoundingClientRect').mockReturnValue({
      x: 200,
      y: 0,
      left: 200,
      right: 260,
      top: 0,
      bottom: 28,
      width: 60,
      height: 28,
      toJSON: () => ({}),
    })

    await act(async () => {
      tabButtons()[0]?.dispatchEvent(createDragEvent('dragstart'))
      tabButtons()[2]?.dispatchEvent(setClientX(createDragEvent('dragover'), 259))
    })

    expect(tabButtons().map((button) => button.textContent)).toEqual(['A', 'B', 'C'])
    expect(container.querySelector('.right-panel-tab__drop-indicator--after')).not.toBeNull()

    await act(async () => {
      tabButtons()[2]?.dispatchEvent(setClientX(createDragEvent('drop'), 259))
      tabButtons()[0]?.dispatchEvent(createDragEvent('dragend'))
    })

    expect(tabButtons().map((button) => button.textContent)).toEqual(['B', 'C', 'A'])

    await act(async () => {
      root.unmount()
    })
    container.remove()
  })
})
