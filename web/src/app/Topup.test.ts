import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../lib/api'
import { golden } from '../lib/testing/golden'
import { fakeApp, flushSync, render } from '../lib/testing/mountTexts'
import type { TgWebApp } from '../lib/telegram'
import type { ErrorBody, MeResponse, TopupResponse } from '../lib/types.gen'
import Topup from './Topup.svelte'

const dict = {
  'app.topup_gateways': 'Касса для {amount}',
  'app.topup_limits': 'От {min} до {max}',
  'app.topup_bad_amount': 'Целое число рублей до {max}',
  'app.money': '{amount} ₽',
  'app.connect_next': 'Дальше',
}

let stop: (() => void) | undefined
afterEach(() => {
  stop?.()
  window.location.hash = ''
})

function meWith(gateways: string[]): MeResponse {
  const me = golden<MeResponse>('me_no_sub')
  return { ...me, gateways: gateways.map((name) => ({ name, title: name === 'stars' ? 'Telegram Stars' : `Касса ${name}` })) }
}

async function show(params: Record<string, string>, opts: { gateways?: string[]; ep?: Record<string, unknown>; tg?: unknown; win?: unknown } = {}) {
  const over: Record<string, unknown> = {}
  if (opts.tg) over.tg = opts.tg
  if (opts.win) over.win = opts.win
  const f = fakeApp(meWith(opts.gateways ?? ['manual', 'fake', 'stars']), (opts.ep ?? {}) as never, over)
  await f.loader.reload()
  const r = render(Topup, { params }, dict, f.extra)
  stop = r.unmount
  await new Promise((res) => setTimeout(res, 0))
  flushSync()
  return r
}

const click = (root: HTMLElement, text: string) => {
  const b = [...root.querySelectorAll('button')].find((x) => x.textContent?.trim().includes(text))
  expect(b, `нет кнопки «${text}»`).toBeDefined()
  b?.click()
  flushSync()
}

const fakeTg = (status: 'paid' | 'cancelled' = 'paid') =>
  ({ openInvoice: vi.fn((_u: string, cb: (s: string) => void) => cb(status)), openLink: vi.fn(), openTelegramLink: vi.fn() }) as unknown as TgWebApp

describe('Topup: суммы', () => {
  it('готовые суммы ведут к выбору кассы', async () => {
    const { root } = await show({})
    const texts = [...root.querySelectorAll('.preset')].map((b) => b.textContent?.replace(/\s/g, ' ').trim())
    expect(texts).toEqual(['100 ₽', '250 ₽', '500 ₽', '1 000 ₽'])
    click(root, '250 ₽')
    expect(window.location.hash).toBe('#/topup/25000')
  })

  it('своя сумма: целые рубли, мусор отклоняется', async () => {
    const { root } = await show({})
    const input = root.querySelector('input') as HTMLInputElement
    const next = [...root.querySelectorAll('button')].find((b) => b.textContent?.trim() === 'Дальше') as HTMLButtonElement
    for (const bad of ['0', '-5', '12.5', 'abc', '1000001', '007']) {
      input.value = bad
      input.dispatchEvent(new Event('input', { bubbles: true }))
      flushSync()
      expect(next.disabled, bad).toBe(true)
    }
    input.value = '777'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()
    expect(next.disabled).toBe(false)
    next.click()
    expect(window.location.hash).toBe('#/topup/77700')
  })
})

