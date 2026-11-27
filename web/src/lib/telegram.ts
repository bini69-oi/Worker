// Мост к Telegram Mini App. Скрипт Telegram грузится только внутри Telegram: в браузере его нет совсем.
import type { Router } from './router'

export const SCRIPT_URL = 'https://telegram.org/js/telegram-web-app.js'

export type InvoiceStatus = 'paid' | 'cancelled' | 'failed' | 'pending'

/** То, что страница берёт у Telegram.WebApp. */
export interface TgWebApp {
  initData: string
  colorScheme: 'light' | 'dark'
  themeParams: Record<string, string>
  ready(): void
  expand(): void
  openInvoice(url: string, cb: (status: InvoiceStatus) => void): void
  openLink(url: string): void
  openTelegramLink(url: string): void
  onEvent(name: string, fn: () => void): void
  offEvent(name: string, fn: () => void): void
  BackButton: { show(): void; hide(): void; onClick(fn: () => void): void; offClick(fn: () => void): void }
}

type TgWindow = Window & { Telegram?: { WebApp?: TgWebApp } }

/** Открыта ли страница внутри Telegram: в адресе есть данные запуска или они сохранены с прошлой загрузки. */
export function inTelegram(win: Window): boolean {
  const { hash, search } = win.location
  if (hash.includes('tgWebAppData=') || search.includes('tgWebAppData=')) return true
  try {
    return win.sessionStorage.getItem('__telegram__initParams') !== null
  } catch {
    return false
  }
}

/** Подключает скрипт Telegram и отдаёт WebApp. Не в Telegram: null, скрипт не грузится. */
export async function loadTelegram(
  win: Window,
  loadScript: (src: string) => Promise<void> = (src) => addScript(win.document, src),
): Promise<TgWebApp | null> {
  if (!inTelegram(win)) return null
  try {
    await loadScript(SCRIPT_URL)
  } catch {
    return null
  }
  const app = (win as TgWindow).Telegram?.WebApp
  if (!app) return null
  app.ready()
  app.expand()
  return app
}

function addScript(doc: Document, src: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const s = doc.createElement('script')
    s.src = src
    s.async = true
    s.onload = () => resolve()
    s.onerror = () => reject(new Error('telegram script'))
    doc.head.appendChild(s)
  })
}

/** Цвета темы Telegram в CSS-переменные страницы. Что Telegram не прислал, остаётся из стилей страницы. */
const THEME_VARS: Record<string, string> = {
  bg_color: '--bg',
  text_color: '--text',
  hint_color: '--hint',
  link_color: '--link',
  button_color: '--accent',
  button_text_color: '--accent-text',
  secondary_bg_color: '--surface',
}

export function applyTheme(app: TgWebApp, root: HTMLElement): void {
  root.dataset.theme = app.colorScheme
  for (const [key, cssVar] of Object.entries(THEME_VARS)) {
    const v = app.themeParams[key]
    if (v) root.style.setProperty(cssVar, v)
  }
}

/** Тема Telegram меняется на лету: подписка на смену, отписка возвращается. */
export function watchTheme(app: TgWebApp, root: HTMLElement): () => void {
  const on = () => applyTheme(app, root)
  app.onEvent('themeChanged', on)
  on()
  return () => app.offEvent('themeChanged', on)
}

/** Кнопка «Назад» Telegram: видна на всех экранах кроме главной, ведёт назад по истории страницы. */
export function bindBackButton(app: TgWebApp, router: Router): () => void {
  const click = () => router.back()
  app.BackButton.onClick(click)
  const off = router.path.subscribe(() => (router.canBack() ? app.BackButton.show() : app.BackButton.hide()))
  return () => {
    off()
    app.BackButton.offClick(click)
    app.BackButton.hide()
  }
}

/** Счёт Stars: Telegram сам показывает окно оплаты и отвечает статусом. */
export function openInvoice(app: TgWebApp, link: string): Promise<InvoiceStatus> {
  return new Promise((resolve) => app.openInvoice(link, resolve))
}

/** Внешняя ссылка: в Telegram через openLink, в браузере новой вкладкой без доступа к странице. */
export function openExternal(app: TgWebApp | null, win: Window, url: string): void {
  if (app) app.openLink(url)
  else win.open(url, '_blank', 'noopener,noreferrer')
}

/** Чат бота: в Telegram открывается внутри приложения, в браузере в новой вкладке. */
export function openChat(app: TgWebApp | null, win: Window, url: string): void {
  if (app) app.openTelegramLink(url)
  else openExternal(null, win, url)
}
