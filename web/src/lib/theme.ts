// Тема в браузере: по настройке системы, пока Telegram её не задал. Цвета лежат в theme.css переменными.

/** Ставит data-theme по prefers-color-scheme и следит за сменой. Отдаёт отписку. */
export function followSystemTheme(win: Window, root: HTMLElement): () => void {
  const mq = win.matchMedia('(prefers-color-scheme: dark)')
  const set = () => {
    root.dataset.theme = mq.matches ? 'dark' : 'light'
  }
  set()
  mq.addEventListener('change', set)
  return () => mq.removeEventListener('change', set)
}
