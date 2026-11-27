// Сессия страницы: как войти (Mini App по initData, браузер через бота), что делать на 401, когда выходить.
import { get, writable, type Readable } from 'svelte/store'
import { ApiError } from '../lib/api'
import type { createTexts } from '../lib/texts'
import { createLoader, failView, type Loader, type View } from '../lib/view'
import type { MeResponse } from '../lib/types.gen'
import type { Endpoints } from './endpoints'

/**
 * booting: узнаём, кто это. login: нужен вход в браузере. ready: кабинет открыт. banned: абонент заблокирован.
 * stale: данные запуска Telegram устарели, нужно открыть кабинет из бота заново. failed: не вышло открыть.
 */
export type SessionState = 'booting' | 'login' | 'ready' | 'banned' | 'stale' | 'failed'

const TOKEN_KEY = 'worker_token'
const BANNED_CODE = 'E3006'
const GONE_CODE = 'E6008'
const BAD_INIT_CODE = 'E6040'

export interface SessionDeps {
  /** initData Telegram, если страница открыта внутри Telegram. */
  initData: string | null
  storage: Pick<Storage, 'getItem' | 'setItem' | 'removeItem'> | null
  texts: ReturnType<typeof createTexts>
}

export interface Session {
  state: Readable<SessionState>
  /** Почему кабинет не открылся (state failed): для кнопки «Повторить». */
  failure: Readable<View<never>>
  /** Меняется после автоматического входа заново: экраны перечитывают данные. */
  revision: Readable<number>
  me: Loader<MeResponse> | null
  token(): string | null
  /** Для api.onUnauthorized: сессия кончилась. */
  expired(): void
  attach(ep: Endpoints): void
  boot(): Promise<void>
  /** Вход в браузере подтверждён, cookie уже стоит. */
  loggedIn(): Promise<void>
  logout(all: boolean): Promise<void>
}

export function createSession(deps: SessionDeps): Session {
  const state = writable<SessionState>('booting')
  const failure = writable<View<never>>({ status: 'loading' })
  const revision = writable(0)
  let ep: Endpoints | null = null
  let token: string | null = readToken(deps)
  let booting = true
  let relogin: Promise<void> | null = null

  const endpoint = () => {
    if (!ep) throw new Error('session: endpoints are not attached')
    return ep
  }

  function remember(t: string | null) {
    token = t
    try {
      if (t) deps.storage?.setItem(TOKEN_KEY, t)
      else deps.storage?.removeItem(TOKEN_KEY)
    } catch {
      // sessionStorage закрыт (приватный режим): токен живёт только в памяти страницы
    }
  }

  /** Открывает кабинет с тем, что есть. false: сессия не признана (401), решает вызывающий. */
  async function open(): Promise<boolean> {
    const me = createLoader(() => endpoint().me(), { auto: false }) // первый запрос делает reload ниже, а не конструктор
    s.me = me
    await me.reload()
    const v = get(me)
    if (v.status === 'ready') {
      state.set('ready')
      if (v.data.lang !== get(deps.texts.lang)) await reloadTexts()
      return true
    }
    if (v.status === 'error' && v.code === BANNED_CODE) {
      state.set('banned')
      return true
    }
    if (v.status === 'error' && v.code === GONE_CODE) return false
    failure.set(v)
    state.set('failed')
    return true
  }

  async function reloadTexts() {
    try {
      deps.texts.set(await endpoint().texts())
    } catch {
      // прежние тексты остаются: страница работает на них
    }
  }

  async function telegramLogin(): Promise<void> {
    try {
      const r = await endpoint().sessionTg(deps.initData ?? '')
      remember(r.token)
    } catch (e) {
      const v = failView(e)
      if (v.status === 'error' && v.code === BANNED_CODE) return void state.set('banned')
      // Подпись не сошлась или данные запуска старше часа: войти заново без бота нельзя.
      if (v.status === 'error' && v.code === BAD_INIT_CODE) return void state.set('stale')
      failure.set(v)
      return void state.set('failed')
    }
    if (!(await open())) state.set('failed')
  }

  const s: Session = {
    state,
    failure,
    revision,
    me: null,
    token: () => token,
    expired() {
      if (booting) return
      remember(null)
      if (!deps.initData) return void state.set('login')
      // В Telegram вход тихий: подписанные данные запуска те же, сессию просто выдают заново.
      relogin ??= telegramLogin()
        .then(() => void revision.update((n) => n + 1))
        .finally(() => (relogin = null))
    },
    attach(e) {
      ep = e
    },
    async boot() {
      booting = true
      state.set('booting')
      await reloadTexts()
      if (deps.initData) {
        // Токен из прошлой загрузки страницы ещё жив: пробуем его, а новые данные запуска тратим, только если он не признан.
        if (!(token && (await open()))) await telegramLogin()
      } else if (!(await open())) {
        state.set('login')
      }
      booting = false
    },
    async loggedIn() {
      booting = true
      if (!(await open())) state.set('login')
      booting = false
      await reloadTexts()
    },
    async logout(all) {
      try {
        await (all ? endpoint().logoutAll() : endpoint().logout())
      } catch (e) {
        if (!(e instanceof ApiError)) throw e // сеть упала: выйти всё равно надо
      }
      remember(null)
      state.set('login')
    },
  }
  return s
}

function readToken(deps: SessionDeps): string | null {
  if (!deps.initData) return null
  try {
    return deps.storage?.getItem(TOKEN_KEY) ?? null
  } catch {
    return null
  }
}
