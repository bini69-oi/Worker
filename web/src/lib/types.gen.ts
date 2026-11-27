// Код создан командой make types из структур Go (пакет api). Руками не править.

export interface AppView {
  id: string;
  name: string;
  stores: Record<string, string>;
  open_url: string;
}

export interface BuyResponse {
  sub: SubView;
  queued: boolean;
}

export interface ConnectResponse {
  sub_url: string;
  apps: AppView[];
}

export interface DeviceView {
  hwid: string;
  platform: string;
  model: string;
  created_at: string;
}

export interface DevicesResponse {
  devices: DeviceView[];
}

export interface ErrorBody {
  code: string;
  text: string;
  money?: MoneyDetails | null;
  limit?: LimitDetails | null;
}

export interface GatewayView {
  name: string;
  title: string;
}

export interface HistoryItem {
  amount: number;
  reason: string;
  created_at: string;
}

export interface HistoryResponse {
  items: HistoryItem[];
}

export interface LimitDetails {
  min: number;
  max: number;
}

export interface LoginPollResponse {
  state: string;
}

export interface LoginStartResponse {
  id: string;
  code: string;
  link: string;
  expires_at: string;
}

export interface MeResponse {
  lang: string;
  username: string;
  balance: number;
  maintenance: boolean;
  trial_available: boolean;
  sub: SubView | null;
  gateways: GatewayView[];
  bot_link: string;
}

export interface MoneyDetails {
  price: number;
  balance: number;
  missing: number;
}

export interface NextPlanView {
  tariff_id: number;
  tariff: string;
  days: number;
  starts_at: string;
}

export interface PaymentResponse {
  id: number;
  gateway: string;
  amount: number;
  status: string;
  created_at: string;
  paid_at: string | null;
}

export interface PeriodView {
  days: number;
  price: number;
}

export interface PromoResponse {
  kind: string;
  value: number;
}

export interface QuoteResponse {
  price: number;
  balance: number;
  missing: number;
  queued: boolean;
  starts_at: string | null;
}

export interface RefResponse {
  link: string;
  invited: number;
  bought: number;
  earned: number;
}

export interface SessionResponse {
  token: string;
  lang: string;
  expires_at: string;
}

export interface SubView {
  state: string;
  trial: boolean;
  imported: boolean;
  tariff_id: number;
  tariff: string;
  expires_at: string;
  days_left: number;
  auto_renew: boolean;
  traffic_bytes: number;
  devices: number;
  extra_traffic_bytes: number;
  extra_devices: number;
  next: NextPlanView | null;
}

export interface TariffView {
  id: number;
  name: string;
  traffic_bytes: number;
  devices: number;
  periods: PeriodView[];
}

export interface TariffsResponse {
  tariffs: TariffView[];
}

export interface TextsResponse {
  lang: string;
  texts: Record<string, string>;
}

export interface TopupResponse {
  payment_id: number;
  gateway: string;
  url: string;
  invoice_link: string;
  details: string;
  ext_amount: number;
  ext_currency: string;
}
