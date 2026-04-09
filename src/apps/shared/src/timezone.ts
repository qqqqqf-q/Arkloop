let activeTimeZone: string | null = null

export type ResolveTimeZoneInput = {
  userTimeZone?: string | null
  accountTimeZone?: string | null
  fallbackTimeZone?: string | null
}

export type FormatDateTimeOptions = {
  timeZone?: string | null
  includeDate?: boolean
  includeSeconds?: boolean
  includeMilliseconds?: boolean
  includeZone?: boolean
}

type DateTimeLocalParts = {
  year: number
  month: number
  day: number
  hour: number
  minute: number
  second: number
}

function isValidDateTimeLocalParts(parts: DateTimeLocalParts): boolean {
  const { year, month, day, hour, minute, second } = parts
  if (month < 1 || month > 12) return false
  if (day < 1 || day > 31) return false
  if (hour < 0 || hour > 23) return false
  if (minute < 0 || minute > 59) return false
  if (second < 0 || second > 59) return false

  const normalized = new Date(Date.UTC(year, month - 1, day, hour, minute, second))
  return normalized.getUTCFullYear() === year
    && normalized.getUTCMonth() === month - 1
    && normalized.getUTCDate() === day
    && normalized.getUTCHours() === hour
    && normalized.getUTCMinutes() === minute
    && normalized.getUTCSeconds() === second
}

export function normalizeTimeZone(value: string | null | undefined): string | null {
  const cleaned = value?.trim()
  if (!cleaned) return null
  try {
    return new Intl.DateTimeFormat('en-US', { timeZone: cleaned }).resolvedOptions().timeZone
  } catch {
    return null
  }
}

export function detectDeviceTimeZone(): string {
  return normalizeTimeZone(Intl.DateTimeFormat().resolvedOptions().timeZone) ?? 'UTC'
}

export function resolveTimeZone(input: ResolveTimeZoneInput): string {
  return normalizeTimeZone(input.userTimeZone)
    ?? normalizeTimeZone(input.accountTimeZone)
    ?? normalizeTimeZone(input.fallbackTimeZone)
    ?? detectDeviceTimeZone()
}

export function setActiveTimeZone(timeZone: string | null | undefined): void {
  activeTimeZone = normalizeTimeZone(timeZone)
}

export function getActiveTimeZone(): string {
  return activeTimeZone ?? detectDeviceTimeZone()
}

export function listSupportedTimeZones(): string[] {
  const supported = (Intl as typeof Intl & {
    supportedValuesOf?: (kind: string) => string[]
  }).supportedValuesOf?.('timeZone')
  if (Array.isArray(supported) && supported.length > 0) {
    return supported
  }
  return Array.from(new Set([detectDeviceTimeZone(), 'UTC']))
}

function parseDateInput(value: string | Date): Date | null {
  const date = value instanceof Date ? value : new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

function parseDateTimeLocalInput(value: string): DateTimeLocalParts | null {
  const match = value.trim().match(/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/)
  if (!match) return null
  return {
    year: Number(match[1]),
    month: Number(match[2]),
    day: Number(match[3]),
    hour: Number(match[4]),
    minute: Number(match[5]),
    second: Number(match[6] ?? '0'),
  }
}

function getZonedParts(
  value: string | Date,
  timeZone: string,
  includeMilliseconds = false,
): Record<string, string> | null {
  const date = parseDateInput(value)
  if (!date) return null
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    ...(includeMilliseconds ? { fractionalSecondDigits: 3 as const } : {}),
    hour12: false,
  }).formatToParts(date)
  const out: Record<string, string> = {}
  for (const part of parts) {
    if (part.type !== 'literal') out[part.type] = part.value
  }
  return out
}

export function formatTimeZoneOffset(value: string | Date, timeZone?: string | null): string {
  const resolved = resolveTimeZone({ fallbackTimeZone: timeZone ?? getActiveTimeZone() })
  const date = parseDateInput(value)
  if (!date) return 'UTC'
  const offsetMinutes = getTimeZoneOffsetMinutes(date, resolved)
  const sign = offsetMinutes >= 0 ? '+' : '-'
  const absMinutes = Math.abs(offsetMinutes)
  const hours = Math.floor(absMinutes / 60)
  const minutes = absMinutes % 60
  return minutes === 0
    ? `UTC${sign}${hours}`
    : `UTC${sign}${hours}:${String(minutes).padStart(2, '0')}`
}

