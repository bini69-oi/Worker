import type { AdminLoginStarted, AdminLoginState } from '../lib/types.gen'
import type { Api } from './api'

export type LoginResult = 'ok' | 'denied' | 'expired'

// Вход: пароль, показ кода, опрос. onCode зовётся один раз с кодом, который админ выбирает в Telegram.
// Опрос идёт раз в delayMs, не дольше tries раз: страница не висит вечно.
export async function runLogin(
  api: Api,
  tgId: number,
  password: string,
  onCode: (code: string) => void,
  sleep: (ms: number) => Promise<void> = (ms) => new Promise((r) => setTimeout(r, ms)),
  tries = 150,
  delayMs = 2000,
): Promise<LoginResult> {
  const started = await api<AdminLoginStarted>('POST', '/login', { tg_id: tgId, password })
  onCode(started.code)
  for (let i = 0; i < tries; i++) {
    await sleep(delayMs)
    const st = await api<AdminLoginState>('GET', '/login/' + encodeURIComponent(started.id))
    if (st.status === 'ok' || st.status === 'denied' || st.status === 'expired') return st.status
  }
  return 'expired'
}
