import type { AdminNotify } from '../lib/types.gen'

// notifyView это раздел уведомлений: привязана ли группа и что можно переключать.
// Тема «Безопасность» всегда включена и выключателя не имеет.
export function notifyView(n: AdminNotify) {
  return {
    bound: n.bound,
    state: n.bound ? 'Группа привязана' : 'Группа не привязана',
    topics: n.topics.map((t) => ({ id: t.id, title: t.title, enabled: t.enabled, canToggle: !t.locked })),
  }
}

// bindHint это что сделать с кодом: бот привязывает группу по команде /bind.
export function bindHint(code: string): string {
  return `Добавьте бота в группу-форум и отправьте там: /bind ${code}`
}
