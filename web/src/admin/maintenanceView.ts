import type { AdminMaintenance } from '../lib/types.gen'

// maintenanceView это блок техработ: статус, что делает кнопка и что увидят абоненты.
export function maintenanceView(m: AdminMaintenance) {
  return {
    status: m.on ? 'Техработы идут: абоненты видят текст о работах, админы пользуются ботом' : 'Бот работает в обычном режиме',
    button: m.on ? 'Выключить техработы' : 'Включить техработы',
    next: !m.on,
    text: m.text,
  }
}
