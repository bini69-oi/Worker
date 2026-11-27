// Все вызовы API кабинета. Тела запросов описаны здесь, ответы берутся из типов, сделанных из Go (types.gen.ts).
import type { Api } from '../lib/api'
import { loadTexts } from '../lib/texts'
import type {
  BuyResponse,
  ConnectResponse,
  DevicesResponse,
  HistoryResponse,
  LoginPollResponse,
  LoginStartResponse,
  MeResponse,
  PaymentResponse,
  PromoResponse,
  QuoteResponse,
  RefResponse,
  SessionResponse,
  SubView,
  TariffsResponse,
  TopupResponse,
} from '../lib/types.gen'

export function endpoints(api: Api) {
  return {
    texts: (lang?: string) => loadTexts(api, lang),
    me: () => api.get<MeResponse>('/me'),
    sessionTg: (initData: string) => api.post<SessionResponse>('/session/tg', { init_data: initData }),
    loginStart: () => api.post<LoginStartResponse>('/login', {}),
    loginPoll: (id: string) => api.get<LoginPollResponse>(`/login/${encodeURIComponent(id)}`),
    logout: () => api.post<Record<string, never>>('/logout'),
    logoutAll: () => api.post<Record<string, never>>('/logout-all'),
    trial: () => api.post<SubView>('/trial'),
    tariffs: () => api.get<TariffsResponse>('/tariffs'),
    quote: (tariffId: number, days: number) => api.post<QuoteResponse>('/quote', { tariff_id: tariffId, days }),
    buy: (tariffId: number, days: number) => api.post<BuyResponse>('/buy', { tariff_id: tariffId, days }),
    topup: (gateway: string, amount: number) => api.post<TopupResponse>('/topup', { gateway, amount }),
    payment: (id: number) => api.get<PaymentResponse>(`/payments/${id}`),
    history: () => api.get<HistoryResponse>('/history'),
    ref: () => api.get<RefResponse>('/ref'),
    promo: (code: string) => api.post<PromoResponse>('/promo', { code }),
    autorenew: (on: boolean) => api.post<SubView>('/autorenew', { on }),
    connect: () => api.get<ConnectResponse>('/connect'),
    devices: () => api.get<DevicesResponse>('/devices'),
    deleteDevice: (hwid: string) => api.del<Record<string, never>>(`/devices/${encodeURIComponent(hwid)}`),
    resetDevices: () => api.post<Record<string, never>>('/devices/reset'),
    revokeLink: () => api.post<ConnectResponse>('/link/revoke'),
    cancelPlan: () => api.post<SubView>('/plan/cancel'),
    buyTraffic: (gb: number) => api.post<SubView>('/addons/traffic', { gb }),
    buyDevice: () => api.post<SubView>('/addons/device'),
  }
}

export type Endpoints = ReturnType<typeof endpoints>
