// Сессия страницы: как войти (Mini App по initData, браузер через бота), что делать на 401, когда выходить.
import { get, writable, type Readable } from 'svelte/store'
import { ApiError } from '../lib/api'
import type { createTexts } from '../lib/texts'
import { createLoader, failView, type Loader, type View } from '../lib/view'
import type { MeResponse } from '../lib/types.gen'
import type { Endpoints } from './endpoints'

/** booting: узнаём, кто это. login: нужен вход в браузере. ready: кабинет открыт. banned: абонент заблокирован. failed: не вышло открыть. */
export type SessionState = 'booting' | 'login' | 'ready' | 'banned' | 'failed'

const TOKEN_KEY = 'worker_token'
const BANNED_CODE = 'E3006'

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

  async function open(): Promise<void> {
    const me = createLoader(() => endpoint().me())
    s.me = me
    await me.reload()
    const v = get(me)
    if (v.status === 'ready') {
      state.set('ready')
      if (v.data.lang !== get(deps.texts.lang)) await reloadTexts()
      return
    }
    if (v.status === 'error' && v.code === BANNED_CODE) return void state.set('banned')
    if (v.status === 'error' && v.code === 'E6008') return void state.set(deps.initData ? 'failed' : 'login')
    failure.set(v)
    state.set('failed')
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
      failure.set(v)
      return void state.set('failed')
    }
    await open()
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
      if (deps.initData) await telegramLogin()
      else await open()
      booting = false
    },
    async loggedIn() {
      booting = true
      await open()
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
