import type { AdminMe } from '../lib/types.gen'
import { api, ApiError } from './api'

// Кто вошёл. null значит не вошёл; loaded ложно, пока не спросили сервер.
export const session = $state<{ me: AdminMe | null; loaded: boolean }>({ me: null, loaded: false })

export async function loadMe(): Promise<void> {
  try {
    session.me = await api<AdminMe>('GET', '/me')
  } catch (e) {
    if (!(e instanceof ApiError) || e.status !== 401) throw e
    session.me = null
  }
  session.loaded = true
}

export async function logout(): Promise<void> {
  await api('POST', '/logout')
  session.me = null
}
