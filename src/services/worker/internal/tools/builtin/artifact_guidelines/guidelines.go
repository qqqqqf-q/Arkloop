package artifactguidelines

var moduleSections = map[string][]string{
	"interactive": {sectionColorPalette, sectionUIComponents},
	"chart":       {sectionColorPalette, sectionUIComponents, sectionCharts},
	"diagram":     {sectionColorPalette, sectionSVGSetup, sectionDiagrams},
	"art":         {sectionSVGSetup, sectionArt},
}

const guidelineCore = `# Artifact Design Guidelines

## Streaming Architecture

Structure code so useful content appears early during streaming:
1. <style> block first (keep short)
2. HTML content next (renders progressively)
3. <script> block last (executes after streaming completes)

Rules:
- No gradients, shadows, or blur effects (they flash during DOM diffing)
- No HTML comments (waste tokens, break streaming)
- No inline event handlers; attach listeners in the final <script>
- Two font weights only: 400 (normal) and 500 (medium), never 600 or 700
- Sentence case for all labels, never Title Case or ALL CAPS

## Theme Integration

Use Arkloop CSS variables for ALL colors. Never hardcode color values.

### Background
- --c-bg-page: page background
- --c-bg-sub: secondary/card background
- --c-bg-input: input field background
- --c-bg-card-hover: hover state background

### Text
- --c-text-primary: main text
- --c-text-secondary: secondary text
- --c-text-tertiary: subdued text
- --c-text-muted: disabled/placeholder text

### Border
- --c-border: standard border
- --c-border-subtle: light border
- --c-border-mid: medium emphasis border

### Status
- --c-error-text: error state text

## Dark & Light Mode

Artifacts MUST work in both modes. Using CSS variables guarantees this.
The iframe inherits the host page's CSS variables, so var(--c-text-primary) automatically resolves to the correct color.
Never use hardcoded white/black; use --c-text-primary and --c-bg-page.

## Typography

- Body text: 14px, font-weight 400, line-height 1.5, color var(--c-text-primary)
- Labels: 12px, font-weight 500, color var(--c-text-secondary)
- Headings: 16px, font-weight 500, color var(--c-text-primary)
- Monospace: font-family: ui-monospace, SFMono-Regular, Menlo, monospace

## CDN Libraries

Load external libraries ONLY from these domains:
- cdnjs.cloudflare.com
- cdn.jsdelivr.net
- unpkg.com
- esm.sh

Use <script src="..."> in the script section, not import statements.

## Interaction Bridge

To send data back to the conversation:
` + "`" + `window.arkloop.sendPrompt("user selected option A")` + "`" + `
This injects a message as if the user typed it.

## Layout

- Max width: 100% of container (no fixed widths)
- Padding: 16px for content areas
- Border radius: 8px for cards, 6px for buttons
- Gap: 12px between items in a grid/flex layout`

const sectionColorPalette = `## Color Palette

Nine ramps, 7 stops each (100 to 900). Use for data visualization and accents.

| Ramp   | 100     | 200     | 300     | 400     | 500     | 700     | 900     |
|--------|---------|---------|---------|---------|---------|---------|---------|
| Purple | #EEEDFE | #CECBF6 | #AFA9EC | #7F77DD | #534AB7 | #3C3489 | #26215C |
| Teal   | #E1F5EE | #9FE1CB | #5DCAA5 | #1D9E75 | #0F6E56 | #085041 | #04342C |
| Coral  | #FAECE7 | #F5C4B3 | #F0997B | #D85A30 | #993C1D | #712B13 | #4A1B0C |
| Blue   | #E6F0FF | #B3D4FF | #80B8FF | #4D9CFF | #1A80FF | #0052CC | #003380 |
| Amber  | #FFF8E1 | #FFE082 | #FFD54F | #FFCA28 | #FFC107 | #FF8F00 | #E65100 |
| Green  | #E8F5E9 | #A5D6A7 | #66BB6A | #43A047 | #2E7D32 | #1B5E20 | #0D3B0F |
| Rose   | #FFF0F0 | #FFCDD2 | #EF9A9A | #E57373 | #EF5350 | #C62828 | #7F1D1D |
| Slate  | #F0F0EF | #D4D4D2 | #A8A8A5 | #78787A | #525254 | #3A3A3C | #1C1C1E |
| Indigo | #EEF0FF | #C7CCFF | #9FA8FF | #7B85F0 | #5C63D2 | #3D41A0 | #252770 |

Rules:
- Color encodes meaning, not sequence
- 2-3 ramps per widget maximum
- Text on colored backgrounds: use the 700/900 stop from the same ramp, never plain black
- Use 100 stops for subtle backgrounds, 500 for primary accents`

const sectionUIComponents = `## UI Components

### Cards
- Background: var(--c-bg-sub)
- Border: 0.5px solid var(--c-border-subtle)
- Border radius: 8px
- Padding: 16px 20px

### Buttons
- Primary: background var(--c-btn-bg), color var(--c-btn-text), border-radius 6px, padding 8px 16px
- Secondary: background transparent, border 0.5px solid var(--c-border), color var(--c-text-secondary)
- Hover: opacity 0.85 for primary, background var(--c-bg-card-hover) for secondary

### Metric Cards
- Large number: 24px, font-weight 500, color from palette ramp
- Label below: 12px, color var(--c-text-tertiary)
- Subtle colored background using palette 100 stops

### Form Elements
- Input: background var(--c-bg-input), border 0.5px solid var(--c-border-subtle), border-radius 6px, padding 8px 12px
- Select: same as input
- Range slider: accent-color from palette

### Layout Patterns
- Editorial: single column, max-width 640px
- Card grid: CSS Grid, auto-fill, minmax(200px, 1fr), gap 12px
- Comparison: side-by-side flex, equal width columns, gap 16px
- Dashboard: 2-3 column grid with metric cards on top`

