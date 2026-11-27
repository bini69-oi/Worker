// Состояние экрана: загрузка, готово, ошибка с кодом, «нет связи». Белого экрана нет ни в одном из них.
import { writable, type Readable } from 'svelte/store'
import { ApiError, NetworkError } from './api'

export type View<T> =
  | { status: 'loading' }
  | { status: 'ready'; data: T }
  | { status: 'error'; code: string; text: string }
  | { status: 'offline' }

/** Ошибка вызова в состояние экрана. Не API и не сеть (ошибка в самой странице): общий текст без подробностей. */
export function failView(err: unknown): View<never> {
  if (err instanceof ApiError) return { status: 'error', code: err.code, text: err.message }
  if (err instanceof NetworkError) return { status: 'offline' }
  return { status: 'error', code: '', text: '' }
}

export interface Loader<T> extends Readable<View<T>> {
  /** Загрузить заново: экран снова показывает загрузку. */
  reload(): Promise<void>
  /** Обновить данные тихо, без экрана загрузки (после действия на этом же экране). */
  refresh(): Promise<void>
}

/** Загрузчик данных экрана. Если запросов несколько подряд, в экран попадает ответ последнего. */
export function createLoader<T>(fn: () => Promise<T>, opts: { auto?: boolean } = {}): Loader<T> {
  const store = writable<View<T>>({ status: 'loading' })
  let seq = 0
  async function run(silent: boolean): Promise<void> {
    const mine = ++seq
    if (!silent) store.set({ status: 'loading' })
    try {
      const data = await fn()
      if (mine === seq) store.set({ status: 'ready', data })
    } catch (e) {
      if (mine === seq && !(silent && isNetwork(e))) store.set(failView(e))
    }
  }
  const loader: Loader<T> = {
    subscribe: store.subscribe,
    reload: () => run(false),
    refresh: () => run(true),
  }
  if (opts.auto !== false) void run(false)
  return loader
}

function isNetwork(e: unknown): boolean {
  return e instanceof NetworkError
}
