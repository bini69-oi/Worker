import { describe, expect, it, vi } from 'vitest'
import { ApiError, NetworkError, createApi } from './api'
import { golden } from './testing/golden'
import type { ErrorBody, MeResponse } from './types.gen'

/** Фейковый fetch: отдаёт один ответ и запоминает запрос. */
function fakeFetch(status: number, body: unknown) {
  const calls: { url: string; init: RequestInit }[] = []
  const f = vi.fn(async (url: string, init: RequestInit) => {
    calls.push({ url, init })
    return new Response(typeof body === 'string' ? body : JSON.stringify(body), { status })
  })
  return { f: f as unknown as typeof fetch, calls }
}

describe('lib/api', () => {
  it('читает ответ сервера как есть', async () => {
    const me = golden<MeResponse>('me_with_sub')
    const { f, calls } = fakeFetch(200, me)
    const api = createApi({ base: '/api/app', fetch: f })
    const got = await api.get<MeResponse>('/me')
    expect(got.sub?.tariff).toBe('Месяц')
    expect(calls[0]?.url).toBe('/api/app/me')
    expect(calls[0]?.init.method).toBe('GET')
  })

  it('на изменяющий запрос ставит X-Worker и JSON, на чтение нет', async () => {
    const { f, calls } = fakeFetch(200, {})
    const api = createApi({ base: '/api/app', fetch: f })
    await api.get('/me')
    await api.post('/promo', { code: 'ABC' })
    await api.del('/devices/x')
    const [get, post, del] = calls.map((c) => c.init)
    expect((get?.headers as Record<string, string>)['X-Worker']).toBeUndefined()
    expect((post?.headers as Record<string, string>)['X-Worker']).toBe('1')
    expect((post?.headers as Record<string, string>)['Content-Type']).toBe('application/json')
    expect(post?.body).toBe('{"code":"ABC"}')
    expect(del?.body).toBe('{}')
    expect(post?.credentials).toBe('same-origin')
  })

  it('кладёт токен в Authorization, если он есть', async () => {
    const { f, calls } = fakeFetch(200, {})
    let token: string | null = null
    const api = createApi({ base: '/api/app', fetch: f, token: () => token })
    await api.get('/me')
    token = 'tok'
    await api.get('/me')
    expect((calls[0]?.init.headers as Record<string, string>).Authorization).toBeUndefined()
    expect((calls[1]?.init.headers as Record<string, string>).Authorization).toBe('Bearer tok')
  })

  it('ошибка сервера становится ApiError с кодом и текстом', async () => {
    const e = golden<ErrorBody>('error_not_found')
    const { f } = fakeFetch(404, e)
    const api = createApi({ base: '/api/app', fetch: f })
    const err = await api.get('/x').catch((x: unknown) => x)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(404)
    expect((err as ApiError).code).toBe('E1001')
    expect((err as ApiError).message).toBe(e.text)
  })

  it('недостача денег приходит вместе с цифрами', async () => {
    const e = golden<ErrorBody>('buy_need_money')
    const { f } = fakeFetch(402, e)
    const api = createApi({ base: '/api/app', fetch: f })
    const err = (await api.post('/buy', {}).catch((x: unknown) => x)) as ApiError
    expect(err.code).toBe('E2001')
    expect(err.money).toEqual({ price: 25000, balance: 10000, missing: 15000 })
  })

  it('на 401 зовёт onUnauthorized, на другие ошибки нет', async () => {
    const onUnauthorized = vi.fn()
    const gone = golden<ErrorBody>('error_not_found')
    const api401 = createApi({ base: '/api/app', fetch: fakeFetch(401, { ...gone, code: 'E6008' }).f, onUnauthorized })
    await expect(api401.get('/me')).rejects.toBeInstanceOf(ApiError)
    expect(onUnauthorized).toHaveBeenCalledTimes(1)
    const api404 = createApi({ base: '/api/app', fetch: fakeFetch(404, gone).f, onUnauthorized })
    await expect(api404.get('/me')).rejects.toBeInstanceOf(ApiError)
    expect(onUnauthorized).toHaveBeenCalledTimes(1)
  })

  it('нет связи: сбой сети, не JSON и ответ без кода дают NetworkError', async () => {
    const down = createApi({
      base: '/api/app',
      fetch: (async () => {
        throw new TypeError('Failed to fetch')
      }) as unknown as typeof fetch,
    })
    await expect(down.get('/me')).rejects.toBeInstanceOf(NetworkError)
    const html = createApi({ base: '/api/app', fetch: fakeFetch(502, '<html>bad gateway</html>').f })
    await expect(html.get('/me')).rejects.toBeInstanceOf(NetworkError)
    const odd = createApi({ base: '/api/app', fetch: fakeFetch(500, { nope: 1 }).f })
    await expect(odd.get('/me')).rejects.toBeInstanceOf(NetworkError)
  })
})
