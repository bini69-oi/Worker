import { afterEach, describe, expect, it } from 'vitest'
import { golden } from '../lib/testing/golden'
import { fakeApp, flushSync, render } from '../lib/testing/mountTexts'
import type { MeResponse, TextsResponse } from '../lib/types.gen'
import Home from './Home.svelte'
import Sub from './Sub.svelte'

const en = golden<TextsResponse>('texts_en_app')
const ru = golden<TextsResponse>('texts_ru_app')
const cyrillic = /[Ѐ-ӿ]/

let stop: (() => void) | undefined
afterEach(() => stop?.())

async function show(comp: typeof Home | typeof Sub, texts: TextsResponse, me: MeResponse) {
  const f = fakeApp(me, {} as never, { session: { logout: async () => {} } as never })
  await f.loader.reload()
  const r = render(comp as never, {}, texts.texts, f.extra)
  stop = r.unmount
  await new Promise((res) => setTimeout(res, 0))
  flushSync()
  return r.root
}

describe('язык абонента', () => {
  it('сервер отдал английский: язык en и тексты на английском', () => {
    expect(en.lang).toBe('en')
    expect(ru.lang).toBe('ru')
    expect(en.texts['app.home_connect']).toBe('Connect')
    expect(ru.texts['app.home_connect']).toBe('Подключить')
    // Каждый ключ есть на обоих языках.
    expect(Object.keys(en.texts).sort()).toEqual(Object.keys(ru.texts).sort())
  })

  it('главная англоязычного абонента: ни одной русской строки', async () => {
    const me = golden<MeResponse>('me_en')
    expect(me.lang).toBe('en')
    const root = await show(Home, en, me)
    expect(root.textContent).toContain('Try for free')
    expect(root.textContent).toContain('No subscription')
    expect(cyrillic.test(root.textContent ?? '')).toBe(false)
  })

  it('подписка англоязычного абонента: даты и деньги тоже по-английски', async () => {
    const me = { ...golden<MeResponse>('me_with_sub'), lang: 'en' }
    const root = await show(Sub, en, me)
    expect(root.textContent).toContain('Valid until')
    expect(root.textContent).toContain('04/09/2026')
    expect(root.textContent).toContain('100 GB')
    expect(cyrillic.test((root.textContent ?? '').replace('Месяц', ''))).toBe(false) // название тарифа задаёт админ
  })

  it('тот же экран на русском остаётся русским', async () => {
    const root = await show(Home, ru, golden<MeResponse>('me_with_sub'))
    expect(root.textContent).toContain('Подключить')
    expect(root.textContent).toContain('09.04.2026')
  })
})
