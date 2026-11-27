import type { AdminPayment } from '../lib/types.gen'
import { dt, rub } from './format'

export const payStatus: Record<string, string> = {
  pending: 'ожидает',
  paid: 'оплачен',
  expired: 'истёк',
  refunded: 'возвращён',
  failed: 'ошибка',
}

export const gateways = [
  { id: '', title: 'все кассы' },
  { id: 'manual', title: 'ручное зачисление' },
  { id: 'stars', title: 'Stars' },
]

// payView это строка платежа для экрана. Кнопки зависят от кассы и статуса: ручной ждущий подтверждается,
// оплаченный Stars возвращается (только владельцем, сервер всё равно проверит).
export function payView(p: AdminPayment, owner: boolean) {
  return {
    id: p.id,
    userId: p.user_id,
    user: p.username ? '@' + p.username : 'абонент ' + p.user_id,
    gateway: p.gateway,
    amount: rub(p.amount),
    status: payStatus[p.status] ?? p.status,
    created: dt(p.created_at),
    paid: dt(p.paid_at),
    canConfirm: p.gateway === 'manual' && p.status === 'pending',
    canRefund: owner && p.gateway === 'stars' && p.status === 'paid',
  }
}