function getTimeZoneOffsetMinutes(date: Date, timeZone: string): number {
  const zoned = new Date(date.toLocaleString('en-US', { timeZone }))
  const utc = new Date(date.toLocaleString('en-US', { timeZone: 'UTC' }))
  return Math.round((zoned.getTime() - utc.getTime()) / 60000)
}

function matchesDateTimeLocalParts(parts: Record<string, string> | null, target: DateTimeLocalParts): boolean {
  if (!parts) return false
  return Number(parts.year) === target.year
    && Number(parts.month) === target.month
    && Number(parts.day) === target.day
    && Number(parts.hour) === target.hour
    && Number(parts.minute) === target.minute
    && Number(parts.second) === target.second
}

export function parseDateTimeLocalToUTC(value: string, timeZone?: string | null): string | undefined {
  const parsed = parseDateTimeLocalInput(value)
  if (!parsed || !isValidDateTimeLocalParts(parsed)) return undefined
  const resolved = resolveTimeZone({ fallbackTimeZone: timeZone ?? getActiveTimeZone() })
  const targetWallClock = Date.UTC(
    parsed.year,
    parsed.month - 1,
    parsed.day,
    parsed.hour,
    parsed.minute,
    parsed.second,
  )

  const offsets = new Set<number>()
  for (const sample of [targetWallClock - 36 * 3600_000, targetWallClock - 12 * 3600_000, targetWallClock, targetWallClock + 12 * 3600_000, targetWallClock + 36 * 3600_000]) {
    offsets.add(getTimeZoneOffsetMinutes(new Date(sample), resolved))
  }

  const matches = new Set<number>()
  for (const offsetMinutes of offsets) {
    const candidate = targetWallClock - offsetMinutes * 60_000
    if (matchesDateTimeLocalParts(getZonedParts(new Date(candidate), resolved), parsed)) {
      matches.add(candidate)
    }
  }

  if (matches.size !== 1) return undefined
  return new Date([...matches][0]).toISOString()
}

export function formatDateTime(value: string | Date, options: FormatDateTimeOptions = {}): string {
  const {
    timeZone = getActiveTimeZone(),
    includeDate = true,
    includeSeconds = false,
    includeMilliseconds = false,
    includeZone = false,
  } = options
  const resolved = resolveTimeZone({ fallbackTimeZone: timeZone })
  const parts = getZonedParts(value, resolved, includeMilliseconds)
  if (!parts) {
    return typeof value === 'string' ? value : String(value)
  }
  const datePart = `${parts.year}-${parts.month}-${parts.day}`
  const secondPart = (includeSeconds || includeMilliseconds) ? `:${parts.second}` : ''
  const millisecondPart = includeMilliseconds ? `.${parts.fractionalSecond ?? '000'}` : ''
  const timePart = `${parts.hour}:${parts.minute}${secondPart}${millisecondPart}`
  const zonePart = includeZone ? ` [${formatTimeZoneOffset(value, resolved)}]` : ''
  if (!includeDate) {
    return `${timePart}${zonePart}`
  }
  return `${datePart} ${timePart}${zonePart}`
}

export function formatMonthDay(value: string | Date, timeZone?: string | null, includeZone = false): string {
  const resolved = resolveTimeZone({ fallbackTimeZone: timeZone ?? getActiveTimeZone() })
  const parts = getZonedParts(value, resolved)
  if (!parts) {
    return typeof value === 'string' ? value : String(value)
  }
  const dayPart = `${parts.month}-${parts.day}`
  if (!includeZone) return dayPart
  return `${dayPart} [${formatTimeZoneOffset(value, resolved)}]`
}

export function isSameCalendarDay(left: string | Date, right: string | Date, timeZone?: string | null): boolean {
  const resolved = resolveTimeZone({ fallbackTimeZone: timeZone ?? getActiveTimeZone() })
  const leftParts = getZonedParts(left, resolved)
  const rightParts = getZonedParts(right, resolved)
  if (!leftParts || !rightParts) return false
  return leftParts.year === rightParts.year
    && leftParts.month === rightParts.month
    && leftParts.day === rightParts.day
}
