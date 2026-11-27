import { describe, expect, it } from 'vitest'
import { ApiError, NetworkError } from './api'
import { actionKey, newKey, withKey } from './actionKey'
import { golden } from './testing/golden'
import type { ErrorBody } from './types.gen'

const valid = /^[A-Za-z0-9_-]{16,64}$/

describe('newKey', () => {
  it('подходит под формат сервера и не повторяется', () => {
    const seen = new Set<string>()
    for (let i = 0; i < 200; i++) {
      const k = newKey()
      expect(k).toMatch(valid)
      seen.add(k)
    }
    expect(seen.size).toBe(200)
  })
  it('байты с + и / превращаются в безопасные знаки', () => {
    expect(newKey(() => new Uint8Array(16).fill(0xfb))).toMatch(valid)
    expect(newKey(() => new Uint8Array(16).fill(0xff))).toMatch(valid)
  })
})

describe('withKey', () => {
  it('повтор после обрыва связи шлёт тот же ключ', async () => {
    const k = actionKey()
    const keys: string[] = []
    await expect(
      withKey(k, async (key) => {
        keys.push(key)
        throw new NetworkError()
      }),
    ).rejects.toBeInstanceOf(NetworkError)
    await withKey(k, async (key) => void keys.push(key))
    expect(keys[1]).toBe(keys[0])
  })

  it('после ответа сервера следующее действие получает новый ключ', async () => {
    const k = actionKey()
    const keys: string[] = []
    await withKey(k, async (key) => void keys.push(key))
    const e = golden<ErrorBody>('error_not_found')
    await expect(
      withKey(k, async (key) => {
        keys.push(key)
        throw new ApiError(402, e)
      }),
    ).rejects.toBeInstanceOf(ApiError)
    await withKey(k, async (key) => void keys.push(key))
    expect(new Set(keys).size).toBe(3)
  })
})