const sectionCharts = `## Charts (Chart.js)

Load Chart.js from CDN:
<script src="https://cdn.jsdelivr.net/npm/chart.js@4"></script>

### Canvas Setup
Wrap canvas in a positioned container with explicit height:
<div style="position:relative; height:300px">
  <canvas id="myChart"></canvas>
</div>

### Configuration Rules
- Always set responsive: true, maintainAspectRatio: false
- Disable default legend: plugins.legend.display = false
- Build custom HTML legends for better styling control
- Use palette colors for datasets (e.g. Teal-500 for primary, Coral-500 for secondary)
- Gridlines: color var(--c-border-subtle), lineWidth 0.5
- Tick labels: color var(--c-text-tertiary), font size 11px
- Tooltip: backgroundColor var(--c-bg-sub), borderColor var(--c-border), titleColor var(--c-text-primary)

### Number Formatting
- Negative currency: -$5M not $-5M
- Use Intl.NumberFormat for locale-aware formatting
- K/M/B suffixes for large numbers

### Chart Types
- Line: smooth tension 0.3, point radius 0 (show on hover only)
- Bar: border-radius 4px on top corners, max bar thickness 40px
- Doughnut: cutout 65%, no border between segments
- Pie: avoid if > 5 segments, use horizontal bar instead`

const sectionSVGSetup = `## SVG Setup

### ViewBox
Every SVG must declare viewBox. Verification checklist:
1. All nodes fit within viewBox bounds
2. Arrow markers do not clip
3. Text does not overflow node boundaries
4. Minimum 20px padding on all sides
5. Aspect ratio matches intended display

### Font Width Calibration
For calculating text width in SVG:
- 14px normal: ~7.5px per character
- 12px normal: ~6.5px per character
- 16px medium: ~9px per character
Add 24px padding (12px each side) to computed text width for node sizing.

### CSS Classes (define in <style>)
- .node: fill var(--c-bg-sub), stroke var(--c-border), stroke-width 0.5, rx 8
- .node-label: fill var(--c-text-primary), font-size 14px, text-anchor middle, dominant-baseline central
- .connector: fill none, stroke var(--c-border-mid), stroke-width 1
- .arrow: marker-end url(#arrowhead)

### Arrow Markers
<defs>
  <marker id="arrowhead" markerWidth="8" markerHeight="6" refX="8" refY="3" orient="auto">
    <path d="M0,0 L8,3 L0,6" fill="none" stroke="context-stroke" stroke-width="1"/>
  </marker>
</defs>

Important: connector paths must have fill="none" (SVG defaults to fill:black).
Use context-stroke on markers so arrow color inherits from the path.`

const sectionDiagrams = `## Diagrams

### Decision Framework
Route on the verb, not the noun:
- "how does X work" -> Flowchart (process steps with arrows)
- "X architecture" / "structure of X" -> Structural (boxes and connections)
- "explain X" / "illustrate X" -> Illustrative (visual metaphor)

### Complexity Budget
- Max 5 words per node label
- Max 4 boxes per horizontal tier
- Max 3 tiers for simple diagrams, 5 for complex
- If > 20 nodes needed, split into multiple diagrams

### Flowchart Rules
- Flow direction: top-to-bottom or left-to-right, never mixed
- Decision diamonds: center text, two labeled exits (yes/no or true/false)
- Use palette ramps to group related nodes (e.g. all "input" nodes in Teal, "process" in Purple)
- Start/end nodes: rounded rectangle (rx=16)

### Structural Diagrams
- Hierarchy: parent centered above children
- Connections: straight lines for direct hierarchy, curved for cross-references
- Grouping: use subtle colored rectangles (palette 100 stops) as backgrounds for clusters

### Arrow Intersection
Before finalizing: verify no arrows cross through nodes. Route around obstacles.
This is the #1 cause of diagram rendering failures.

### Box Width from Label
Calculate: (character count * char width) + padding.
Never hardcode box widths. Measure from the actual label text.`

const sectionArt = `## Art & Illustration

### Style
- Flat illustration style, no 3D effects or gradients
- Clean geometric shapes with subtle rounded corners
- Limited palette: pick 2-3 ramps from the color palette
- White space is a design element; don't fill every area

### Composition
- Visual hierarchy through size contrast (hero element 3x supporting elements)
- Rule of thirds for positioning
- Consistent stroke width throughout (1px or 1.5px)
- No outlines on filled shapes; use color contrast for separation

### Iconographic Elements
- Simple geometric forms: circles, rounded rectangles, lines
- Icon size: 24x24 or 32x32 within larger compositions
- Consistent corner radius across all shapes (4px for small, 8px for large)

### Background
- Use var(--c-bg-page) or transparent for the SVG background
- Optional: single palette 100 stop as a subtle tint
- No pattern fills or textures`
