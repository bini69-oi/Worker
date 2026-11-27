// Код создан командой make types из структур Go (пакет api). Руками не править.

export interface AdminAudit {
  id: number;
  admin_tg: number;
  action: string;
  target: string;
  details: Record<string, string>;
  ip: string;
  at: string;
}

export interface AdminBalance {
  balance: number;
}

export interface AdminDevice {
  hwid: string;
  platform: string;
  model: string;
}

export interface AdminExpires {
  expires_at: string;
}

export interface AdminLedger {
  amount: number;
  reason: string;
  comment: string;
  admin_tg: number;
  created_at: string;
}

export interface AdminLoginStarted {
  id: string;
  code: string;
}

export interface AdminLoginState {
  status: string;
}

export interface AdminMe {
  tg_id: number;
  owner: boolean;
  perms: string[];
}

export interface AdminPayment {
  id: number;
  user_id: number;
  username: string;
  gateway: string;
  amount: number;
  status: string;
  created_at: string;
  paid_at: string | null;
}

export interface AdminPaymentList {
  items: AdminPayment[];
  next: number;
}

export interface AdminSub {
  tariff: string;
  trial: boolean;
  status: string;
  period_days: number;
  price: number;
  expires_at: string;
  auto_renew: boolean;
  traffic_bytes: number;
  device_limit: number;
}

export interface AdminUserCard {
  id: number;
  tg_id: number;
  username: string;
  lang: string;
  balance: number;
  banned: boolean;
  unreachable: boolean;
  created_at: string;
  referrer_tg: number;
  sub: AdminSub | null;
  ledger: AdminLedger[];
  payments: AdminPayment[];
  audit: AdminAudit[];
  devices: AdminDevice[];
  devices_ok: boolean;
}

export interface AdminUserList {
  items: AdminUserRow[];
  next: number;
}

export interface AdminUserRow {
  id: number;
  tg_id: number;
  username: string;
  status: string;
  banned: boolean;
  balance: number;
  expires_at: string | null;
  created_at: string;
}

export interface ErrorBody {
  code: string;
  text: string;
  detail?: string;
}
