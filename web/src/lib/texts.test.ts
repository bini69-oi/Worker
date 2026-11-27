import { get } from 'svelte/store'
import { describe, expect, it, vi } from 'vitest'
import { createApi } from './api'
import { amount, createTexts, date, devices, format, loadTexts, money, traffic } from './texts'
import { followSystemTheme } from './theme'
import { golden } from './testing/golden'
import type { TextsResponse } from './types.gen'

describe('format', () => {
  it('подставляет значения', () => {
    expect(format('Осталось {days} дн.', { days: 3 })).toBe('Осталось 3 дн.')
    expect(format('{a} и {b}', { a: 'x', b: 2 })).toBe('x и 2')
  })
  it('чего нет в vars, остаётся как есть', () => {
    expect(format('до {date}', { days: 1 })).toBe('до {date}')
    expect(format('до {date}')).toBe('до {date}')
  })
  it('подставленное повторно не разбирается', () => {
    expect(format('{a}', { a: '{b}', b: 'x' })).toBe('{b}')
  })
})

describe('createTexts', () => {
  it('отдаёт текст по ключу, пропуск виден ключом', () => {
    const tx = createTexts()
    tx.set({ lang: 'ru', texts: { 'app.hello': 'Привет, {name}' } })
    const t = get(tx.t)
    expect(t('app.hello', { name: 'Иван' })).toBe('Привет, Иван')
    expect(t('app.missing')).toBe('app.missing')
    expect(get(tx.lang)).toBe('ru')
  })

  it('читает английский эталон ответа сервера', () => {
    const tx = createTexts()
    tx.set(golden<TextsResponse>('texts_en'))
    expect(get(tx.lang)).toBe('en')
    expect(get(tx.t)('app.hello')).toBe('en:app.hello')
  })

  it('обновление текстов доходит до подписчиков', () => {
    const tx = createTexts()
    const seen: string[] = []
    tx.t.subscribe((t) => seen.push(t('app.x')))
    tx.set({ lang: 'ru', texts: { 'app.x': 'один' } })
    tx.set({ lang: 'ru', texts: { 'app.x': 'два' } })
    expect(seen).toEqual(['app.x', 'один', 'два'])
  })
})

describe('loadTexts', () => {
  it('просит язык у сервера, если он не задан, и по параметру, если задан', async () => {
    const f = vi.fn(async () => new Response(JSON.stringify(golden('texts_en'))))
    const api = createApi({ base: '/api/app', fetch: f as unknown as typeof fetch })
    await loadTexts(api)
    await loadTexts(api, 'en')
    expect(f.mock.calls.map((c) => (c as unknown as [string])[0])).toEqual(['/api/app/texts', '/api/app/texts?lang=en'])
  })
})

describe('форматы', () => {
  const t = (key: string, vars?: Record<string, string | number>) => format({ 'app.money': '{amount} ₽', 'app.unlimited': 'без лимита', 'app.gb': '{n} ГБ' }[key] ?? key, vars)
  it('деньги из копеек', () => {
    expect(amount(25000, 'en')).toBe('250')
    expect(amount(25050, 'en')).toBe('250.50')
    expect(amount(123456700, 'en')).toBe('1,234,567')
    expect(money(t, 25000, 'en')).toBe('250 ₽')
  })
  it('дата по языку', () => {
    expect(date('2026-04-09T12:00:00Z', 'ru')).toBe('09.04.2026')
  })
  it('трафик и устройства: ноль это без лимита', () => {
    expect(traffic(t, 0)).toBe('без лимита')
    expect(traffic(t, 100 * 2 ** 30)).toBe('100 ГБ')
    expect(devices(t, 0)).toBe('без лимита')
    expect(devices(t, 3)).toBe('3')
  })
})

describe('тема браузера', () => {
  function fakeWin(dark: boolean) {
    let fn: () => void = () => {}
    const mq = {
      matches: dark,
      addEventListener: (_: string, f: () => void) => (fn = f),
      removeEventListener: vi.fn(),
    }
    return { win: { matchMedia: () => mq } as unknown as Window, mq, change: (v: boolean) => ((mq.matches = v), fn()) }
  }

  it('берёт настройку системы и следит за ней', () => {
    const root = document.createElement('html')
    const w = fakeWin(true)
    const stop = followSystemTheme(w.win, root)
    expect(root.dataset.theme).toBe('dark')
    w.change(false)
    expect(root.dataset.theme).toBe('light')
    stop()
    expect(w.mq.removeEventListener).toHaveBeenCalled()
  })
})
