/// <reference types="vitest/globals" />

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

if (typeof window !== 'undefined' && typeof window.scrollTo !== 'function') {
  Object.defineProperty(window, 'scrollTo', {
    configurable: true,
    writable: true,
    value: () => {},
  })
}

if (typeof Element !== 'undefined' && typeof Element.prototype.scrollTo !== 'function') {
  Object.defineProperty(Element.prototype, 'scrollTo', {
    configurable: true,
    writable: true,
    value: function scrollTo(options?: ScrollToOptions | number, y?: number) {
      const element = this as Element & { scrollTop: number; scrollLeft: number }
      if (typeof options === 'number') {
        element.scrollLeft = options
        element.scrollTop = y ?? 0
        return
      }
      if (options?.left != null) {
        element.scrollLeft = options.left
      }
      if (options?.top != null) {
        element.scrollTop = options.top
      }
    },
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

// 测试默认用中文 locale
try {
  Object.defineProperty(navigator, 'language', {
    configurable: true,
    get() { return 'zh-CN' },
  })
} catch {
  // navigator.language 可能不可写；直接设 localStorage fallback
  try {
    localStorage.setItem('arkloop:web:locale', 'zh')
  } catch {
    // ignore
  }
}
