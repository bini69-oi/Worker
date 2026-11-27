import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, NetworkError } from '../lib/api'
import { golden } from '../lib/testing/golden'
import { fakeApp, flushSync, render } from '../lib/testing/mountTexts'
import type { ErrorBody, MeResponse, PaymentResponse } from '../lib/types.gen'
import Pay from './Pay.svelte'

const dict = { 'app.pay_paid': 'Оплачено', 'app.pay_waiting': 'Ждём оплату', 'app.pay_expired': 'Счёт истёк', 'app.pay_gave_up': 'Оплата не найдена', 'app.money': '{amount} ₽' }
const paid = golden<PaymentResponse>('payment_paid')
const pending: PaymentResponse = { ...paid, status: 'pending', paid_at: null }

let stop: (() => void) | undefined
beforeEach(() => vi.useFakeTimers())
afterEach(() => {
  stop?.()
  vi.useRealTimers()
  window.location.hash = ''
})

async function show(payments: (() => Promise<PaymentResponse>)[], over: { every?: number; limit?: number } = {}) {
  const queue = [...payments]
  const payment = vi.fn(() => (queue.length > 1 ? queue.shift()! : queue[0]!)())
  const f = fakeApp(golden<MeResponse>('me_with_sub'), { payment } as never)
  const refresh = vi.spyOn(f.loader, 'refresh')
  await f.loader.reload()
  const r = render(Pay, { params: { id: '1' }, every: 3000, ...over }, dict, f.extra)
  stop = r.unmount
  await vi.advanceTimersByTimeAsync(0)
  flushSync()
  return { ...r, payment, refresh }
}

const tick = async (ms: number) => {
  await vi.advanceTimersByTimeAsync(ms)
  flushSync()
}

describe('Pay: статус платежа', () => {
  it('опрашивает раз в 3 секунды, пока платёж ожидает', async () => {
    const { root, payment } = await show([async () => pending])
    expect(root.textContent).toContain('Ждём оплату')
    await tick(3000)
    await tick(3000)
    expect(payment).toHaveBeenCalledTimes(3)
    expect(payment).toHaveBeenCalledWith(1)
  })

  it('оплачен: показывает сумму, обновляет главную и перестаёт опрашивать', async () => {
    const { root, payment, refresh } = await show([async () => pending, async () => paid])
    await tick(3000)
    expect(root.textContent).toContain('Оплачено')
    expect(root.textContent).toContain('300 ₽')
    expect(refresh).toHaveBeenCalled()
    await tick(10000)
    expect(payment).toHaveBeenCalledTimes(2)
  })

  it('истёк: сообщение и «попробовать снова»', async () => {
    const { root } = await show([async () => ({ ...pending, status: 'expired' })])
    expect(root.textContent).toContain('Счёт истёк')
  })

  it('связь моргнула: опрос продолжается', async () => {
    const { root } = await show([() => Promise.reject(new NetworkError()), async () => paid])
    await tick(3000)
    expect(root.textContent).toContain('Оплачено')
  })

  it('чужой или несуществующий платёж: текст ошибки без повторов', async () => {
    const e = golden<ErrorBody>('error_not_found')
    const { root, payment } = await show([() => Promise.reject(new ApiError(404, e))])
    expect(root.querySelector('[role=alert]')?.textContent).toContain('E1001')
    await tick(9000)
    expect(payment).toHaveBeenCalledTimes(1)
  })

  it('через 10 минут перестаёт ждать и зовёт в поддержку', async () => {
    const { root, payment } = await show([async () => pending], { limit: 10 * 60 * 1000 })
    await tick(10 * 60 * 1000 + 3000)
    expect(root.textContent).toContain('Оплата не найдена')
    const calls = payment.mock.calls.length
    await tick(30000)
    expect(payment).toHaveBeenCalledTimes(calls)
  })

  it('уход с экрана останавливает опрос', async () => {
    const { payment, unmount } = await show([async () => pending])
    unmount()
    await tick(10000)
    expect(payment).toHaveBeenCalledTimes(1)
  })
})
