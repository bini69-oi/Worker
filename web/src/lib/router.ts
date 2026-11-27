// Маршруты через `#`: серверу не нужно знать маршруты страницы, а Telegram открывает адрес как есть.
import { readable, type Readable } from 'svelte/store'

/** Путь из адреса: `#/buy/3` это `/buy/3`. Всё, что не похоже на маршрут (например `#tgWebAppData=...`), это `/`. */
export function pathOf(hash: string): string {
  if (!hash.startsWith('#/')) return '/'
  const path = hash.slice(1).split('?')[0] ?? '/'
  return path.length > 1 ? path.replace(/\/+$/, '') : '/'
}

/** Сверяет путь с образцом вида `/pay/:id`. Возвращает значения частей или null. */
export function match(pattern: string, path: string): Record<string, string> | null {
  const want = pattern.split('/').filter(Boolean)
  const have = path.split('/').filter(Boolean)
  if (want.length !== have.length) return null
  const params: Record<string, string> = {}
  for (const [i, part] of want.entries()) {
    const value = have[i] ?? ''
    if (part.startsWith(':')) params[part.slice(1)] = decodeURIComponent(value)
    else if (part !== value) return null
  }
  return params
}

export interface Router {
  /** Текущий путь. */
  path: Readable<string>
  /** Переход вперёд: в истории появляется новый шаг. */
  go(path: string): void
  /** Замена текущего шага: «назад» вернёт не сюда. */
  replace(path: string): void
  /** Шаг назад по истории страницы, а с первого экрана на главную. */
  back(): void
  /** Можно ли вернуться: на главной нет. */
  canBack(): boolean
}

/** Роутер на адресе окна win. Шаги истории считает сам: так «назад» не уводит из кабинета на прошлый сайт. */
export function createRouter(win: Window): Router {
  let depth = 0 // сколько переходов вперёд сделано внутри страницы
  const path = readable(pathOf(win.location.hash), (set) => {
    const on = () => set(pathOf(win.location.hash))
    win.addEventListener('hashchange', on)
    on()
    return () => win.removeEventListener('hashchange', on)
  })
  const current = () => pathOf(win.location.hash)
  return {
    path,
    go(to) {
      if (to === current()) return
      depth++
      win.location.hash = '#' + to
    },
    replace(to) {
      win.location.replace('#' + to)
    },
    back() {
      if (depth > 0) {
        depth--
        win.history.back()
      } else {
        win.location.replace('#/')
      }
    },
    canBack: () => current() !== '/',
  }
}
