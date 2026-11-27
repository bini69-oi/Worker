// Общие вещи страницы доходят до экранов через контекст Svelte: тексты, API, роутер, сессия.
import { getContext, setContext } from 'svelte'
import type { Readable } from 'svelte/store'
import type { Translate } from './texts'

const TEXTS = Symbol('texts')

export function provideTexts(t: Readable<Translate>): void {
  setContext(TEXTS, t)
}

/** Переводчик текущего экрана: `$t(ключ, { n: 1 })`. */
export function useT(): Readable<Translate> {
  return getContext<Readable<Translate>>(TEXTS)
}

export { TEXTS }
