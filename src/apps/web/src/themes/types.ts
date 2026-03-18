export type ThemePreset = 'default' | 'terra' | 'github' | 'nord' | 'catppuccin' | 'tokyo-night' | 'custom'
export type FontFamily = 'inter' | 'system' | 'serif' | 'noto-sans' | 'source-sans' | 'custom'
export type CodeFontFamily = 'jetbrains-mono' | 'fira-code' | 'cascadia-code' | 'source-code-pro'
export type FontSize = 'compact' | 'normal' | 'relaxed'

export type ThemeColorVars = {
  // Backgrounds
  '--c-bg-page': string
  '--c-bg-sidebar': string
  '--c-bg-deep': string
  '--c-bg-deep2': string
  '--c-bg-sub': string
  '--c-bg-card': string
  '--c-bg-input': string
  '--c-bg-menu': string
  // Text
  '--c-text-primary': string
  '--c-text-heading': string
  '--c-text-secondary': string
  '--c-text-tertiary': string
  '--c-text-muted': string
  '--c-text-icon': string
  '--c-placeholder': string
  // Borders
  '--c-border': string
  '--c-border-subtle': string
  '--c-border-mid': string
  '--c-border-auth': string
  // Accent
  '--c-accent': string
  '--c-accent-fg': string
  '--c-btn-bg': string
  '--c-btn-text': string
  '--c-accent-send': string
  '--c-accent-send-hover': string
  '--c-accent-send-text': string
  '--c-bg-plus': string
  '--c-pro-bg': string
  // Status
  '--c-status-success-text': string
  '--c-status-error-text': string
  '--c-status-warning-text': string
  '--c-error-bg': string
  '--c-error-text': string
  '--c-error-border': string
  '--c-status-ok-bg': string
  '--c-status-ok-text': string
  '--c-status-danger-bg': string
  '--c-status-danger-text': string
  '--c-status-warn-bg': string
  '--c-status-warn-text': string
  // Code & Markdown
  '--c-md-code-block-bg': string
  '--c-md-inline-code-bg': string
  '--c-md-inline-code-color': string
  '--c-md-table-head-bg': string
  '--c-avatar-bg': string
  '--c-avatar-text': string
  '--c-code-panel-bg': string
  '--c-code-panel-output-bg': string
  // Interactive
  '--c-scrollbar': string
  '--c-scrollbar-hover': string
  '--c-bg-card-hover': string
  '--c-overlay': string
  '--c-lightbox-overlay': string
  '--c-modal-ring': string
  '--c-sidebar-btn-hover': string
  // Attachments
  '--c-attachment-bg': string
  '--c-attachment-border': string
  '--c-attachment-border-hover': string
  '--c-attachment-close-bg': string
  '--c-attachment-close-border': string
  '--c-attachment-badge-border': string
  // Input borders
  '--c-input-border-color': string
  '--c-input-border-color-hover': string
  '--c-input-border-color-focus': string
  // Mode switch & Claw
  '--c-mode-switch-track': string
  '--c-mode-switch-border': string
  '--c-mode-switch-pill': string
  '--c-mode-switch-active-text': string
  '--c-mode-switch-inactive-text': string
  '--c-claw-card-border': string
  '--c-claw-card-hover': string
  '--c-claw-step-pending': string
  '--c-claw-step-line': string
  '--c-claw-file-bg': string
  '--c-claw-file-border': string
  '--c-chip-active-bg': string
  '--c-chip-active-text': string
}

export type ThemeDefinition = {
  id: string
  name: string
  dark: Partial<ThemeColorVars>
  light: Partial<ThemeColorVars>
}

export type ColorGroup = {
  key: string
  vars: Array<keyof ThemeColorVars>
}

export const COLOR_GROUPS: ColorGroup[] = [
  {
    key: 'backgrounds',
    vars: ['--c-bg-page', '--c-bg-sidebar', '--c-bg-deep', '--c-bg-deep2', '--c-bg-sub', '--c-bg-card', '--c-bg-input', '--c-bg-menu'],
  },
  {
    key: 'text',
    vars: ['--c-text-primary', '--c-text-heading', '--c-text-secondary', '--c-text-tertiary', '--c-text-muted', '--c-text-icon', '--c-placeholder'],
  },
  {
    key: 'borders',
    vars: ['--c-border', '--c-border-subtle', '--c-border-mid', '--c-border-auth'],
  },
  {
    key: 'accent',
    vars: ['--c-accent', '--c-accent-fg', '--c-btn-bg', '--c-btn-text', '--c-accent-send', '--c-accent-send-hover', '--c-accent-send-text', '--c-bg-plus', '--c-pro-bg'],
  },
  {
    key: 'status',
    vars: ['--c-status-success-text', '--c-status-error-text', '--c-status-warning-text', '--c-error-bg', '--c-error-text', '--c-error-border', '--c-status-ok-bg', '--c-status-ok-text', '--c-status-danger-bg', '--c-status-danger-text', '--c-status-warn-bg', '--c-status-warn-text'],
  },
  {
    key: 'code',
    vars: ['--c-md-code-block-bg', '--c-md-inline-code-bg', '--c-md-inline-code-color', '--c-md-table-head-bg', '--c-avatar-bg', '--c-avatar-text', '--c-code-panel-bg', '--c-code-panel-output-bg'],
  },
  {
    key: 'interactive',
    vars: ['--c-scrollbar', '--c-scrollbar-hover', '--c-bg-card-hover', '--c-overlay', '--c-modal-ring', '--c-sidebar-btn-hover'],
  },
  {
    key: 'input',
    vars: ['--c-input-border-color', '--c-input-border-color-hover', '--c-input-border-color-focus'],
  },
  {
    key: 'claw',
    vars: ['--c-mode-switch-track', '--c-mode-switch-border', '--c-mode-switch-pill', '--c-mode-switch-active-text', '--c-mode-switch-inactive-text', '--c-claw-card-border', '--c-claw-card-hover', '--c-claw-file-bg', '--c-claw-file-border', '--c-claw-step-pending', '--c-claw-step-line'],
  },
]
