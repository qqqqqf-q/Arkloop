export type ShortcutId =
  | 'openSettings'
  | 'openSearch'
  | 'toggleSidebar'
  | 'toggleRightPanel'
  | 'sendMessage'
  | 'insertNewLine'
  | 'openCommandMenu'
  | 'promptHistoryPrevious'
  | 'promptHistoryNext'
  | 'chooseCommand'
  | 'closeCommand'
  | 'togglePlanMode'

export type ShortcutModifier = 'mod' | 'shift' | 'alt'
export type ShortcutBinding = {
  key: string
  modifiers?: ShortcutModifier[]
}

export type ShortcutDefinition = {
  id: ShortcutId
  binding: ShortcutBinding
}

export type ShortcutPlatform = 'mac' | 'other'

export const SHORTCUTS: Record<ShortcutId, ShortcutDefinition> = {
  openSettings: {
    id: 'openSettings',
    binding: { key: ',', modifiers: ['mod'] },
  },
  openSearch: {
    id: 'openSearch',
    binding: { key: 'k', modifiers: ['mod'] },
  },
  toggleSidebar: {
    id: 'toggleSidebar',
    binding: { key: 'b', modifiers: ['mod'] },
  },
  toggleRightPanel: {
    id: 'toggleRightPanel',
    binding: { key: 'b', modifiers: ['alt', 'mod'] },
  },
  sendMessage: {
    id: 'sendMessage',
    binding: { key: 'Enter' },
  },
  insertNewLine: {
    id: 'insertNewLine',
    binding: { key: 'Enter', modifiers: ['shift'] },
  },
  openCommandMenu: {
    id: 'openCommandMenu',
    binding: { key: '/' },
  },
  promptHistoryPrevious: {
    id: 'promptHistoryPrevious',
    binding: { key: 'ArrowUp' },
  },
  promptHistoryNext: {
    id: 'promptHistoryNext',
    binding: { key: 'ArrowDown' },
  },
  chooseCommand: {
    id: 'chooseCommand',
    binding: { key: 'Enter' },
  },
  closeCommand: {
    id: 'closeCommand',
    binding: { key: 'Escape' },
  },
  togglePlanMode: {
    id: 'togglePlanMode',
    binding: { key: 'Tab', modifiers: ['shift'] },
  },
}

export function getShortcutPlatform(): ShortcutPlatform {
  if (typeof navigator === 'undefined') return 'other'
  const platform = navigator.platform.toLowerCase()
  return platform.includes('mac') || platform.includes('iphone') || platform.includes('ipad') ? 'mac' : 'other'
}

export function formatShortcut(binding: ShortcutBinding, platform = getShortcutPlatform()): string[] {
  const modifiers = binding.modifiers ?? []
  return [
    ...modifiers.map((modifier) => formatModifier(modifier, platform)),
    formatKey(binding.key),
  ]
}

export function shortcutAriaLabel(binding: ShortcutBinding, platform = getShortcutPlatform()): string {
  return [
    ...(binding.modifiers ?? []).map((modifier) => ariaModifier(modifier, platform)),
    ariaKey(binding.key),
  ].join('+')
}

export function matchesShortcut(event: KeyboardEvent, shortcut: ShortcutDefinition | ShortcutBinding): boolean {
  const binding = 'binding' in shortcut ? shortcut.binding : shortcut
  const modifiers = new Set(binding.modifiers ?? [])
  const wantsMod = modifiers.has('mod')
  const platform = getShortcutPlatform()
  const modPressed = platform === 'mac' ? event.metaKey : event.ctrlKey
  const extraPrimaryModifierPressed = platform === 'mac' ? event.ctrlKey : event.metaKey

  if (wantsMod !== modPressed) return false
  if (extraPrimaryModifierPressed) return false
  if (modifiers.has('shift') !== event.shiftKey) return false
  if (modifiers.has('alt') !== event.altKey) return false
  if (!wantsMod && (event.metaKey || event.ctrlKey)) return false

  return matchesShortcutKey(event, binding.key)
}

function formatModifier(modifier: ShortcutModifier, platform: ShortcutPlatform): string {
  switch (modifier) {
    case 'mod':
      return platform === 'mac' ? '⌘' : 'Ctrl'
    case 'shift':
      return platform === 'mac' ? '⇧' : 'Shift'
    case 'alt':
      return platform === 'mac' ? '⌥' : 'Alt'
  }
}

function ariaModifier(modifier: ShortcutModifier, platform: ShortcutPlatform): string {
  switch (modifier) {
    case 'mod':
      return platform === 'mac' ? 'Meta' : 'Control'
    case 'shift':
      return 'Shift'
    case 'alt':
      return 'Alt'
  }
}

function formatKey(key: string): string {
  switch (key) {
    case 'Enter':
      return 'Enter'
    case 'Escape':
      return 'Esc'
    case 'Tab':
      return 'Tab'
    case 'ArrowUp':
      return '↑'
    case 'ArrowDown':
      return '↓'
    default:
      return key.length === 1 ? key.toUpperCase() : key
  }
}

function ariaKey(key: string): string {
  return key.length === 1 ? key.toUpperCase() : key
}

function normalizeKey(key: string): string {
  if (key === 'Esc') return 'escape'
  return key.toLowerCase()
}

function matchesShortcutKey(event: KeyboardEvent, key: string): boolean {
  if (normalizeKey(event.key) === normalizeKey(key)) return true
  return event.code === physicalKeyCode(key)
}

function physicalKeyCode(key: string): string {
  const normalized = key.toLowerCase()
  if (/^[a-z]$/.test(normalized)) return `Key${normalized.toUpperCase()}`
  if (/^[0-9]$/.test(normalized)) return `Digit${normalized}`
  switch (key) {
    case ',':
      return 'Comma'
    case '/':
      return 'Slash'
    default:
      return key
  }
}
