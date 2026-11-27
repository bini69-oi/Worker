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
