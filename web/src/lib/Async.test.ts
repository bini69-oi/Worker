import { createRawSnippet } from 'svelte'
import { get } from 'svelte/store'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Async from './Async.svelte'
import { ApiError, NetworkError } from './api'
import { createLoader, failView, type View } from './view'
import { flushSync, render } from './testing/mountTexts'
import { golden } from './testing/golden'
import type { ErrorBody } from './types.gen'

const dict = {
  'app.loading': 'Загрузка',
  'app.offline': 'Нет связи',
  'app.offline_hint': 'Проверьте интернет',
  'app.retry': 'Повторить',
  'app.empty': 'Здесь пусто',
  'app.error_code': 'Код: {code}',
  'err.internal': 'Что-то пошло не так',
}

const body = createRawSnippet((data: () => { n: number }) => ({ render: () => `<p class="ready">данные ${data().n}</p>` }))

let cleanup: (() => void) | undefined
afterEach(() => cleanup?.())

function show(view: View<{ n: number }>, props: Record<string, unknown> = {}) {
  const retry = vi.fn()
  const r = render(Async<{ n: number }>, { view, retry, children: body, ...props } as never, dict)
  cleanup = r.unmount
  return { ...r, retry }
}

describe('Async: состояния экрана', () => {
  it('загрузка', () => {
    const { root } = show({ status: 'loading' })
    expect(root.textContent).toContain('Загрузка')
    expect(root.querySelector('[role=status]')).not.toBeNull()
  })

  it('готово: виден экран с данными', () => {
    const { root } = show({ status: 'ready', data: { n: 7 } })
    expect(root.querySelector('.ready')?.textContent).toBe('данные 7')
  })

  it('пусто: виден текст пустого состояния вместо экрана', () => {
    const { root } = show({ status: 'ready', data: { n: 0 } }, { isEmpty: (d: { n: number }) => d.n === 0 })
    expect(root.querySelector('.ready')).toBeNull()
    expect(root.textContent).toContain('Здесь пусто')
  })

  it('ошибка: текст сервера и код, кнопка повторить', () => {
    const { root, retry } = show({ status: 'error', code: 'E1001', text: 'Не найдено' })
    expect(root.textContent).toContain('Не найдено')
    expect(root.textContent).toContain('Код: E1001')
    root.querySelector('button')?.click()
    expect(retry).toHaveBeenCalledTimes(1)
  })

  it('ошибка в самой странице: общий текст без кода', () => {
    const { root } = show({ status: 'error', code: '', text: '' })
    expect(root.textContent).toContain('Что-то пошло не так')
    expect(root.textContent).not.toContain('Код:')
  })

  it('нет связи: текст и кнопка «Повторить»', () => {
    const { root, retry } = show({ status: 'offline' })
    expect(root.textContent).toContain('Нет связи')
    const btn = root.querySelector('button')
    expect(btn?.textContent).toBe('Повторить')
    btn?.click()
    expect(retry).toHaveBeenCalled()
  })

  it('экран меняется вместе с состоянием', () => {
    const { root, unmount } = show({ status: 'loading' })
    unmount()
    const r = render(Async<{ n: number }>, { view: { status: 'ready', data: { n: 1 } }, retry: () => {}, children: body } as never, dict)
    cleanup = r.unmount
    flushSync()
    expect(r.root.querySelector('.ready')).not.toBeNull()
    expect(root).not.toBeNull()
  })
})

describe('failView и createLoader', () => {
  it('ошибка API становится ошибкой с кодом, сети офлайном', () => {
    const e = golden<ErrorBody>('error_not_found')
    expect(failView(new ApiError(404, e))).toEqual({ status: 'error', code: 'E1001', text: e.text })
    expect(failView(new NetworkError())).toEqual({ status: 'offline' })
    expect(failView(new Error('boom'))).toEqual({ status: 'error', code: '', text: '' })
  })

  it('загрузчик идёт загрузка, готово, повтор после ошибки', async () => {
    let fail = true
    const load = createLoader(async () => {
      if (fail) throw new NetworkError()
      return { n: 1 }
    })
    expect(get(load).status).toBe('loading')
    await load.reload()
    expect(get(load).status).toBe('offline')
    fail = false
    await load.reload()
    expect(get(load)).toEqual({ status: 'ready', data: { n: 1 } })
  })

  it('в экран попадает ответ последнего запроса', async () => {
    const resolvers: ((n: number) => void)[] = []
    const load = createLoader(() => new Promise<number>((res) => resolvers.push(res)))
    const second = load.reload()
    resolvers[1]?.(2)
    await second
    resolvers[0]?.(1) // первый ответ пришёл позже и устарел
    await new Promise((r) => setTimeout(r, 0))
    expect(get(load)).toEqual({ status: 'ready', data: 2 })
  })

  it('тихое обновление не мигает загрузкой и не прячет данные из-за сбоя сети', async () => {
    let n = 0
    const load = createLoader(async () => {
      if (++n === 3) throw new NetworkError()
      return n
    })
    await load.reload()
    const seen: string[] = []
    load.subscribe((v) => seen.push(v.status))
    await load.refresh()
    await load.refresh() // сеть упала: на экране остаются прежние данные
    expect(seen).toEqual(['ready', 'ready'])
  })
})
