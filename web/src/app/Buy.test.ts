import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, NetworkError } from '../lib/api'
import { golden } from '../lib/testing/golden'
import { fakeApp, flushSync, render } from '../lib/testing/mountTexts'
import type { BuyResponse, ErrorBody, MeResponse, QuoteResponse, TariffsResponse } from '../lib/types.gen'
import Buy from './Buy.svelte'

const dict = {
  'app.buy_title': 'Тариф',
  'app.buy_limits': 'Трафик: {traffic}. Устройства: {devices}',
  'app.buy_from': 'от {price}',
  'app.buy_days': '{days} дн.',
  'app.buy_price': 'Цена',
  'app.buy_balance': 'На балансе',
  'app.buy_missing': 'Не хватает',
  'app.buy_pay': 'Оплатить с баланса',
  'app.buy_topup_diff': 'Пополнить на {amount}',
  'app.buy_queued': 'Включится {date}',
  'app.money': '{amount} ₽',
  'app.gb': '{n} ГБ',
}

const tariffs = golden<TariffsResponse>('tariffs')
let stop: (() => void) | undefined
afterEach(() => {
  stop?.()
  window.location.hash = ''
})

async function show(params: Record<string, string>, ep: Record<string, unknown>) {
  const f = fakeApp(golden<MeResponse>('me_no_sub'), { tariffs: vi.fn(async () => tariffs), ...ep } as never)
  await f.loader.reload()
  const r = render(Buy, { params }, dict, f.extra)
  stop = r.unmount
  await vi.waitFor(() => expect(r.root.querySelector('.card, .state')).not.toBeNull())
  await new Promise((res) => setTimeout(res, 0))
  flushSync()
  return r
}

const click = (root: HTMLElement, text: string) => {
  const b = [...root.querySelectorAll('button')].find((x) => x.textContent?.replace(/\s+/g, ' ').trim().includes(text))
  expect(b, `нет кнопки «${text}»`).toBeDefined()
  b?.click()
  flushSync()
}

describe('Buy', () => {
  it('список тарифов: имя, лимиты и цена «от»', async () => {
    const { root } = await show({}, {})
    expect(root.textContent).toContain('Месяц')
    expect(root.textContent).toContain('Трафик: 100 ГБ. Устройства: 3')
    expect(root.textContent).toContain('от 250 ₽')
    expect(root.textContent).not.toContain('Старый') // скрытый тариф сервер не отдаёт
    click(root, 'Месяц')
    expect(window.location.hash).toBe('#/buy/1')
  })

  it('сроки тарифа с ценами', async () => {
    const { root } = await show({ tariff: '1' }, {})
    expect(root.textContent).toContain('30 дн.')
    expect(root.textContent).toContain('750 ₽')
    click(root, '90 дн.')
    expect(window.location.hash).toBe('#/buy/1/90')
  })

  it('хватает денег: итог, «Оплатить с баланса», покупка и переход на подключение', async () => {
    const buy = vi.fn(async () => golden<BuyResponse>('buy'))
    const { root } = await show({ tariff: '1', days: '30' }, { quote: vi.fn(async () => golden<QuoteResponse>('quote_enough')), buy })
    expect(root.textContent).toContain('250 ₽')
    expect(root.textContent).toContain('400 ₽')
    expect(root.textContent).not.toContain('Не хватает')
    click(root, 'Оплатить с баланса')
    await vi.waitFor(() => expect(window.location.hash).toBe('#/connect'))
    expect(buy).toHaveBeenCalledWith(1, 30, expect.stringMatching(/^[A-Za-z0-9_-]{16,64}$/))
  })

  it('не хватает: недостача и «Пополнить на разницу», выбор запоминается покупкой, переход на пополнение с суммой', async () => {
    const e = golden<ErrorBody>('buy_need_money')
    const buy = vi.fn(async () => Promise.reject(new ApiError(402, e)))
    const { root } = await show({ tariff: '1', days: '30' }, { quote: vi.fn(async () => golden<QuoteResponse>('quote_short')), buy })
    expect(root.textContent).toContain('Не хватает')
    expect(root.textContent).toContain('250 ₽') // и цена, и недостача
    expect(root.textContent).not.toContain('Оплатить с баланса')
    click(root, 'Пополнить на 250 ₽')
    await vi.waitFor(() => expect(window.location.hash).toBe('#/topup/15000'))
    expect(buy).toHaveBeenCalledTimes(1)
  })

  it('другой тариф при живой подписке: когда включится', async () => {
    const q: QuoteResponse = { ...golden<QuoteResponse>('quote_enough'), queued: true, starts_at: '2026-04-09T12:00:00Z' }
    const { root } = await show({ tariff: '1', days: '30' }, { quote: vi.fn(async () => q) })
    expect(root.textContent).toContain('Включится 09.04.2026')
  })

  it('покупка с ошибкой: текст и код, на подключение не уходит', async () => {
    const e = { ...golden<ErrorBody>('error_not_found'), code: 'E3012', text: 'Уже ждёт смена тарифа' }
    const buy = vi.fn(async () => Promise.reject(new ApiError(409, e)))
    const { root } = await show({ tariff: '1', days: '30' }, { quote: vi.fn(async () => golden<QuoteResponse>('quote_enough')), buy })
    click(root, 'Оплатить с баланса')
    await vi.waitFor(() => expect(root.querySelector('[role=alert]')?.textContent).toContain('E3012'))
    expect(window.location.hash).not.toBe('#/connect')
  })

  it('двойной клик покупает один раз', async () => {
    let resolve: (v: BuyResponse) => void = () => {}
    const buy = vi.fn(() => new Promise<BuyResponse>((r) => (resolve = r)))
    const { root } = await show({ tariff: '1', days: '30' }, { quote: vi.fn(async () => golden<QuoteResponse>('quote_enough')), buy })
    click(root, 'Оплатить с баланса')
    click(root, 'Оплатить с баланса')
    expect(buy).toHaveBeenCalledTimes(1)
    resolve(golden<BuyResponse>('buy'))
  })
})

describe('Buy: ключ повтора', () => {
  it('после обрыва связи повтор шлёт тот же ключ, после ответа сервера новое действие новый', async () => {
    const buy = vi
      .fn()
      .mockRejectedValueOnce(new NetworkError())
      .mockResolvedValue(golden<BuyResponse>('buy'))
    const { root } = await show({ tariff: '1', days: '30' }, { quote: vi.fn(async () => golden<QuoteResponse>('quote_enough')), buy })
    click(root, 'Оплатить с баланса')
    await vi.waitFor(() => expect(buy).toHaveBeenCalledTimes(1))
    await vi.waitFor(() => expect((root.querySelector('button.btn') as HTMLButtonElement).disabled).toBe(false))
    click(root, 'Оплатить с баланса')
    await vi.waitFor(() => expect(buy).toHaveBeenCalledTimes(2))
    expect(buy.mock.calls[1]?.[2]).toBe(buy.mock.calls[0]?.[2])
  })
})
