import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, NetworkError } from '../lib/api'
import { golden } from '../lib/testing/golden'
import { flushSync, render } from '../lib/testing/mountTexts'
import type { ErrorBody, LoginPollResponse, LoginStartResponse } from '../lib/types.gen'
import Login from './Login.svelte'

const start = golden<LoginStartResponse>('login_start')
const dict = {
  'app.login_title': 'Вход',
  'app.login_rejected': 'Вход отклонён, начните заново',
  'app.login_expired': 'Время вышло, начните заново',
  'app.login_restart': 'Начать заново',
  'app.login_button': 'Войти через Telegram',
  'app.login_waiting': 'Ждём подтверждения',
}

let stop: (() => void) | undefined
beforeEach(() => {
  vi.useFakeTimers({ now: new Date('2026-03-10T12:00:00Z') })
})
afterEach(() => {
  stop?.()
  vi.useRealTimers()
})

function show(polls: LoginPollResponse[], over: Record<string, unknown> = {}) {
  const queue = [...polls]
  const ep = {
    loginStart: vi.fn(async () => start),
    loginPoll: vi.fn(async () => queue.shift() ?? golden<LoginPollResponse>('login_pending')),
    ...over,
  }
  const onDone = vi.fn()
  const r = render(Login, { ep, onDone, every: 1000 } as never, dict)
  stop = r.unmount
  return { ...r, ep, onDone }
}

async function tick(ms: number) {
  await vi.advanceTimersByTimeAsync(ms)
  flushSync()
}

describe('Login: вход в браузере', () => {
  it('показывает код и кнопку на бота, скрипт Telegram не нужен', async () => {
    const { root } = show([])
    await tick(0)
    expect(root.querySelector('[data-testid=code]')?.textContent).toBe('42')
    const a = root.querySelector('a.btn') as HTMLAnchorElement
    expect(a.href).toBe('https://t.me/worker_bot?start=web_NONCE')
    expect(a.rel).toContain('noopener')
    expect(document.querySelector('script[src*="telegram"]')).toBeNull()
  })

  it('опрашивает раз в паузу, подтверждённый вход зовёт onDone', async () => {
    const { ep, onDone } = show([golden('login_pending'), golden('login_confirmed')])
    await tick(0)
    await tick(1000)
    expect(onDone).not.toHaveBeenCalled()
    await tick(1000)
    expect(ep.loginPoll).toHaveBeenCalledWith('LOGIN-ID')
    expect(onDone).toHaveBeenCalledTimes(1)
    await tick(5000)
    expect(ep.loginPoll).toHaveBeenCalledTimes(2) // после входа опрос остановлен
  })

  it('отклонённый вход: сообщение и «Начать заново»', async () => {
    const { root, ep } = show([golden('login_rejected')])
    await tick(0)
    await tick(1000)
    expect(root.textContent).toContain('Вход отклонён, начните заново')
    expect(root.querySelector('[data-testid=code]')).toBeNull()
    ;(root.querySelector('button') as HTMLButtonElement).click()
    await tick(0)
    expect(ep.loginStart).toHaveBeenCalledTimes(2)
    expect(root.querySelector('[data-testid=code]')).not.toBeNull()
  })

  it('истёкший запрос: предлагает начать заново', async () => {
    const { root } = show([golden('login_expired')])
    await tick(0)
    await tick(1000)
    expect(root.textContent).toContain('Время вышло, начните заново')
  })

  it('по часам тоже истекает, если ответа не было', async () => {
    const { root } = show([])
    await tick(0)
    await tick(5 * 60 * 1000 + 2000)
    expect(root.textContent).toContain('Время вышло, начните заново')
  })

  it('связь моргнула во время опроса: опрос продолжается', async () => {
    const polls = [() => Promise.reject(new NetworkError()), () => Promise.resolve(golden<LoginPollResponse>('login_confirmed'))]
    const ep = { loginStart: vi.fn(async () => start), loginPoll: vi.fn(() => (polls.shift() ?? polls[0])!()) }
    const onDone = vi.fn()
    stop = render(Login, { ep, onDone, every: 1000 } as never, dict).unmount
    await tick(0)
    await tick(1000)
    await tick(1000)
    expect(onDone).toHaveBeenCalled()
  })

  it('запуск входа не удался: текст ошибки сервера и повтор', async () => {
    const e = golden<ErrorBody>('error_not_found')
    const { root, ep } = show([], { loginStart: vi.fn().mockRejectedValueOnce(new ApiError(429, { ...e, text: 'Слишком много попыток' })).mockResolvedValue(start) })
    await tick(0)
    expect(root.textContent).toContain('Слишком много попыток')
    ;(root.querySelector('button') as HTMLButtonElement).click()
    await tick(0)
    expect(root.querySelector('[data-testid=code]')).not.toBeNull()
    expect(ep.loginStart).toHaveBeenCalledTimes(2)
  })
})
