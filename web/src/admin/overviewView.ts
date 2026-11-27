import type { AdminStats } from '../lib/types.gen'
import { rub } from './format'

const dayMs = 24 * 3600 * 1000

// range даёт даты «с» и «по» (включая обе) для быстрых периодов: сегодня, 7 дней, 30 дней.
export function range(days: number, now: Date = new Date()): { from: string; to: string } {
  const iso = (d: Date) =>
    `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  return { from: iso(new Date(now.getTime() - (days - 1) * dayMs)), to: iso(now) }
}

// overviewView это сводка для экрана: плитки, дни с долей от лучшего дня и зависшие задачи.
export function overviewView(s: AdminStats) {
  const best = Math.max(1, ...s.days.map((d) => Math.abs(d.revenue)))
  return {
    tiles: [
      { title: 'Выручка', value: rub(s.revenue) },
      { title: 'Новые абоненты', value: String(s.new_users) },
      { title: 'Пробные', value: String(s.trials) },
      { title: 'Первые оплаты', value: String(s.first_paid) },
      { title: 'Активные подписки', value: String(s.active_subs) },
    ],
    days: s.days.map((d) => ({
      day: d.day,
      revenue: rub(d.revenue),
      refunded: d.refunded > 0 ? `возвраты ${rub(d.refunded)}` : '',
      share: Math.round((Math.abs(d.revenue) / best) * 100),
      negative: d.revenue < 0,
    })),
    gateways: s.gateways.map((g) => ({ name: g.gateway, revenue: rub(g.revenue) })),
    stuck: s.stuck_tasks > 0 ? { count: s.stuck_tasks, users: s.stuck_users } : null,
  }
}
