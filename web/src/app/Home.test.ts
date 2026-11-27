import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../lib/api'
import { flushSync, fakeApp, render } from '../lib/testing/mountTexts'
import { golden } from '../lib/testing/golden'
import type { ErrorBody, MeResponse, SubView } from '../lib/types.gen'
import Home from './Home.svelte'

const dict = {
  'app.home_status_active': 'Активна',
  'app.home_status_trial': 'Пробная',
  'app.home_status_expired': 'Истекла',
  'app.home_status_none': 'Нет подписки',
  'app.home_until': 'до {date}',
  'app.days_unit': 'дн.',
  'app.home_connect': 'Подключить',
  'app.home_trial': 'Попробовать бесплатно',
  'app.home_buy': 'Купить',
  'app.home_renew': 'Продлить',
  'app.home_balance': 'Баланс',
  'app.money': '{amount} ₽',
  'app.tile_support': 'Поддержка',
}

let stop: (() => void) | undefined
afterEach(() => {
  stop?.()
  window.location.hash = ''
})

async function show(me: MeResponse, ep: Record<string, unknown> = {}) {
  const f = fakeApp(me, ep as never)
  await f.loader.reload()
  const r = render(Home, {}, dict, f.extra)
  stop = r.unmount
  return { ...r, ...f }
}

const buttons = (root: HTMLElement) => [...root.querySelectorAll('button')].map((b) => b.textContent?.trim() ?? '')

describe('Home', () => {
  it('активная подписка: статус, дни, дата и «Подключить»', async () => {
    const { root } = await show(golden<MeResponse>('me_with_sub'))
    expect(root.textContent).toContain('Активна')
    expect(root.querySelector('.days b')?.textContent).toBe('28')
    expect(root.textContent).toContain('до 09.04.2026 · Месяц')
    expect(root.textContent).toContain('750 ₽')
    expect(buttons(root)).toContain('Подключить')
    expect(buttons(root)).not.toContain('Попробовать бесплатно')
  })

  it('«Подключить» ведёт на подключение', async () => {
    const { root, router } = await show(golden<MeResponse>('me_with_sub'))
    ;[...root.querySelectorAll('button')].find((b) => b.textContent === 'Подключить')?.click()
    flushSync()
    expect(window.location.hash).toBe('#/connect')
    expect(router.canBack()).toBe(true)
  })

  it('нет подписки, пробный доступен: «Попробовать бесплатно» и «Купить»', async () => {
    const { root } = await show(golden<MeResponse>('me_no_sub'))
    expect(root.textContent).toContain('Нет подписки')
    const b = buttons(root)
    expect(b).toContain('Попробовать бесплатно')
    expect(b).toContain('Купить')
    expect(b).not.toContain('Подключить')
    expect(root.querySelector('.days')).toBeNull()
  })

  it('нет подписки и пробный уже был: только «Купить»', async () => {
    const me = { ...golden<MeResponse>('me_no_sub'), trial_available: false }
    const { root } = await show(me)
    const b = buttons(root)
    expect(b).toContain('Купить')
    expect(b).not.toContain('Попробовать бесплатно')
  })

  it('истёкшая подписка: «Продлить», дней нет', async () => {
    const base = golden<MeResponse>('me_with_sub')
    const sub = { ...(base.sub as SubView), state: 'expired', days_left: 0 }
    const { root } = await show({ ...base, sub })
    expect(root.textContent).toContain('Истекла')
    expect(buttons(root)).toContain('Продлить')
    expect(buttons(root)).not.toContain('Подключить')
    expect(root.querySelector('.days')).toBeNull()
  })

  it('пробный: вызывает API, обновляет главную и ведёт на подключение', async () => {
    const trial = vi.fn(async () => golden<SubView>('trial'))
    const { root, loader } = await show(golden<MeResponse>('me_no_sub'), { trial })
    const refresh = vi.spyOn(loader, 'refresh')
    ;[...root.querySelectorAll('button')].find((b) => b.textContent === 'Попробовать бесплатно')?.click()
    await vi.waitFor(() => expect(window.location.hash).toBe('#/connect'))
    expect(trial).toHaveBeenCalledTimes(1)
    expect(refresh).toHaveBeenCalled()
  })

  it('пробный не выдан: текст ошибки с кодом, на подключение не переходит', async () => {
    const e = golden<ErrorBody>('error_not_found')
    const trial = vi.fn(async () => Promise.reject(new ApiError(409, { ...e, code: 'E3005', text: 'Пробный период уже был' })))
    const { root } = await show(golden<MeResponse>('me_no_sub'), { trial })
    ;[...root.querySelectorAll('button')].find((b) => b.textContent === 'Попробовать бесплатно')?.click()
    await vi.waitFor(() => expect(root.querySelector('[role=alert]')?.textContent).toContain('E3005'))
    expect(window.location.hash).not.toBe('#/connect')
  })

  it('поддержка открывает чат бота', async () => {
    const open = vi.fn()
    const f = fakeApp(golden<MeResponse>('me_with_sub'), {}, { win: { open } as unknown as Window })
    await f.loader.reload()
    const r = render(Home, {}, dict, f.extra)
    stop = r.unmount
    ;[...r.root.querySelectorAll('button')].find((b) => b.textContent?.trim() === 'Поддержка')?.click()
    expect(open).toHaveBeenCalledWith('https://t.me/worker_bot', '_blank', 'noopener,noreferrer')
  })
})
