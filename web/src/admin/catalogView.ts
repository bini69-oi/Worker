import type { AdminPromo, AdminTariff } from '../lib/types.gen'
import { dt, parseRub, rub } from './format'

const GIB = 2 ** 30

export const resets = [
  { id: 'NO_RESET', title: 'не сбрасывать' },
  { id: 'DAY', title: 'раз в день' },
  { id: 'WEEK', title: 'раз в неделю' },
  { id: 'MONTH', title: 'раз в месяц' },
]

// TariffForm это тариф в полях формы: трафик в гигабайтах, цены в рублях строкой.
export interface TariffForm {
  id: number
  name: string
  trafficGb: string
  devices: string
  reset: string
  sort: string
  active: boolean
  squads: string[]
  periods: { days: string; price: string }[]
}

export function emptyTariff(): TariffForm {
  return { id: 0, name: '', trafficGb: '0', devices: '0', reset: 'NO_RESET', sort: '0', active: true, squads: [],
    periods: [{ days: '30', price: '' }] }
}

export function tariffToForm(t: AdminTariff): TariffForm {
  return {
    id: t.id,
    name: t.name,
    trafficGb: String(Math.round((t.traffic_bytes / GIB) * 100) / 100),
    devices: String(t.devices),
    reset: t.reset,
    sort: String(t.sort),
    active: t.active,
    squads: [...t.squads],
    periods: t.periods.map((p) => ({ days: String(p.days), price: rub(p.price).replace(/[^\d,−-]/g, '').replace('−', '-') })),
  }
}

const int = (s: string) => (/^\d{1,9}$/.test(s.trim()) ? Number(s) : null)

// formToTariff собирает тело запроса или говорит, что не так. Сервер проверит всё ещё раз.
export function formToTariff(f: TariffForm): { ok: AdminTariff } | { error: string } {
  const gb = parseRub(f.trafficGb) // тот же разбор «число с запятой»
  const devices = int(f.devices)
  const sort = int(f.sort)
  if (!f.name.trim()) return { error: 'Укажите название' }
  if (gb === null || gb < 0) return { error: 'Трафик: число гигабайт, 0 без лимита' }
  if (devices === null) return { error: 'Устройства: целое число, 0 без лимита' }
  if (sort === null) return { error: 'Порядок: целое число' }
  if (f.periods.length === 0) return { error: 'Добавьте хотя бы один срок' }
  const periods: { days: number; price: number }[] = []
  for (const p of f.periods) {
    const days = int(p.days)
    const price = parseRub(p.price)
    if (days === null || days <= 0 || price === null || price < 0) return { error: 'Срок: дни целым числом и цена в рублях' }
    periods.push({ days, price })
  }
  return {
    ok: { id: f.id, name: f.name.trim(), traffic_bytes: Math.round((gb / 100) * GIB), devices, reset: f.reset, squads: f.squads,
      active: f.active, sort, periods },
  }
}

// squadChoices это сквады панели с галочками плюс те, что стоят в тарифе, но панель их не вернула.
export function squadChoices(all: { uuid: string; name: string }[], chosen: string[]) {
  const known = new Set(all.map((s) => s.uuid))
  const extra = chosen.filter((u) => !known.has(u)).map((u) => ({ uuid: u, name: u + ' (нет в панели)' }))
  return [...all, ...extra].map((s) => ({ ...s, on: chosen.includes(s.uuid) }))
}

export function promoView(p: AdminPromo) {
  return {
    id: p.id,
    code: p.code,
    gives: p.kind === 'days' ? `${p.value} дн.` : rub(p.value),
    uses: p.max_uses > 0 ? `${p.uses} из ${p.max_uses}` : String(p.uses),
    until: p.expires_at ? dt(p.expires_at) : 'без срока',
    active: p.active,
    link: p.link,
  }
}
