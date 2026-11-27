import { get } from 'svelte/store'
import { describe, expect, it, vi } from 'vitest'
import { ApiError, NetworkError } from '../lib/api'
import { createTexts } from '../lib/texts'
import { golden } from '../lib/testing/golden'
import type { ErrorBody, MeResponse, SessionResponse, TextsResponse } from '../lib/types.gen'
import type { Endpoints } from './endpoints'
import { createSession } from './session'

const me = golden<MeResponse>('me_with_sub')
const texts = golden<TextsResponse>('texts_en')

function err(status: number, code: string): ApiError {
  const base = golden<ErrorBody>('error_not_found')
  return new ApiError(status, { ...base, code })
}

function fakeEp(over: Partial<Endpoints> = {}): Endpoints {
  return {
    texts: vi.fn(async () => texts),
    me: vi.fn(async () => me),
    sessionTg: vi.fn(async (): Promise<SessionResponse> => ({ token: 'tg-token', lang: 'ru', expires_at: '2026-03-11T12:00:00Z' })),
    logout: vi.fn(async () => ({})),
    logoutAll: vi.fn(async () => ({})),
    ...over,
  } as unknown as Endpoints
}

function memStorage(init: Record<string, string> = {}) {
  const m = new Map(Object.entries(init))
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k), m }
}

function make(ep: Endpoints, initData: string | null = null, storage = memStorage()) {
  const s = createSession({ initData, storage, texts: createTexts() })
  s.attach(ep)
  return { s, storage }
}

describe('вход в Mini App', () => {
  it('меняет initData на токен, запоминает его и открывает кабинет', async () => {
    const ep = fakeEp()
    const { s, storage } = make(ep, 'query_id=x&hash=y')
    await s.boot()
    expect(ep.sessionTg).toHaveBeenCalledWith('query_id=x&hash=y')
    expect(get(s.state)).toBe('ready')
    expect(s.token()).toBe('tg-token')
    expect(storage.m.get('worker_token')).toBe('tg-token')
  })

  it('забаненный видит блокировку', async () => {
    const ep = fakeEp({ sessionTg: vi.fn(async () => Promise.reject(err(403, 'E3006'))) })
    const { s } = make(ep, 'x')
    await s.boot()
    expect(get(s.state)).toBe('banned')
  })

  it('подделанные данные запуска: кабинет не открывается, есть ошибка для «Повторить»', async () => {
    const ep = fakeEp({ sessionTg: vi.fn(async () => Promise.reject(err(401, 'E6040'))) })
    const { s } = make(ep, 'x')
    await s.boot()
    expect(get(s.state)).toBe('failed')
    expect(get(s.failure)).toMatchObject({ status: 'error', code: 'E6040' })
  })

  it('401 посреди работы: вход тихо повторяется и экраны перечитываются', async () => {
    const ep = fakeEp()
    const { s } = make(ep, 'x')
    await s.boot()
    s.expired()
    s.expired() // второй 401 подряд не запускает второй вход
    await vi.waitFor(() => expect(get(s.revision)).toBe(1))
    expect(ep.sessionTg).toHaveBeenCalledTimes(2)
    expect(get(s.state)).toBe('ready')
  })

  it('токен из прошлой загрузки страницы берётся из sessionStorage', () => {
    const { s } = make(fakeEp(), 'x', memStorage({ worker_token: 'old' }))
    expect(s.token()).toBe('old')
  })
})

describe('вход в браузере', () => {
  it('есть cookie: кабинет открывается, токена в странице нет', async () => {
    const ep = fakeEp()
    const { s } = make(ep)
    await s.boot()
    expect(get(s.state)).toBe('ready')
    expect(s.token()).toBeNull()
    expect(ep.sessionTg).not.toHaveBeenCalled()
  })

  it('нет сессии: нужен вход', async () => {
    const ep = fakeEp({ me: vi.fn(async () => Promise.reject(err(401, 'E6008'))) })
    const { s } = make(ep)
    await s.boot()
    expect(get(s.state)).toBe('login')
  })

  it('401 посреди работы возвращает на вход', async () => {
    const { s } = make(fakeEp())
    await s.boot()
    s.expired()
    expect(get(s.state)).toBe('login')
  })

  it('401 при самой загрузке не считается «сессия кончилась» дважды', async () => {
    const ep = fakeEp({
      me: vi.fn(async () => {
        s.expired() // api зовёт onUnauthorized на 401 до того, как boot разберёт ответ
        return Promise.reject(err(401, 'E6008'))
      }),
    })
    const { s } = make(ep)
    await s.boot()
    expect(get(s.state)).toBe('login')
  })

  it('сеть недоступна: «Нет связи» с повтором, а не вход', async () => {
    const ep = fakeEp({ me: vi.fn(async () => Promise.reject(new NetworkError())) })
    const { s } = make(ep)
    await s.boot()
    expect(get(s.state)).toBe('failed')
    expect(get(s.failure)).toEqual({ status: 'offline' })
  })

  it('после подтверждения входа кабинет открывается', async () => {
    let signedIn = false
    const ep = fakeEp({ me: vi.fn(async () => (signedIn ? me : Promise.reject(err(401, 'E6008')))) })
    const { s } = make(ep)
    await s.boot()
    signedIn = true
    await s.loggedIn()
    expect(get(s.state)).toBe('ready')
  })

  it('выход: просит сервер, забывает токен и показывает вход, даже если сеть упала', async () => {
    const ep = fakeEp({ logout: vi.fn(async () => Promise.reject(err(401, 'E6008'))) })
    const { s } = make(ep)
    await s.boot()
    await s.logout(false)
    expect(get(s.state)).toBe('login')
    await s.logout(true)
    expect(ep.logoutAll).toHaveBeenCalled()
  })
})

describe('тексты', () => {
  it('после входа перечитываются, если язык абонента не тот, что был', async () => {
    const ep = fakeEp()
    const { s } = make(ep)
    await s.boot()
    // me.lang = ru, а тексты пришли английские: страница просит их заново уже с сессией
    expect(ep.texts).toHaveBeenCalledTimes(2)
    expect(get(s.state)).toBe('ready')
  })
})
