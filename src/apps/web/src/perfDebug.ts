import { debugBus } from '@arkloop/shared'
import { isDesktop } from '@arkloop/shared/desktop'

type PerfSample = Record<string, string | number | boolean | null | undefined>
export type PerfTrace = {
  metric: string
  startedAt: number
  sample?: PerfSample
}

type Aggregate = {
  count: number
  total: number
  max: number
  last: number
  unit: string
  sample?: PerfSample
}

const DEBUG_PANEL_KEY = 'arkloop:web:developer_show_debug_panel'
const FLUSH_INTERVAL_MS = 1000

let initialized = false
let enabled = false
let flushTimer: ReturnType<typeof setTimeout> | null = null
const aggregates = new Map<string, Aggregate>()

function syncEnabledFromStorage() {
  if (typeof window === 'undefined') {
    enabled = false
    return
  }
  try {
    enabled = window.localStorage.getItem(DEBUG_PANEL_KEY) === 'true'
  } catch {
    enabled = false
  }
}

function ensureInitialized() {
  if (initialized) return
  initialized = true
  if (typeof window === 'undefined' || typeof localStorage === 'undefined') return
  syncEnabledFromStorage()
  window.addEventListener('arkloop:developer_show_debug_panel', ((event: Event) => {
    enabled = !!(event as CustomEvent<boolean>).detail
    if (!enabled) {
      aggregates.clear()
      if (flushTimer !== null) {
        clearTimeout(flushTimer)
        flushTimer = null
      }
    }
  }) as EventListener)
  window.addEventListener('storage', (event) => {
    if (event.key !== DEBUG_PANEL_KEY) return
    syncEnabledFromStorage()
  })
}

function shouldRecordPerf() {
  ensureInitialized()
  return enabled && import.meta.env.DEV && isDesktop() && typeof performance !== 'undefined'
}

function flushAggregates() {
  flushTimer = null
  if (!enabled || aggregates.size === 0) {
    aggregates.clear()
    return
  }
  for (const [metric, aggregate] of aggregates) {
    debugBus.emit({
      ts: Date.now(),
      type: `perf:${metric}`,
      source: 'web-perf',
      data: {
        metric,
        unit: aggregate.unit,
        count: aggregate.count,
        total: Number(aggregate.total.toFixed(2)),
        avg: Number((aggregate.total / Math.max(aggregate.count, 1)).toFixed(2)),
        max: Number(aggregate.max.toFixed(2)),
        last: Number(aggregate.last.toFixed(2)),
        sample: aggregate.sample,
      },
    })
  }
  aggregates.clear()
}

function scheduleFlush() {
  if (flushTimer !== null) return
  flushTimer = setTimeout(flushAggregates, FLUSH_INTERVAL_MS)
}

function recordPerfMetric(metric: string, value: number, unit: string, sample?: PerfSample) {
  if (!shouldRecordPerf() || !Number.isFinite(value)) return
  const current = aggregates.get(metric)
  if (current) {
    current.count += 1
    current.total += value
    current.max = Math.max(current.max, value)
    current.last = value
    if (sample) current.sample = sample
  } else {
    aggregates.set(metric, {
      count: 1,
      total: value,
      max: value,
      last: value,
      unit,
      sample,
    })
  }
  scheduleFlush()
}

export function recordPerfDuration(metric: string, durationMs: number, sample?: PerfSample) {
  recordPerfMetric(metric, durationMs, 'ms', sample)
}

export function recordPerfCount(metric: string, count = 1, sample?: PerfSample) {
  recordPerfMetric(metric, count, 'count', sample)
}

export function recordPerfValue(metric: string, value: number, unit: string, sample?: PerfSample) {
  recordPerfMetric(metric, value, unit, sample)
}

export function beginPerfTrace(metric: string, sample?: PerfSample): PerfTrace | null {
  if (!shouldRecordPerf()) return null
  return {
    metric,
    startedAt: performance.now(),
    sample,
  }
}

export function endPerfTrace(trace: PerfTrace | null, sample?: PerfSample) {
  if (!trace || typeof performance === 'undefined') return
  recordPerfDuration(trace.metric, performance.now() - trace.startedAt, {
    ...trace.sample,
    ...sample,
  })
}

export function isPerfDebugEnabled() {
  return shouldRecordPerf()
}
