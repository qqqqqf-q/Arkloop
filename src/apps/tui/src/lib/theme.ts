import { RGBA } from "@opentui/core"

export const tuiTheme = {
  // Temporary Terra dark mapping for TUI evaluation.
  panel: RGBA.fromHex("#1E1D1B"),
  element: RGBA.fromHex("#141413"),
  border: RGBA.fromHex("#5e5d55"),
  borderSubtle: RGBA.fromHex("#48473f"),
  text: RGBA.fromHex("#eeeeee"),
  textMuted: RGBA.fromHex("#9e9d97"),
  primary: RGBA.fromHex("#d49a79"),
  accent: RGBA.fromHex("#c3825c"),
  userPromptGlow: RGBA.fromHex("#5a4a43"),
  userPromptMid: RGBA.fromHex("#403c38"),
  userPromptBg: RGBA.fromHex("#30302E"),
  success: RGBA.fromHex("#4ade80"),
  warning: RGBA.fromHex("#fbbf24"),
  error: RGBA.fromHex("#f87171"),
  info: RGBA.fromHex("#a8bcc8"),
}

// 选中行以 primary 为底，文字用深色保证对比度。
export const activeText = RGBA.fromHex("#262624")
