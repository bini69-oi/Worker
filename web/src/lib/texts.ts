// Тексты страницы приходят с сервера (GET /api/app/texts). В коде страницы строк для абонента нет:
// есть ключи, подстановки вида {days} и форматы чисел, дат и денег под язык абонента.
import { derived, writable, type Readable } from 'svelte/store'
import type { Api } from './api'
import type { TextsResponse } from './types.gen'

export type Vars = Record<string, string | number>
export type Translate = (key: string, vars?: Vars) => string

/** Подставляет {имя} в шаблон. Подстановка, которой нет в vars, остаётся как есть. */
export function format(template: string, vars?: Vars): string {
  if (!vars) return template
  return template.replace(/\{([A-Za-z0-9_]+)\}/g, (whole, name: string) => (name in vars ? String(vars[name]) : whole))
}

/** Хранилище текстов. Неизвестный ключ показывается самим ключом: так пропуск виден на экране, а не пустое место. */
export function createTexts() {
  const dict = writable<Record<string, string>>({})
  const lang = writable('ru')
  const t: Readable<Translate> = derived(dict, ($d): Translate => (key: string, vars?: Vars) => format($d[key] ?? key, vars))
  return {
    t,
    lang,
    set(r: TextsResponse) {
      dict.set(r.texts)
      lang.set(r.lang)
    },
  }
}

/** Загружает тексты. Язык берёт сервер у абонента, без сессии из параметра или заголовка браузера. */
export async function loadTexts(api: Api, lang?: string): Promise<TextsResponse> {
  return api.get<TextsResponse>(lang ? `/texts?lang=${encodeURIComponent(lang)}` : '/texts')
}

/** Копейки в рубли с разрядами по языку: 25000 -> «250», 25050 -> «250,50». Знак валюты стоит в тексте app.money. */
export function amount(kop: number, lang: string): string {
  const rub = kop / 100
  return new Intl.NumberFormat(lang, { minimumFractionDigits: kop % 100 === 0 ? 0 : 2, maximumFractionDigits: 2 }).format(rub)
}

/** Сумма с валютой из текста app.money. */
export function money(t: Translate, kop: number, lang: string): string {
  return t('app.money', { amount: amount(kop, lang) })
}

/** Дата по языку абонента, например «09.04.2026». */
export function date(iso: string, lang: string): string {
  return new Intl.DateTimeFormat(lang, { day: '2-digit', month: '2-digit', year: 'numeric' }).format(new Date(iso))
}

/** Дата и время по языку абонента. */
export function dateTime(iso: string, lang: string): string {
  return new Intl.DateTimeFormat(lang, { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(iso))
}

/** Гигабайты из байтов: 0 это «без лимита» (текст app.unlimited). */
export function traffic(t: Translate, bytes: number): string {
  if (bytes <= 0) return t('app.unlimited')
  return t('app.gb', { n: Math.round((bytes / 2 ** 30) * 10) / 10 })
}

/** Число устройств: 0 это «без лимита». */
export function devices(t: Translate, n: number): string {
  return n <= 0 ? t('app.unlimited') : String(n)
}
