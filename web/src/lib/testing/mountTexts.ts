// Помощники тестов компонентов: тексты и монтирование в happy-dom. В сборку не попадает.
import { flushSync, mount, unmount, type Component } from 'svelte'
import { TEXTS } from '../context'
import { createTexts } from '../texts'

/** Тексты для тестов: ключ остаётся ключом, если его нет в dict. */
export function fakeTexts(dict: Record<string, string> = {}, lang = 'ru') {
  const tx = createTexts()
  tx.set({ lang, texts: dict })
  return tx
}

/** Монтирует компонент в body с контекстом текстов. Отдаёт корень и размонтирование. */
export function render<P extends Record<string, unknown>>(
  comp: Component<P>,
  props: P,
  dict: Record<string, string> = {},
  extra: [unknown, unknown][] = [],
) {
  const tx = fakeTexts(dict)
  const target = document.createElement('div')
  document.body.appendChild(target)
  const ctx = new Map<unknown, unknown>([[TEXTS, tx.t], ...extra])
  const app = mount(comp, { target, props, context: ctx })
  flushSync()
  return {
    root: target,
    texts: tx,
    unmount: () => {
      void unmount(app)
      target.remove()
    },
  }
}

export { flushSync }

import { createRouter } from '../router'
import { APP, type AppCtx } from '../../app/context'
import type { Endpoints } from '../../app/endpoints'
import { createLoader } from '../view'
import type { MeResponse } from '../types.gen'

/** Контекст кабинета для теста экрана: данные главной из эталона, вызовы API подставляет тест. */
export function fakeApp(me: MeResponse, ep: Partial<Endpoints> = {}, over: Partial<AppCtx> = {}) {
  const router = createRouter(window)
  const loader = createLoader(async () => me)
  const ctx = { ep: ep as Endpoints, router, me: loader, tg: null, win: window, session: {} as never, ...over } as AppCtx
  return { ctx, router, loader, extra: [[APP, ctx]] as [unknown, unknown][] }
}
