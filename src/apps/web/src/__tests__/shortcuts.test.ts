import { afterEach, describe, expect, it } from 'vitest'

import { SHORTCUTS, formatShortcut, matchesShortcut } from '../shortcuts'

const originalPlatform = navigator.platform

afterEach(() => {
  Object.defineProperty(navigator, 'platform', {
    configurable: true,
    value: originalPlatform,
  })
})

function setPlatform(platform: string) {
  Object.defineProperty(navigator, 'platform', {
    configurable: true,
    value: platform,
  })
}

describe('shortcuts', () => {
  it('区分侧栏和右侧面板的 B 快捷键', () => {
    setPlatform('MacIntel')

    const sidebarEvent = new KeyboardEvent('keydown', { key: 'b', metaKey: true })
    const rightPanelEvent = new KeyboardEvent('keydown', { key: 'b', altKey: true, metaKey: true })
    const planModeEvent = new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true })

    expect(matchesShortcut(sidebarEvent, SHORTCUTS.toggleSidebar)).toBe(true)
    expect(matchesShortcut(sidebarEvent, SHORTCUTS.toggleRightPanel)).toBe(false)
    expect(matchesShortcut(rightPanelEvent, SHORTCUTS.toggleRightPanel)).toBe(true)
    expect(matchesShortcut(rightPanelEvent, SHORTCUTS.toggleSidebar)).toBe(false)
    expect(matchesShortcut(planModeEvent, SHORTCUTS.togglePlanMode)).toBe(true)
  })

  it('使用物理按键匹配 Option 改写后的字符', () => {
    setPlatform('MacIntel')

    const rightPanelEvent = new KeyboardEvent('keydown', {
      key: '∫',
      code: 'KeyB',
      altKey: true,
      metaKey: true,
    })

    expect(matchesShortcut(rightPanelEvent, SHORTCUTS.toggleRightPanel)).toBe(true)
  })

  it('拒绝混入非当前平台的主修饰键', () => {
    setPlatform('MacIntel')
    expect(matchesShortcut(new KeyboardEvent('keydown', {
      key: ',',
      code: 'Comma',
      ctrlKey: true,
      metaKey: true,
    }), SHORTCUTS.openSettings)).toBe(false)

    setPlatform('Win32')
    expect(matchesShortcut(new KeyboardEvent('keydown', {
      key: ',',
      code: 'Comma',
      ctrlKey: true,
      metaKey: true,
    }), SHORTCUTS.openSettings)).toBe(false)
  })

  it('按平台显示修饰键', () => {
    expect(formatShortcut(SHORTCUTS.toggleSidebar.binding, 'mac')).toEqual(['⌘', 'B'])
    expect(formatShortcut(SHORTCUTS.toggleRightPanel.binding, 'mac')).toEqual(['⌥', '⌘', 'B'])
    expect(formatShortcut(SHORTCUTS.togglePlanMode.binding, 'mac')).toEqual(['⇧', 'Tab'])
    expect(formatShortcut(SHORTCUTS.toggleSidebar.binding, 'other')).toEqual(['Ctrl', 'B'])
    expect(formatShortcut(SHORTCUTS.toggleRightPanel.binding, 'other')).toEqual(['Alt', 'Ctrl', 'B'])
    expect(formatShortcut(SHORTCUTS.togglePlanMode.binding, 'other')).toEqual(['Shift', 'Tab'])
  })
})
