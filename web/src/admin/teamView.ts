import type { AdminAudit, AdminMember, AdminSession } from '../lib/types.gen'
import { dt } from './format'

export const permTitles: Record<string, string> = {
  users: 'Абоненты: поиск и карточка',
  grant: 'Деньги и дни, ручные платежи',
  ban: 'Бан и разбан',
  promo: 'Промокоды',
  tariffs: 'Тарифы',
  broadcast: 'Рассылки',
  stats: 'Сводка и платежи',
  support: 'Сообщения абоненту',
}

export const actionTitles: Record<string, string> = {
  login: 'вход в админку',
  'session.close': 'закрыта сессия',
  'sub.grant_days': 'выданы дни',
  'balance.adjust': 'правка баланса',
  'user.ban': 'бан',
  'user.unban': 'разбан',
  'user.message': 'сообщение абоненту',
  'payment.confirm': 'подтверждён платёж',
  'payment.refund': 'возврат платежа',
  'tariff.save': 'тариф сохранён',
  'promo.save': 'промокод сохранён',
  'broadcast.create': 'рассылка создана',
  'broadcast.start': 'рассылка запущена',
  'broadcast.cancel': 'рассылка отменена',
  'text.set': 'текст изменён',
  'text.reset': 'текст сброшен',
  'setting.set': 'настройка изменена',
  'maintenance.on': 'техработы включены',
  'maintenance.off': 'техработы выключены',
  'import.panel': 'импорт из панели',
  'devices.reset': 'сброшены устройства',
  'link.revoke': 'перевыпущена ссылка',
}

// actionTitle называет действие журнала по-человечески. Служебные префиксы (запрос, подтверждение, отказ)
// раскрываются, неизвестное остаётся как есть.
export function actionTitle(action: string): string {
  const known = actionTitles[action]
  if (known) return known
  for (const [prefix, word] of [['denied:', 'отказ по праву'], ['request:', 'запрос на «Да»'], ['approve:', 'подтверждено «Да»'], ['reject:', 'отклонено «Нет»']] as const) {
    if (action.startsWith(prefix)) return `${word}: ${actionTitle(action.slice(prefix.length))}`
  }
  return action
}

export function memberView(m: AdminMember) {
  return {
    tg: m.tg_id,
    role: m.owner ? 'владелец' : m.disabled ? 'отключён' : 'помощник',
    perms: m.owner ? 'все права' : m.perms.map((p) => permTitles[p] ?? p).join(', ') || 'нет прав',
    canRemove: !m.owner && !m.disabled,
  }
}

export function sessionView(s: AdminSession) {
  return { id: s.id, admin: s.admin_tg, ip: s.ip, agent: s.agent.slice(0, 60), seen: dt(s.seen_at), current: s.current }
}

export function auditView(a: AdminAudit) {
  return {
    id: a.id,
    when: dt(a.at),
    admin: a.admin_tg,
    action: actionTitle(a.action),
    target: a.target || '—',
    details: Object.entries(a.details)
      .map(([k, v]) => `${k}: ${v}`)
      .join('; '),
    ip: a.ip || '—',
  }
}
