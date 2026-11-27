import { describe, expect, it } from 'vitest'
import type { AdminBroadcast } from '../lib/types.gen'
import { golden } from '../lib/testing/golden'
import { bcView } from './broadcastView'

describe('рассылки на эталонах API', () => {
  it('черновик можно проверить на себе, запустить и отменить', () => {
    const v = bcView(golden<AdminBroadcast>('admin_broadcast_draft'))
    expect(v.status).toBe('черновик')
    expect([v.canTest, v.canStart, v.canCancel, v.running]).toEqual([true, true, true, false])
    expect(v.progress).toBe(0)
  })

  it('идущая показывает прогресс и отменяется', () => {
    const v = bcView(golden<AdminBroadcast>('admin_broadcast_running'))
    expect(v.status).toBe('идёт')
    expect(v.progress).toBe(83) // 25 из 30
    expect(v.counts).toBe('доставлено 25, не удалось 0, заблокировали бота 0 из 30')
    expect([v.canTest, v.canStart, v.canCancel, v.running]).toEqual([false, false, true, true])
  })

  it('законченную не тронуть, длинный текст обрезается', () => {
    const b = { ...golden<AdminBroadcast>('admin_broadcast_running'), status: 'done', text: 'я'.repeat(200) }
    const v = bcView(b)
    expect([v.canTest, v.canStart, v.canCancel]).toEqual([false, false, false])
    expect(v.text.length).toBe(81)
  })
})
