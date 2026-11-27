import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../lib/api'
import { golden } from '../lib/testing/golden'
import { fakeApp, flushSync, render } from '../lib/testing/mountTexts'
import type { DevicesResponse, ErrorBody, HistoryResponse, MeResponse, PromoResponse, RefResponse } from '../lib/types.gen'
import type { Component } from 'svelte'
import Devices from './Devices.svelte'
import History from './History.svelte'
import Promo from './Promo.svelte'
import Ref from './Ref.svelte'

const dict = {
  'app.devices_count': 'Устройств: {n} из {limit}',
  'app.devices_delete': 'Удалить',
  'app.devices_confirm': 'Точно удалить?',
  'app.devices_reset': 'Сбросить все',
  'app.devices_reset_confirm': 'Точно сбросить все?',
  'app.devices_empty': 'Устройств нет',
  'app.history_empty': 'Движений нет',
  'app.reason_topup': 'Пополнение',
  'app.reason_buy': 'Покупка',
  'app.reason_admin': 'Корректировка',
  'app.promo_money': 'Начислено {amount}',
  'app.promo_days': 'Добавлено дней: {days}',
  'app.promo_apply': 'Применить',
  'app.ref_invited': 'Пришло',
  'app.connect_copy': 'Скопировать ссылку',
  'app.connect_copied': 'Скопировано',
  'app.money': '{amount} ₽',
}

let stop: (() => void) | undefined
afterEach(() => {
  stop?.()
  window.location.hash = ''
})

async function show(comp: Component, ep: Record<string, unknown>, over: Record<string, unknown> = {}) {
  const f = fakeApp(golden<MeResponse>('me_with_sub'), ep as never, over)
  await f.loader.reload()
  const r = render(comp as Component<Record<string, unknown>>, {}, dict, f.extra)
  stop = r.unmount
  await new Promise((res) => setTimeout(res, 0))
  flushSync()
  return { ...r, loader: f.loader }
}

const click = (root: HTMLElement, text: string) => {
  const b = [...root.querySelectorAll('button')].find((x) => x.textContent?.trim() === text)
  expect(b, `нет кнопки «${text}»`).toBeDefined()
  b?.click()
  flushSync()
}

describe('Devices', () => {
  const list = golden<DevicesResponse>('devices')

  it('список: модель, платформа, дата и сколько из скольки', async () => {
    const { root } = await show(Devices, { devices: vi.fn(async () => list) })
    expect(root.textContent).toContain('iPhone')
    expect(root.textContent).toContain('Android · 10.03.2026')
    expect(root.textContent).toContain('Устройств: 2 из 3')
  })

  it('удаление просит второе нажатие и убирает устройство только после ответа ядра', async () => {
    let remaining = list.devices
    const devices = vi.fn(async () => ({ devices: remaining }))
    const deleteDevice = vi.fn(async (hwid: string) => {
      remaining = remaining.filter((d) => d.hwid !== hwid)
      return {}
    })
    const { root } = await show(Devices, { devices, deleteDevice })
    click(root, 'Удалить')
    expect(deleteDevice).not.toHaveBeenCalled()
    click(root, 'Точно удалить?')
    await vi.waitFor(() => expect(root.textContent).not.toContain('iPhone'))
    expect(deleteDevice).toHaveBeenCalledWith('hw-a1')
    expect(root.textContent).toContain('Pixel')
  })

  it('ошибка удаления: устройство остаётся, виден код', async () => {
    const e = golden<ErrorBody>('error_not_found')
    const deleteDevice = vi.fn(async () => Promise.reject(new ApiError(503, { ...e, code: 'E3008', text: 'Сервис недоступен' })))
    const { root } = await show(Devices, { devices: vi.fn(async () => list), deleteDevice })
    click(root, 'Удалить')
    click(root, 'Точно удалить?')
    await vi.waitFor(() => expect(root.querySelector('[role=alert]')?.textContent).toContain('E3008'))
    expect(root.textContent).toContain('iPhone')
  })

  it('сбросить все: два нажатия, потом пусто', async () => {
    let remaining = list.devices
    const resetDevices = vi.fn(async () => ((remaining = []), {}))
    const { root } = await show(Devices, { devices: vi.fn(async () => ({ devices: remaining })), resetDevices })
    click(root, 'Сбросить все')
    click(root, 'Точно сбросить все?')
    await vi.waitFor(() => expect(root.textContent).toContain('Устройств нет'))
    expect(resetDevices).toHaveBeenCalledTimes(1)
  })
})

