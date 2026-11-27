import { describe, expect, it, vi } from 'vitest'
import { createRouter } from './router'
import {
  SCRIPT_URL,
  applyTheme,
  bindBackButton,
  inTelegram,
  loadTelegram,
  openChat,
  openExternal,
  openInvoice,
  watchTheme,
  type TgWebApp,
} from './telegram'

function fakeApp(over: Partial<TgWebApp> = {}): TgWebApp & { fire(name: string): void; clicks: (() => void)[]; visible: boolean } {
  const handlers = new Map<string, (() => void)[]>()
  const clicks: (() => void)[] = []
  const app = {
    initData: 'query_id=x&hash=y',
    colorScheme: 'dark',
    themeParams: { bg_color: '#111111', text_color: '#eeeeee', button_color: '#22aa66', secondary_bg_color: '#222222' },
    visible: false,
    clicks,
    ready: vi.fn(),
    expand: vi.fn(),
    openInvoice: vi.fn((_u: string, cb: (s: 'paid') => void) => cb('paid')),
    openLink: vi.fn(),
    openTelegramLink: vi.fn(),
    onEvent: (n: string, fn: () => void) => handlers.set(n, [...(handlers.get(n) ?? []), fn]),
    offEvent: (n: string, fn: () => void) => handlers.set(n, (handlers.get(n) ?? []).filter((f) => f !== fn)),
    fire: (n: string) => (handlers.get(n) ?? []).forEach((f) => f()),
    BackButton: {
      show() {
        app.visible = true
      },
      hide() {
        app.visible = false
      },
      onClick: (fn: () => void) => clicks.push(fn),
      offClick: (fn: () => void) => clicks.splice(clicks.indexOf(fn), 1),
    },
    ...over,
  }
  return app as unknown as ReturnType<typeof fakeApp>
}

function winWith(hash: string, search = ''): Window {
  return { location: { hash, search }, sessionStorage: { getItem: () => null } } as unknown as Window
}

describe('inTelegram', () => {
  it('видит данные запуска в адресе', () => {
    expect(inTelegram(winWith('#tgWebAppData=query_id%3Dx&tgWebAppVersion=8'))).toBe(true)
    expect(inTelegram(winWith('', '?tgWebAppData=x'))).toBe(true)
  })
  it('видит сохранённые данные после перезагрузки внутри Telegram', () => {
    const w = { location: { hash: '#/buy', search: '' }, sessionStorage: { getItem: () => '{}' } } as unknown as Window
    expect(inTelegram(w)).toBe(true)
  })
  it('в обычном браузере нет', () => {
    expect(inTelegram(winWith('#/buy'))).toBe(false)
    expect(inTelegram(winWith(''))).toBe(false)
  })
  it('не падает, если sessionStorage закрыт', () => {
    const w = {
      location: { hash: '', search: '' },
      get sessionStorage(): never {
        throw new Error('blocked')
      },
    } as unknown as Window
    expect(inTelegram(w)).toBe(false)
  })
})

describe('loadTelegram', () => {
  it('в браузере скрипт не грузится', async () => {
    const load = vi.fn(async () => {})
    expect(await loadTelegram(winWith('#/'), load)).toBeNull()
    expect(load).not.toHaveBeenCalled()
  })

  it('в Telegram грузит скрипт, готовит окно и отдаёт WebApp', async () => {
    const app = fakeApp()
    const w = winWith('#tgWebAppData=x') as Window & { Telegram?: { WebApp: TgWebApp } }
    const load = vi.fn(async (_src: string) => {
      w.Telegram = { WebApp: app }
    })
    expect(await loadTelegram(w, load)).toBe(app)
    expect(load).toHaveBeenCalledWith(SCRIPT_URL)
    expect(app.ready).toHaveBeenCalled()
    expect(app.expand).toHaveBeenCalled()
  })

  it('скрипт не загрузился: страница остаётся рабочей без Telegram', async () => {
    const load = vi.fn(async () => {
      throw new Error('offline')
    })
    expect(await loadTelegram(winWith('#tgWebAppData=x'), load)).toBeNull()
  })
})

describe('тема', () => {
  it('берёт цвета Telegram и схему', () => {
    const root = document.createElement('html')
    applyTheme(fakeApp(), root)
    expect(root.dataset.theme).toBe('dark')
    expect(root.style.getPropertyValue('--bg')).toBe('#111111')
    expect(root.style.getPropertyValue('--accent')).toBe('#22aa66')
    expect(root.style.getPropertyValue('--surface')).toBe('#222222')
    expect(root.style.getPropertyValue('--link')).toBe('') // Telegram не прислал: остаётся своё
  })

  it('меняется вместе с темой Telegram, отписка работает', () => {
    const root = document.createElement('html')
    const app = fakeApp()
    const stop = watchTheme(app, root)
    app.themeParams = { bg_color: '#ffffff' }
    app.colorScheme = 'light'
    app.fire('themeChanged')
    expect(root.dataset.theme).toBe('light')
    expect(root.style.getPropertyValue('--bg')).toBe('#ffffff')
    stop()
    app.colorScheme = 'dark'
    app.fire('themeChanged')
    expect(root.dataset.theme).toBe('light')
  })
})

describe('кнопка «Назад»', () => {
  it('скрыта на главной, видна на других экранах и ведёт назад', async () => {
    window.location.hash = ''
    const router = createRouter(window)
    const app = fakeApp()
    const back = vi.spyOn(router, 'back')
    const stop = bindBackButton(app, router)
    expect(app.visible).toBe(false)
    router.go('/buy')
    await new Promise((r) => setTimeout(r, 0))
    expect(app.visible).toBe(true)
    app.clicks[0]?.()
    expect(back).toHaveBeenCalled()
    stop()
    expect(app.visible).toBe(false)
    expect(app.clicks).toHaveLength(0)
  })
})

describe('счёт, ссылки, чат', () => {
  it('openInvoice отдаёт статус от Telegram', async () => {
    const app = fakeApp()
    expect(await openInvoice(app, 'https://t.me/$inv')).toBe('paid')
    expect(app.openInvoice).toHaveBeenCalledWith('https://t.me/$inv', expect.any(Function))
  })

  it('внешняя ссылка: в Telegram через openLink, в браузере новая вкладка без opener', () => {
    const app = fakeApp()
    const open = vi.fn()
    const win = { open } as unknown as Window
    openExternal(app, win, 'https://pay.test/1')
    expect(app.openLink).toHaveBeenCalledWith('https://pay.test/1')
    expect(open).not.toHaveBeenCalled()
    openExternal(null, win, 'https://pay.test/2')
    expect(open).toHaveBeenCalledWith('https://pay.test/2', '_blank', 'noopener,noreferrer')
  })

  it('поддержка открывает чат бота внутри Telegram', () => {
    const app = fakeApp()
    const open = vi.fn()
    openChat(app, { open } as unknown as Window, 'https://t.me/worker_bot')
    expect(app.openTelegramLink).toHaveBeenCalledWith('https://t.me/worker_bot')
    openChat(null, { open } as unknown as Window, 'https://t.me/worker_bot')
    expect(open).toHaveBeenCalled()
  })
})
