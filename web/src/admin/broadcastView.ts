import type { AdminBroadcast } from '../lib/types.gen'
import { dt } from './format'

export const segments = [
  { id: 'all', title: 'все' },
  { id: 'active', title: 'с подпиской' },
  { id: 'none', title: 'без подписки' },
  { id: 'expired30', title: 'истекла за 30 дней' },
  { id: 'trial', title: 'на пробном' },
]

export const statusTitle: Record<string, string> = {
  draft: 'черновик',
  running: 'идёт',
  done: 'готово',
  cancelled: 'отменена',
}

// bcView это рассылка для экрана: прогресс в процентах и что с ней можно сделать.
export function bcView(b: AdminBroadcast) {
  const handled = b.sent + b.failed + b.blocked
  return {
    id: b.id,
    segment: segments.find((s) => s.id === b.segment)?.title ?? b.segment,
    status: statusTitle[b.status] ?? b.status,
    text: b.text.length > 80 ? b.text.slice(0, 80) + '…' : b.text,
    progress: b.total > 0 ? Math.min(100, Math.round((handled / b.total) * 100)) : 0,
    counts: `доставлено ${b.sent}, не удалось ${b.failed}, заблокировали бота ${b.blocked} из ${b.total}`,
    created: dt(b.created_at),
    canStart: b.status === 'draft',
    canTest: b.status === 'draft',
    canCancel: b.status === 'draft' || b.status === 'running',
    running: b.status === 'running',
  }
}
