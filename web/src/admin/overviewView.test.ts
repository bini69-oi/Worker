import { describe, expect, it } from 'vitest'
import type { AdminStats } from '../lib/types.gen'
import { golden } from '../lib/testing/golden'
import { overviewView, range } from './overviewView'

describe('сводка на эталоне API', () => {
  const s = golden<AdminStats>('admin_stats')

  it('плитки и день', () => {
    const v = overviewView(s)
    expect(v.tiles.map((t) => t.title)).toEqual(['Выручка', 'Новые абоненты', 'Пробные', 'Первые оплаты', 'Активные подписки'])
    expect(v.tiles[0]?.value).toBe('1 000,00 ₽')
    expect(v.tiles[1]?.value).toBe('2')
    expect(v.days[0]).toMatchObject({ day: '2026-01-01', share: 100, negative: false, refunded: '' })
    expect(v.gateways).toEqual([{ name: 'manual', revenue: '1 000,00 ₽' }])
    expect(v.stuck).toBeNull()
  })

  it('зависшие задачи видны со ссылками на абонентов', () => {
    const v = overviewView({ ...s, stuck_tasks: 3, stuck_users: [7, 9] })
    expect(v.stuck).toEqual({ count: 3, users: [7, 9] })
  })

  it('возврат в день: доля и пометка', () => {
    const v = overviewView({ ...s, days: [{ day: '2026-01-02', gross: 0, refunded: 5000, revenue: -5000 }] })
    expect(v.days[0]).toMatchObject({ negative: true, refunded: 'возвраты 50,00 ₽', share: 100 })
  })

  it('быстрые периоды', () => {
    const now = new Date(2026, 0, 31, 12)
    expect(range(1, now)).toEqual({ from: '2026-01-31', to: '2026-01-31' })
    expect(range(7, now)).toEqual({ from: '2026-01-25', to: '2026-01-31' })
  })
})
