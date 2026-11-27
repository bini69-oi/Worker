// Деньги приходят копейками. На экране рубли с копейками.
export function rub(kop: number): string {
  const sign = kop < 0 ? '−' : ''
  const abs = Math.abs(kop)
  const r = Math.floor(abs / 100)
  const k = String(abs % 100).padStart(2, '0')
  return `${sign}${r.toLocaleString('ru-RU')},${k} ₽`
}

// parseRub разбирает «100», «100.5», «-20,25» в копейки. Всё остальное это null: ввод проверяется строго.
export function parseRub(s: string): number | null {
  const m = /^(-?)(\d{1,9})(?:[.,](\d{1,2}))?$/.exec(s.trim())
  if (!m) return null
  const kop = Number(m[2]) * 100 + Number((m[3] ?? '').padEnd(2, '0'))
  return m[1] ? -kop : kop
}

export function dt(iso: string | null | undefined): string {
  if (!iso) return '—'
  return new Date(iso).toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' })
}

export function gib(bytes: number): string {
  return bytes <= 0 ? 'без лимита' : `${Math.round(bytes / 2 ** 30)} ГБ`
}