describe('History', () => {
  it('движения со знаком и причиной, новые первыми', async () => {
    const { root } = await show(History, { history: vi.fn(async () => golden<HistoryResponse>('history')) })
    const rows = [...root.querySelectorAll('li')].map((li) => li.textContent?.replace(/\s+/g, ' ').trim())
    expect(rows).toHaveLength(3)
    expect(rows[0]).toContain('Покупка')
    expect(rows[0]).toContain('-250 ₽')
    expect(rows[1]).toContain('Корректировка')
    expect(rows[2]).toContain('Пополнение')
    expect(rows[2]).toContain('+300 ₽')
  })

  it('пусто: понятный текст', async () => {
    const { root } = await show(History, { history: vi.fn(async () => ({ items: [] })) })
    expect(root.textContent).toContain('Движений нет')
  })
})

describe('Ref', () => {
  const ref = golden<RefResponse>('ref')
  it('ссылка, цифры и копирование', async () => {
    const writeText = vi.fn(async () => {})
    const { root } = await show(Ref, { ref: vi.fn(async () => ref) }, { win: { navigator: { clipboard: { writeText } }, open: vi.fn() } })
    expect(root.textContent).toContain('https://t.me/worker_bot?start=ref_REFCODE')
    expect(root.textContent).toContain('Пришло')
    click(root, 'Скопировать ссылку')
    await vi.waitFor(() => expect(root.textContent).toContain('Скопировано'))
    expect(writeText).toHaveBeenCalledWith(ref.link)
  })
})

describe('Promo', () => {
  const type = (root: HTMLElement, v: string) => {
    const input = root.querySelector('input') as HTMLInputElement
    input.value = v
    input.dispatchEvent(new Event('input', { bubbles: true }))
    flushSync()
  }

  it('пустой код не отправляется', async () => {
    const promo = vi.fn()
    const { root } = await show(Promo, { promo })
    expect((root.querySelector('button[type=submit]') as HTMLButtonElement).disabled).toBe(true)
  })

  it('промокод на деньги: результат и обновление главной', async () => {
    const promo = vi.fn(async () => golden<PromoResponse>('promo_money'))
    const { root, loader } = await show(Promo, { promo })
    const refresh = vi.spyOn(loader, 'refresh')
    type(root, ' bonus100 ')
    click(root, 'Применить')
    await vi.waitFor(() => expect(root.querySelector('[role=status]')?.textContent).toContain('Начислено 150 ₽'))
    expect(promo).toHaveBeenCalledWith('bonus100')
    expect(refresh).toHaveBeenCalled()
  })

  it('промокод на дни', async () => {
    const promo = vi.fn(async (): Promise<PromoResponse> => ({ kind: 'days', value: 7 }))
    const { root } = await show(Promo, { promo })
    type(root, 'DAYS7777')
    click(root, 'Применить')
    await vi.waitFor(() => expect(root.textContent).toContain('Добавлено дней: 7'))
  })

  it('неверный код: текст и код ошибки', async () => {
    const e = golden<ErrorBody>('error_not_found')
    const promo = vi.fn(async () => Promise.reject(new ApiError(409, { ...e, code: 'E5001', text: 'Такого кода нет' })))
    const { root } = await show(Promo, { promo })
    type(root, 'NOPE1234')
    click(root, 'Применить')
    await vi.waitFor(() => expect(root.querySelector('[role=alert]')?.textContent).toContain('Такого кода нет (E5001)'))
  })
})
