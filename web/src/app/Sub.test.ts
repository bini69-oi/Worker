import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../lib/api'
import { golden } from '../lib/testing/golden'
import { fakeApp, flushSync, render } from '../lib/testing/mountTexts'
import type { ErrorBody, MeResponse, SubView } from '../lib/types.gen'
import Sub from './Sub.svelte'

const dict = {
  'app.sub_next': 'С {date} тариф {tariff}',
  'app.sub_cancel': 'Отменить',
  'app.sub_cancel_confirm': 'Точно отменить?',
  'app.sub_add_traffic': '+{gb} ГБ',
  'app.sub_add_device': '+1 устройство',
  'app.sub_autorenew_on': 'Включить автопродление',
  'app.sub_autorenew_off': 'Выключить автопродление',
  'app.buy_topup_diff': 'Пополнить на {amount}',
  'app.home_status_active': 'Активна',
  'app.home_status_trial': 'Пробная',
  'app.sub_trial': 'Пробный период',
  'app.logout': 'Выйти',
  'app.money': '{amount} ₽',
  'app.gb': '{n} ГБ',
}

let stop: (() => void) | undefined
afterEach(() => {
  stop?.()
  window.location.hash = ''
})

async function show(me: MeResponse, ep: Record<string, unknown> = {}, over: Record<string, unknown> = {}) {
  const f = fakeApp(me, ep as never, over)
  await f.loader.reload()
  const r = render(Sub, {}, dict, f.extra)
  stop = r.unmount
  await new Promise((res) => setTimeout(res, 0))
  flushSync()
  return { ...r, loader: f.loader }
}

const click = (root: HTMLElement, text: string) => {
  const b = [...root.querySelectorAll('button')].find((x) => x.textContent?.replace(/\s+/g, ' ').trim().startsWith(text))
  expect(b, `нет кнопки «${text}»`).toBeDefined()
  b?.click()
  flushSync()
}

describe('Sub: смена тарифа', () => {
  const queued = golden<MeResponse>('me_plan_queued')

  it('ждущая смена: «С 09.04.2026 тариф Год» и «Отменить»', async () => {
    const { root } = await show(queued)
    expect(root.textContent).toContain('С 09.04.2026 тариф Год')
    expect(root.textContent).toContain('Отменить')
  })

  it('отмена просит второе нажатие, потом зовёт API и обновляет данные', async () => {
    const cancelPlan = vi.fn(async () => golden<SubView>('trial'))
    const { root, loader } = await show(queued, { cancelPlan })
    const refresh = vi.spyOn(loader, 'refresh')
    click(root, 'Отменить')
    expect(cancelPlan).not.toHaveBeenCalled()
    click(root, 'Точно отменить?')
    await vi.waitFor(() => expect(cancelPlan).toHaveBeenCalledTimes(1))
    await vi.waitFor(() => expect(refresh).toHaveBeenCalled())
  })

  it('без ждущей смены блока нет', async () => {
    const { root } = await show(golden<MeResponse>('me_with_sub'))
    expect(root.textContent).not.toContain('С 09.04.2026 тариф')
  })
})

describe('Sub: докупка', () => {
  const offer = golden<MeResponse>('me_addons')

  it('пакеты трафика и устройство с ценами', async () => {
    const { root } = await show(offer)
    const text = root.textContent?.replace(/\s+/g, ' ') ?? ''
    expect(text).toContain('+10 ГБ')
    expect(text).toContain('150 ₽')
    expect(text).toContain('+50 ГБ')
    expect(text).toContain('+1 устройство')
    expect(text).toContain('100 ₽')
  })

  it('нажатие докупает выбранный пакет и обновляет данные', async () => {
    const buyTraffic = vi.fn(async () => golden<SubView>('trial'))
    const buyDevice = vi.fn(async () => golden<SubView>('trial'))
    const { root } = await show(offer, { buyTraffic, buyDevice })
    click(root, '+50 ГБ')
    await vi.waitFor(() => expect(buyTraffic).toHaveBeenCalledWith(50, expect.stringMatching(/^[A-Za-z0-9_-]{16,64}$/)))
    await new Promise((r) => setTimeout(r, 10))
    click(root, '+1 устройство')
    await vi.waitFor(() => expect(buyDevice).toHaveBeenCalledTimes(1))
  })

  it('не хватает денег: недостача и «Пополнить на разницу»', async () => {
    const e = golden<ErrorBody>('buy_need_money')
    const buyDevice = vi.fn(async () => Promise.reject(new ApiError(402, { ...e, money: { price: 10000, balance: 4000, missing: 6000 } })))
    const { root } = await show(offer, { buyDevice })
    click(root, '+1 устройство')
    await vi.waitFor(() => expect(root.textContent).toContain('Пополнить на 60 ₽'))
    click(root, 'Пополнить на 60 ₽')
    expect(window.location.hash).toBe('#/topup/6000')
  })

  it('двойной клик докупает один раз', async () => {
    let done: (v: SubView) => void = () => {}
    const buyDevice = vi.fn(() => new Promise<SubView>((r) => (done = r)))
    const { root } = await show(offer, { buyDevice })
    click(root, '+1 устройство')
    click(root, '+1 устройство')
    expect(buyDevice).toHaveBeenCalledTimes(1)
    done(golden<SubView>('trial'))
  })

  it('пробная подписка: докупки нет', async () => {
    const me = golden<MeResponse>('me_no_sub')
    const { root } = await show({ ...me, sub: golden<SubView>('trial') })
    expect(root.textContent).not.toContain('+1 устройство')
    expect(root.textContent).toContain('Пробный период')
  })
})

describe('Sub: автопродление и выход', () => {
  it('автопродление переключается', async () => {
    const autorenew = vi.fn(async () => golden<SubView>('trial'))
    const { root } = await show(golden<MeResponse>('me_with_sub'), { autorenew })
    click(root, 'Включить автопродление')
    await vi.waitFor(() => expect(autorenew).toHaveBeenCalledWith(true))
  })

  it('в браузере есть «Выйти», в Telegram нет', async () => {
    const logout = vi.fn(async () => {})
    const { root } = await show(golden<MeResponse>('me_with_sub'), {}, { session: { logout } })
    click(root, 'Выйти')
    expect(logout).toHaveBeenCalledWith(false)
    stop?.()
    const tg = { openTelegramLink: vi.fn() }
    const second = await show(golden<MeResponse>('me_with_sub'), {}, { tg, session: { logout } })
    expect(second.root.textContent).not.toContain('Выйти')
  })
})
