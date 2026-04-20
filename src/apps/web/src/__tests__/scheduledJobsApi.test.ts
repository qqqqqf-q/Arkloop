import { describe, expect, it } from 'vitest'
import {
  buildScheduledJobRequest,
  formatScheduledJobFireAtForInput,
  normalizeScheduledJobFireAtInput,
  type ScheduledJobFormValues,
} from '../pages/scheduled-jobs/api'

function makeFormValues(
  overrides: Partial<ScheduledJobFormValues> = {},
): ScheduledJobFormValues {
  return {
    name: '  Daily sync  ',
    description: 'desc',
    persona_key: '  assistant  ',
    prompt: '  run task  ',
    model: 'openai^gpt-5',
    work_dir: '/tmp/work',
    thread_id: '',
    schedule_kind: 'interval',
    interval_min: 60,
    daily_time: '09:00',
    monthly_day: 3,
    monthly_time: '10:00',
    weekly_day: 2,
    fire_at: '',
    cron_expr: '0 9 * * *',
    delete_after_run: false,
    timezone: 'UTC',
    reasoning_mode: '',
    timeout: 0,
    ...overrides,
  }
}

describe('scheduled jobs api helpers', () => {
  it('会在 datetime-local 和 RFC3339 之间稳定往返 fire_at', () => {
    const fireAt = '2026-04-20T09:30:00Z'

    const inputValue = formatScheduledJobFireAtForInput(fireAt)

    expect(inputValue).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/)
    expect(normalizeScheduledJobFireAtInput(inputValue)).toBe(fireAt)
  })

  it('创建 at 任务时会把本地时间转换成 RFC3339', () => {
    const expectedFireAt = new Date('2026-04-20T09:30').toISOString().replace('.000Z', 'Z')
    const request = buildScheduledJobRequest(
      makeFormValues({
        schedule_kind: 'at',
        fire_at: '2026-04-20T09:30',
      }),
      'create',
    )

    expect(request.fire_at).toBe(expectedFireAt)
    expect(request.thread_id).toBeUndefined()
    expect(request.reasoning_mode).toBeUndefined()
    expect(request.timeout).toBeUndefined()
  })

  it('编辑时会显式发送清空语义并清掉失活的 schedule 字段', () => {
    const request = buildScheduledJobRequest(
      makeFormValues({
        thread_id: '',
        schedule_kind: 'interval',
        fire_at: '2026-04-20T09:30',
        cron_expr: '0 9 * * *',
        reasoning_mode: '',
        timeout: 0,
      }),
      'update',
    )

    expect(request.thread_id).toBeNull()
    expect(request.reasoning_mode).toBe('')
    expect(request.timeout).toBe(0)
    expect(request.interval_min).toBe(60)
    expect(request.daily_time).toBe('')
    expect(request.monthly_day).toBeNull()
    expect(request.monthly_time).toBe('')
    expect(request.weekly_day).toBeNull()
    expect(request.fire_at).toBeNull()
    expect(request.cron_expr).toBe('')
    expect(request.delete_after_run).toBe(false)
  })
})