describe('Topup: кассы', () => {
  it('Stars только в Telegram: в браузере в списке касс их нет', async () => {
    const { root } = await show({ amount: '30000' })
    const titles = [...root.querySelectorAll('.gw')].map((b) => b.textContent?.trim())
    expect(titles).toEqual(['Касса manual', 'Касса fake'])
  })

  it('в Telegram Stars есть, счёт открывается окном Telegram, потом статус платежа', async () => {
    const tg = fakeTg('paid')
    const topup = vi.fn(async () => ({ ...golden<TopupResponse>('topup_link'), gateway: 'stars', url: '', invoice_link: 'https://t.me/$inv', payment_id: 5 }))
    const { root } = await show({ amount: '30000' }, { tg, ep: { topup } })
    expect([...root.querySelectorAll('.gw')].map((b) => b.textContent?.trim())).toContain('Telegram Stars')
    click(root, 'Telegram Stars')
    await vi.waitFor(() => expect(window.location.hash).toBe('#/pay/5'))
    expect(topup).toHaveBeenCalledWith('stars', 30000)
    expect(tg.openInvoice).toHaveBeenCalledWith('https://t.me/$inv', expect.any(Function))
  })

  it('отмена окна Stars: остаёмся на кассах', async () => {
    const tg = fakeTg('cancelled')
    const topup = vi.fn(async () => ({ ...golden<TopupResponse>('topup_link'), url: '', invoice_link: 'https://t.me/$inv' }))
    const { root } = await show({ amount: '30000' }, { tg, ep: { topup } })
    click(root, 'Telegram Stars')
    await new Promise((r) => setTimeout(r, 0))
    expect(window.location.hash).not.toContain('/pay/')
  })

  it('касса со ссылкой: окно открывается сразу при нажатии, потом ведёт на ссылку кассы', async () => {
    const popup = { location: { href: '' }, opener: {}, close: vi.fn() }
    const open = vi.fn(() => popup)
    let answer: (v: TopupResponse) => void = () => {}
    const topup = vi.fn(() => new Promise<TopupResponse>((r) => (answer = r)))
    const { root } = await show({ amount: '30000' }, { ep: { topup }, win: { open, navigator: {} } })
    click(root, 'Касса fake')
    expect(open).toHaveBeenCalledTimes(1) // до ответа сервера
    expect(popup.location.href).toBe('')
    answer(golden<TopupResponse>('topup_link'))
    await vi.waitFor(() => expect(window.location.hash).toBe('#/pay/1'))
    expect(popup.location.href).toBe('https://pay.test/fake')
    expect(popup.opener).toBeNull()
    expect(popup.close).not.toHaveBeenCalled()
  })

  it('окно заблокировано браузером: ссылка открывается обычным путём', async () => {
    const open = vi.fn().mockReturnValueOnce(null).mockReturnValue({})
    const topup = vi.fn(async () => golden<TopupResponse>('topup_link'))
    const { root } = await show({ amount: '30000' }, { ep: { topup }, win: { open, navigator: {} } })
    click(root, 'Касса fake')
    await vi.waitFor(() => expect(window.location.hash).toBe('#/pay/1'))
    expect(open).toHaveBeenLastCalledWith('https://pay.test/fake', '_blank', 'noopener,noreferrer')
  })

  it('ошибка кассы: пустое окно закрывается', async () => {
    const popup = { location: { href: '' }, close: vi.fn() }
    const e = golden<ErrorBody>('error_not_found')
    const topup = vi.fn(async () => Promise.reject(new ApiError(502, e)))
    const { root } = await show({ amount: '30000' }, { ep: { topup }, win: { open: vi.fn(() => popup), navigator: {} } })
    click(root, 'Касса fake')
    await vi.waitFor(() => expect(popup.close).toHaveBeenCalled())
  })

  it('ручная касса: реквизиты на экране и «Проверить оплату»', async () => {
    const topup = vi.fn(async () => golden<TopupResponse>('topup_manual'))
    const popup = { location: { href: '' }, close: vi.fn() }
    const { root } = await show({ amount: '30000' }, { ep: { topup }, win: { open: vi.fn(() => popup), navigator: {} } })
    click(root, 'Касса manual')
    await vi.waitFor(() => expect(root.textContent).toContain('Карта 0000'))
    expect(popup.close).toHaveBeenCalled() // реквизиты вместо ссылки: пустое окно не нужно
    expect(window.location.hash).not.toContain('/pay/')
  })

  it('сумма вне пределов кассы: пределы в тексте', async () => {
    const e = golden<ErrorBody>('error_not_found')
    const topup = vi.fn(async () => Promise.reject(new ApiError(400, { ...e, code: 'E4001', limit: { min: 10000, max: 0 } })))
    const { root } = await show({ amount: '500' }, { ep: { topup } })
    click(root, 'Касса fake')
    await vi.waitFor(() => expect(root.querySelector('[role=alert]')?.textContent).toContain('От 100 ₽ до app.unlimited'))
  })

  it('нет касс: подсказка', async () => {
    const { root } = await show({ amount: '30000' }, { gateways: [] })
    expect(root.textContent).toContain('app.topup_no_gateways')
  })
})
