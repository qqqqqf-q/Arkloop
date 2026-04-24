import { beforeEach } from 'vitest'

// jsdom 未实现 Blob URL；ArtifactIframe 等依赖此方法。
if (typeof URL.createObjectURL !== 'function') {
  Object.defineProperty(URL, 'createObjectURL', {
    configurable: true,
    writable: true,
    value: (_blob: Blob) => 'blob:jsdom-polyfill',
  })
}
if (typeof URL.revokeObjectURL !== 'function') {
  Object.defineProperty(URL, 'revokeObjectURL', {
    configurable: true,
    writable: true,
    value: (_url: string) => {},
  })
}

if (typeof HTMLCanvasElement !== 'undefined') {
  Object.defineProperty(HTMLCanvasElement.prototype, 'getContext', {
    configurable: true,
    writable: true,
    value: () => ({
      font: '',
      measureText: (text: string) => ({ width: text.length * 8 }),
    }),
  })
}

if (typeof navigator !== 'undefined') {
  Object.defineProperty(navigator, 'language', {
    configurable: true,
    get: () => 'zh-CN',
  })
  Object.defineProperty(navigator, 'languages', {
    configurable: true,
    get: () => ['zh-CN', 'zh'],
  })
}

beforeEach(() => {
  if (typeof localStorage !== 'undefined') {
    localStorage.clear()
    localStorage.setItem('arkloop:web:locale', 'zh')
  }
  if (typeof sessionStorage !== 'undefined') {
    sessionStorage.clear()
  }
})

if (typeof window !== 'undefined' && typeof window.scrollTo !== 'function') {
  Object.defineProperty(window, 'scrollTo', {
    configurable: true,
    writable: true,
    value: () => {},
  })
}

if (typeof globalThis.ResizeObserver === 'undefined') {
  class ResizeObserver {
    observe(): void {}
    unobserve(): void {}
    disconnect(): void {}
  }
  Object.defineProperty(globalThis, 'ResizeObserver', {
    configurable: true,
    writable: true,
    value: ResizeObserver,
  })
}
