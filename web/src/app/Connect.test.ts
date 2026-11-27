import { afterEach, describe, expect, it, vi } from 'vitest'
import { golden } from '../lib/testing/golden'
import { fakeApp, flushSync, render } from '../lib/testing/mountTexts'
import type { ConnectResponse, MeResponse } from '../lib/types.gen'
import { detectPlatform } from './platform'
import Connect from './Connect.svelte'

const dict = {
  'app.platform_ios': 'iPhone',
  'app.platform_android': 'Android',
  'app.platform_desktop': 'Компьютер',
  'app.connect_next': 'Дальше',
  'app.connect_store': 'Скачать',
  'app.connect_add': 'Добавить подписку в {app}',
  'app.connect_copy': 'Скопировать ссылку',
  'app.connect_copied': 'Скопировано',
  'app.connect_nosub': 'Сначала нужна подписка',
  'app.connect_pending': 'Ещё оформляется',
  'app.connect_qr_label': 'QR-код ссылки',
  'app.back': 'Назад',
}

const connect = golden<ConnectResponse>('connect')
let stop: (() => void) | undefined
afterEach(() => stop?.())

async function show(opts: { ua?: string; me?: MeResponse; conn?: ConnectResponse | Error; open?: ReturnType<typeof vi.fn> } = {}) {
  const win = { navigator: { userAgent: opts.ua ?? '', clipboard: { writeText: vi.fn(async () => {}) } }, open: opts.open ?? vi.fn() } as unknown as Window
  const f = fakeApp(opts.me ?? golden<MeResponse>('me_with_sub'), { connect: vi.fn(async () => (opts.conn instanceof Error ? Promise.reject(opts.conn) : (opts.conn ?? connect))) } as never, { win })
  await f.loader.reload()
  const r = render(Connect, {}, dict, f.extra)
  stop = r.unmount
  await vi.waitFor(() => expect(r.root.querySelector('.steps, .state, .card')).not.toBeNull())
  await new Promise((res) => setTimeout(res, 0))
  flushSync()
  return { ...r, win }
}

const click = (root: HTMLElement, text: string) => {
  const b = [...root.querySelectorAll('button, a')].find((x) => x.textContent?.trim() === text) as HTMLElement | undefined
  expect(b, `нет кнопки «${text}»`).toBeDefined()
  b?.click()
  flushSync()
}

describe('detectPlatform', () => {
  it('узнаёт платформу по строке браузера', () => {
    expect(detectPlatform('Mozilla/5.0 (Linux; Android 14; Pixel 8)')).toBe('android')
    expect(detectPlatform('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)')).toBe('ios')
    expect(detectPlatform('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')).toBe('desktop')
    expect(detectPlatform('')).toBe('desktop')
  })
})

describe('Connect: мастер подключения', () => {
  it('Android: платформа выбрана сама, ссылки на магазин ведут в Google Play', async () => {
    const open = vi.fn()
    const { root } = await show({ ua: 'Mozilla/5.0 (Linux; Android 14)', open })
    expect(root.querySelector('[aria-checked=true]')?.textContent?.trim()).toBe('Android')
    click(root, 'Дальше')
    click(root, 'Скачать')
    expect(open).toHaveBeenCalledWith('https://play.google.com/store/apps/details?id=com.happproxy', '_blank', 'noopener,noreferrer')
  })

  it('платформу можно сменить: iPhone ведёт в App Store', async () => {
    const open = vi.fn()
    const { root } = await show({ ua: 'Mozilla/5.0 (Linux; Android 14)', open })
    click(root, 'iPhone')
    click(root, 'Дальше')
    click(root, 'Скачать')
    expect(open.mock.calls[0]?.[0]).toContain('apps.apple.com')
  })

  it('приложение без версии для платформы: подсказка вместо кнопки', async () => {
    const { root } = await show({ ua: 'Mozilla/5.0 (iPhone)', conn: { ...connect, apps: [{ ...connect.apps[0]!, stores: {} }] } })
    click(root, 'Дальше')
    expect(root.textContent).toContain('app.connect_no_store')
  })

  it('последний шаг: QR ссылки подписки, переход в приложение и копирование', async () => {
    const open = vi.fn()
    const { root, win } = await show({ ua: 'Mozilla/5.0 (Windows NT 10.0)', open })
    click(root, 'Дальше')
    click(root, 'Дальше')
    const svg = root.querySelector('svg[role=img]')
    expect(svg?.getAttribute('aria-label')).toBe('QR-код ссылки')
    expect(svg?.querySelector('path')?.getAttribute('d')?.length).toBeGreaterThan(100)
    click(root, 'Добавить подписку в Happ')
    expect(open).toHaveBeenCalledWith('https://w.test/dl/happ/TOKEN', '_blank', 'noopener,noreferrer')
    click(root, 'Скопировать ссылку')
    await vi.waitFor(() => expect(root.textContent).toContain('Скопировано'))
    expect((win.navigator.clipboard.writeText as ReturnType<typeof vi.fn>).mock.calls[0]?.[0]).toBe('https://sub.test/ref-1/0')
  })

  it('выбор другого приложения доходит до перехода', async () => {
    const open = vi.fn()
    const { root } = await show({ ua: 'Mozilla/5.0 (Linux; Android 14)', open })
    click(root, 'Дальше')
    click(root, 'v2RayTun')
    click(root, 'Дальше')
    click(root, 'Добавить подписку в v2RayTun')
    expect(open).toHaveBeenCalledWith('https://w.test/dl/v2raytun/TOKEN', '_blank', 'noopener,noreferrer')
  })

  it('подписку ещё оформляют: подсказка и проверка', async () => {
    const { root } = await show({ conn: { sub_url: '', apps: [] } })
    expect(root.textContent).toContain('Ещё оформляется')
    expect(root.querySelector('svg[role=img]')).toBeNull()
  })

  it('без подписки: предлагает купить, подключения нет', async () => {
    const { root } = await show({ me: golden<MeResponse>('me_no_sub') })
    expect(root.textContent).toContain('Сначала нужна подписка')
  })
})
