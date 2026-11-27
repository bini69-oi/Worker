import type { ErrorBody } from '../lib/types.gen'

// Ошибка ответа API: статус, код и текст с сервера, поясняющее detail (какой тег не разрешён и т.п.).
export class ApiError extends Error {
  constructor(
    public status: number,
    public body: ErrorBody,
  ) {
    super(body.text || body.code)
  }
  get detail(): string {
    return this.body.detail ?? ''
  }
}

export type Fetch = typeof fetch

// Клиент API админки. Изменяющие запросы несут X-Worker и JSON, как требует сервер; cookie ставит браузер сам.
export function client(f: Fetch = (...a) => fetch(...a)) {
  return async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
    const init: RequestInit = { method, credentials: 'same-origin', headers: {} }
    if (method !== 'GET') {
      init.headers = { 'X-Worker': '1', 'Content-Type': 'application/json' }
      init.body = JSON.stringify(body ?? {})
    }
    const res = await f('/api/admin' + path, init)
    const text = await res.text()
    let data: unknown = {}
    try {
      data = text ? JSON.parse(text) : {}
    } catch {
      throw new ApiError(res.status, { code: '', text: 'Ответ сервера не разобран' })
    }
    if (!res.ok) throw new ApiError(res.status, data as ErrorBody)
    return data as T
  }
}

export type Api = ReturnType<typeof client>
export const api: Api = client()

// qs собирает строку запроса без пустых значений.
export function qs(params: Record<string, string | number | undefined>): string {
  const p = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '' && v !== 0) p.set(k, String(v))
  }
  const s = p.toString()
  return s ? '?' + s : ''
}

// errText это текст ошибки для экрана: что сказал сервер и пояснение.
export function errText(e: unknown): string {
  if (e instanceof ApiError) return e.detail ? `${e.message}: ${e.detail}` : e.message
  return 'Не получилось связаться с сервером'
}
