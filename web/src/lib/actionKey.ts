// Ключ повтора действия. Один ключ на одно действие человека: повтор после «Нет связи» шлёт тот же, и сервер
// не спишет деньги второй раз. Новое действие это новый ключ. Ключ: 16-64 знака из латиницы, цифр, `_` и `-`.
import { ApiError } from './api'

/** Случайный ключ из 22 знаков (128 бит). */
export function newKey(rand: (a: Uint8Array<ArrayBuffer>) => Uint8Array = (a) => crypto.getRandomValues(a)): string {
  const bytes = rand(new Uint8Array(16))
  let bin = ''
  for (const b of bytes) bin += String.fromCharCode(b)
  return btoa(bin).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '')
}

export interface ActionKey {
  /** Ключ текущего действия: создаётся при первом обращении. */
  get(): string
  /** Действие закончено (сервер ответил, успехом или отказом): следующее нажатие это новое действие. */
  reset(): void
}

export function actionKey(): ActionKey {
  let key = ''
  return {
    get: () => (key ||= newKey()),
    reset: () => {
      key = ''
    },
  }
}

/** Выполняет действие с ключом. Ответ сервера любой (успех или отказ) закрывает действие, обрыв связи оставляет ключ для повтора. */
export async function withKey<T>(k: ActionKey, run: (key: string) => Promise<T>): Promise<T> {
  try {
    const r = await run(k.get())
    k.reset()
    return r
  } catch (e) {
    if (e instanceof ApiError) k.reset()
    throw e
  }
}
