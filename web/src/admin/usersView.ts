import type { AdminUserCard, AdminUserRow } from '../lib/types.gen'
import { dt, gib, rub } from './format'

export const statusTitle: Record<string, string> = {
  none: 'нет подписки',
  trial: 'пробный',
  active: 'есть подписка',
  expired: 'истекла',
}

export const filters: { id: string; title: string }[] = [
  { id: '', title: 'все' },
  { id: 'active', title: 'с подпиской' },
  { id: 'trial', title: 'пробный' },
  { id: 'expired', title: 'истекла' },
  { id: 'none', title: 'без подписки' },
  { id: 'banned', title: 'забанен' },
]

// rowView это строка поиска в виде, готовом для экрана.
export function rowView(r: AdminUserRow) {
  return {
    id: r.id,
    name: r.username ? '@' + r.username : 'id' + r.tg_id,
    tg: String(r.tg_id),
    status: r.banned ? 'забанен' : (statusTitle[r.status] ?? r.status),
    until: dt(r.expires_at),
    balance: rub(r.balance),
  }
}

// cardView это сводка карточки: подписка, лимиты, пометки.
export function cardView(c: AdminUserCard) {
  const s = c.sub
  return {
    title: c.username ? '@' + c.username : 'id' + c.tg_id,
    balance: rub(c.balance),
    sub: s
      ? {
          tariff: s.trial ? 'пробный период' : s.tariff || 'без тарифа',
          until: dt(s.expires_at),
          traffic: gib(s.traffic_bytes),
          devices: s.device_limit > 0 ? String(s.device_limit) : 'без лимита',
          autoRenew: s.auto_renew ? 'включено' : 'выключено',
          // Ждущая смена тарифа, докупленное и пометка «перенесён» из панели.
          queued: s.next_tariff ? `${s.next_tariff}, ${s.next_days} дн., ${rub(s.next_price)} уже списано` : '',
          extras: [
            s.extra_traffic_bytes > 0 ? `трафик +${gib(s.extra_traffic_bytes)} до ${dt(s.extra_traffic_until)}` : '',
            s.extra_devices > 0 ? `устройства +${s.extra_devices}` : '',
          ].filter(Boolean),
          imported: s.imported
            ? `перенесён из панели${s.own?.username ? ` (${s.own.username})` : ''}: ${gib(s.own?.traffic_bytes ?? 0)}, устройств ${s.own?.devices ? s.own.devices : 'без лимита'}`
            : '',
        }
      : null,
    flags: [c.banned ? 'забанен' : '', c.unreachable ? 'заблокировал бота' : ''].filter(Boolean),
    devices: c.devices_ok ? c.devices.map((d) => `${d.platform} ${d.model}`.trim() || d.hwid) : null,
    devicesNote: c.devices_ok ? '' : 'Панель не ответила',
  }
}
