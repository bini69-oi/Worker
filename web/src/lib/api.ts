// Запросы к API. Один клиент на кабинет и админку: адрес основы и источник токена задаёт вызывающий.
import type { ErrorBody } from './types.gen'

/** Ошибка, которую вернул сервер: код и текст на языке абонента. 401 это сессия кончилась. */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly body: ErrorBody

  constructor(status: number, body: ErrorBody) {
    super(body.text || body.code)
    this.status = status
    this.code = body.code
    this.body = body
  }

  get money(): ErrorBody['money'] {
    return this.body.money
  }

  get limit(): ErrorBody['limit'] {
    return this.body.limit
  }
}

/** Сервер не ответил или ответил не JSON: страница показывает «Нет связи». */
export class NetworkError extends Error {
  constructor(cause?: unknown) {
    super('network', { cause })
  }
}

export interface ApiOptions {
  /** Адрес основы без слеша на конце, например /api/app. */
  base: string
  fetch?: typeof fetch
  /** Токен сессии для заголовка Authorization. Без него работает cookie. */
  token?: () => string | null
  /** Зовётся на каждый 401: сессия кончилась. */
  onUnauthorized?: () => void
}

export interface Api {
  get<T>(path: string): Promise<T>
  post<T>(path: string, body?: unknown): Promise<T>
  del<T>(path: string): Promise<T>
}

function isErrorBody(v: unknown): v is ErrorBody {
  return typeof v === 'object' && v !== null && typeof (v as ErrorBody).code === 'string'
}

export function createApi(opts: ApiOptions): Api {
  const doFetch = opts.fetch ?? ((...a: Parameters<typeof fetch>) => fetch(...a))

  async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' }
    const token = opts.token?.()
    if (token) headers.Authorization = `Bearer ${token}`
    const init: RequestInit = { method, headers, credentials: 'same-origin' }
    if (method !== 'GET') {
      // Изменяющий запрос с cookie обязан нести оба заголовка: так сервер отличает его от формы чужого сайта.
      headers['X-Worker'] = '1'
      headers['Content-Type'] = 'application/json'
      init.body = JSON.stringify(body ?? {})
    }
    let res: Response
    let data: unknown
    try {
      res = await doFetch(opts.base + path, init)
      data = await res.json()
    } catch (e) {
      throw new NetworkError(e)
    }
    if (!res.ok) {
      if (!isErrorBody(data)) throw new NetworkError(data)
      if (res.status === 401) opts.onUnauthorized?.()
      throw new ApiError(res.status, data)
    }
    return data as T
  }

  return {
    get: (path) => request('GET', path),
    post: (path, body) => request('POST', path, body),
    del: (path) => request('DELETE', path),
  }
}
