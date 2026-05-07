import { describe, expect, it } from 'vitest'
import { getAdaptiveMenuLeft } from '../components/settings/_SettingsSelectUtils'

describe('getAdaptiveMenuLeft', () => {
  it('控件在右侧时用右边缘作为菜单锚点', () => {
    const left = getAdaptiveMenuLeft({ left: 900, right: 1000, width: 100 }, 260, 1200)

    expect(left).toBe(740)
  })

  it('左侧空间不足时保持左边缘锚点', () => {
    const left = getAdaptiveMenuLeft({ left: 120, right: 220, width: 100 }, 260, 1200)

    expect(left).toBe(120)
  })

  it('菜单超出视口时只夹紧到安全边距', () => {
    const left = getAdaptiveMenuLeft({ left: 300, right: 400, width: 100 }, 750, 1000)

    expect(left).toBe(234)
  })
})
