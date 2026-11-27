// Код создан командой make types из структур Go (пакет api). Руками не править.

export interface ErrorBody {
  code: string;
  text: string;
}

export interface GatewayView {
  name: string;
  title: string;
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

export interface NextPlanView {
  tariff_id: number;
  tariff: string;
  days: number;
  starts_at: string;
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

export interface TextsResponse {
  lang: string;
  texts: Record<string, string>;
}
