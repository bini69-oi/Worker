import { describe, expect, it } from 'vitest'
import type { Api } from './api'
import { runLogin } from './loginFlow'

function fakeApi(states: string[]): { api: Api; calls: string[] } {
  const calls: string[] = []
  let i = 0
  const api = (async (method: string, path: string) => {
    calls.push(`${method} ${path}`)
    if (path === '/login') return { id: '7.nonce', code: '42' }
    return { status: states[Math.min(i++, states.length - 1)] }
  }) as Api
  return { api, calls }
}

describe('вход с кодом', () => {
  it('показывает код и ждёт нажатия в Telegram', async () => {
    const { api, calls } = fakeApi(['pending', 'pending', 'ok'])
    const codes: string[] = []
    const res = await runLogin(api, 1, 'pw', (c) => codes.push(c), async () => {})
    expect(res).toBe('ok')
    expect(codes).toEqual(['42'])
    expect(calls).toEqual(['POST /login', 'GET /login/7.nonce', 'GET /login/7.nonce', 'GET /login/7.nonce'])
  })

  it('отказ в Telegram', async () => {
    const { api } = fakeApi(['denied'])
    expect(await runLogin(api, 1, 'pw', () => {}, async () => {})).toBe('denied')
  })

  it('не опрашивает вечно', async () => {
    const { api, calls } = fakeApi(['pending'])
    expect(await runLogin(api, 1, 'pw', () => {}, async () => {}, 3)).toBe('expired')
    expect(calls.length).toBe(4)
  })
})
