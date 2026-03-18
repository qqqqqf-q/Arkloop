type Ramp = {
  lightFill: string
  lightStroke: string
  lightTitle: string
  lightSubtitle: string
  darkFill: string
  darkStroke: string
  darkTitle: string
  darkSubtitle: string
}

const ramps: Record<string, Ramp> = {
  purple: {
    lightFill: '#EEEDFE',
    lightStroke: '#534AB7',
    lightTitle: '#3C3489',
    lightSubtitle: '#534AB7',
    darkFill: '#3C3489',
    darkStroke: '#AFA9EC',
    darkTitle: '#CECBF6',
    darkSubtitle: '#AFA9EC',
  },
  teal: {
    lightFill: '#E1F5EE',
    lightStroke: '#0F6E56',
    lightTitle: '#085041',
    lightSubtitle: '#0F6E56',
    darkFill: '#085041',
    darkStroke: '#5DCAA5',
    darkTitle: '#9FE1CB',
    darkSubtitle: '#5DCAA5',
  },
  coral: {
    lightFill: '#FAECE7',
    lightStroke: '#993C1D',
    lightTitle: '#712B13',
    lightSubtitle: '#993C1D',
    darkFill: '#712B13',
    darkStroke: '#F0997B',
    darkTitle: '#F5C4B3',
    darkSubtitle: '#F0997B',
  },
  pink: {
    lightFill: '#FBEAF0',
    lightStroke: '#993556',
    lightTitle: '#72243E',
    lightSubtitle: '#993556',
    darkFill: '#72243E',
    darkStroke: '#ED93B1',
    darkTitle: '#F4C0D1',
    darkSubtitle: '#ED93B1',
  },
  gray: {
    lightFill: '#F1EFE8',
    lightStroke: '#5F5E5A',
    lightTitle: '#444441',
    lightSubtitle: '#5F5E5A',
    darkFill: '#444441',
    darkStroke: '#B4B2A9',
    darkTitle: '#D3D1C7',
    darkSubtitle: '#B4B2A9',
  },
  blue: {
    lightFill: '#E6F1FB',
    lightStroke: '#185FA5',
    lightTitle: '#0C447C',
    lightSubtitle: '#185FA5',
    darkFill: '#0C447C',
    darkStroke: '#85B7EB',
    darkTitle: '#B5D4F4',
    darkSubtitle: '#85B7EB',
  },
  green: {
    lightFill: '#EAF3DE',
    lightStroke: '#3B6D11',
    lightTitle: '#27500A',
    lightSubtitle: '#3B6D11',
    darkFill: '#27500A',
    darkStroke: '#97C459',
    darkTitle: '#C0DD97',
    darkSubtitle: '#97C459',
  },
  amber: {
    lightFill: '#FAEEDA',
    lightStroke: '#854F0B',
    lightTitle: '#633806',
    lightSubtitle: '#854F0B',
    darkFill: '#633806',
    darkStroke: '#EF9F27',
    darkTitle: '#FAC775',
    darkSubtitle: '#EF9F27',
  },
  red: {
    lightFill: '#FCEBEB',
    lightStroke: '#A32D2D',
    lightTitle: '#791F1F',
    lightSubtitle: '#A32D2D',
    darkFill: '#791F1F',
    darkStroke: '#F09595',
    darkTitle: '#F7C1C1',
    darkSubtitle: '#F09595',
  },
}

function buildRampRules(selector: string, mode: keyof Ramp extends infer _ ? 'light' | 'dark' : never): string {
  const fillKey = mode === 'light' ? 'lightFill' : 'darkFill'
  const strokeKey = mode === 'light' ? 'lightStroke' : 'darkStroke'
  const titleKey = mode === 'light' ? 'lightTitle' : 'darkTitle'
  const subtitleKey = mode === 'light' ? 'lightSubtitle' : 'darkSubtitle'

  return Object.entries(ramps)
    .map(([name, ramp]) => `
${selector} svg .c-${name} > rect,
${selector} svg .c-${name} > circle,
${selector} svg .c-${name} > ellipse { fill: ${ramp[fillKey]}; stroke: ${ramp[strokeKey]}; }
${selector} svg .c-${name} > .th,
${selector} svg .c-${name} > .t { fill: ${ramp[titleKey]}; }
${selector} svg .c-${name} > .ts { fill: ${ramp[subtitleKey]}; }
${selector} svg rect.c-${name},
${selector} svg circle.c-${name},
${selector} svg ellipse.c-${name} { fill: ${ramp[fillKey]}; stroke: ${ramp[strokeKey]}; }`)
    .join('\n')
}

export const ARTIFACT_SVG_STYLES = `
:root {
  --p: var(--color-text-primary);
  --s: var(--color-text-secondary);
  --t: var(--color-text-tertiary);
  --bg2: var(--color-background-secondary);
  --b: var(--color-border-tertiary);
}

svg .t  { font-family: var(--c-font-body, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif); font-size: 14px; fill: var(--p); }
svg .ts { font-family: var(--c-font-body, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif); font-size: 12px; fill: var(--s); }
svg .th { font-family: var(--c-font-body, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif); font-size: 14px; font-weight: 500; fill: var(--p); }
svg .box { fill: var(--bg2); stroke: var(--b); }
svg .node { cursor: pointer; }
svg .node:hover { opacity: 0.8; }
svg .arr { stroke: var(--t); stroke-width: 1.5; fill: none; }
svg .leader { stroke: var(--t); stroke-width: 0.5; stroke-dasharray: 4 3; fill: none; }

${buildRampRules('html[data-theme="light"]', 'light')}
${buildRampRules('html[data-theme="dark"]', 'dark')}

@media (prefers-color-scheme: light) {
${buildRampRules('html:not([data-theme])', 'light')}
}

@media (prefers-color-scheme: dark) {
${buildRampRules('html:not([data-theme])', 'dark')}
}
`
