import type { AdminMe } from '../lib/types.gen'

// Раздел меню и право, которое его открывает. 'owner' только владельцу. Сайт лишь прячет разделы:
// каждый адрес всё равно проверяет сервер.
export interface Section {
  id: string
  title: string
  perm: string
}

export const sections: Section[] = [
  { id: 'overview', title: 'Сводка', perm: 'stats' },
  { id: 'users', title: 'Абоненты', perm: 'users' },
  { id: 'payments', title: 'Платежи', perm: 'stats' },
  { id: 'tariffs', title: 'Тарифы', perm: 'tariffs' },
  { id: 'promos', title: 'Промокоды', perm: 'promo' },
  { id: 'broadcasts', title: 'Рассылки', perm: 'broadcast' },
]

export function visible(me: AdminMe): Section[] {
  return sections.filter((s) => me.owner || (s.perm !== 'owner' && me.perms.includes(s.perm)))
}
