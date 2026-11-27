// Код создан командой make types из структур Go (пакет api). Руками не править.

export interface AddonsView {
  traffic: PackView[];
  device_price: number;
}

export interface AdminAudit {
  id: number;
  admin_tg: number;
  action: string;
  target: string;
  details: Record<string, string>;
  ip: string;
  at: string;
}

export interface AdminAuditList {
  items: AdminAudit[];
  next: number;
}

export interface AdminBalance {
  balance: number;
}

export interface AdminBroadcast {
  id: number;
  segment: string;
  text: string;
  button_text: string;
  button_url: string;
  status: string;
  total: number;
  sent: number;
  failed: number;
  blocked: number;
  created_at: string;
  started_at: string | null;
  done_at: string | null;
}

export interface AdminBroadcastList {
  items: AdminBroadcast[];
  next: number;
}

export interface AdminChatCode {
  code: string;
}

export interface AdminDay {
  day: string;
  gross: number;
  refunded: number;
  revenue: number;
}

export interface AdminDevice {
  hwid: string;
  platform: string;
  model: string;
}

export interface AdminExpires {
  expires_at: string;
}

export interface AdminGateway {
  name: string;
  title: string;
  enabled: boolean;
  configured: boolean;
  sort: number;
  last_paid_at: string | null;
  pending: number;
  settings: AdminSetting[];
}

export interface AdminGatewayList {
  items: AdminGateway[];
}

export interface AdminGatewayRevenue {
  gateway: string;
  revenue: number;
}

export interface AdminImportReason {
  reason: string;
  count: number;
}

export interface AdminImportReport {
  subs: number;
  new_users: number;
  reasons: AdminImportReason[];
  skipped: AdminImportSkip[];
}

export interface AdminImportSkip {
  username: string;
  reason: string;
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

export interface AdminMaintenance {
  on: boolean;
  text: string;
}

export interface AdminMe {
  tg_id: number;
  owner: boolean;
  perms: string[];
}

export interface AdminMember {
  tg_id: number;
  owner: boolean;
  perms: string[];
  disabled: boolean;
}

export interface AdminNotify {
  bound: boolean;
  topics: AdminTopic[];
}

export interface AdminOwnLimits {
  username: string;
  traffic_bytes: number;
  devices: number;
  reset: string;
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

export interface AdminPeriod {
  days: number;
  price: number;
}

export interface AdminPromo {
  id: number;
  code: string;
  kind: string;
  value: number;
  tariff_id: number;
  max_uses: number;
  uses: number;
  new_only: boolean;
  expires_at: string | null;
  active: boolean;
  link: string;
}

export interface AdminPromoList {
  items: AdminPromo[];
  next: number;
}

export interface AdminSegmentCount {
  count: number;
}

export interface AdminSession {
  id: string;
  admin_tg: number;
  ip: string;
  agent: string;
  created_at: string;
  seen_at: string;
  current: boolean;
}

export interface AdminSessionList {
  items: AdminSession[];
}

export interface AdminSetting {
  key: string;
  kind: string;
  group: string;
  desc: string;
  default: string;
  value: string;
  changed: boolean;
  secret: boolean;
  confirm: boolean;
}

export interface AdminSettingList {
  items: AdminSetting[];
}

export interface AdminSettingResult {
  pending: boolean;
  confirm_id: number;
}

export interface AdminSquad {
  uuid: string;
  name: string;
  members: number;
}

export interface AdminStats {
  from: string;
  to: string;
  days: AdminDay[];
  gateways: AdminGatewayRevenue[];
  revenue: number;
  new_users: number;
  trials: number;
  first_paid: number;
  active_subs: number;
  stuck_tasks: number;
  stuck_users: number[];
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
  next_tariff: string;
  next_days: number;
  next_price: number;
  extra_traffic_bytes: number;
  extra_traffic_until: string | null;
  extra_devices: number;
  imported: boolean;
  own: AdminOwnLimits | null;
}

export interface AdminTariff {
  id: number;
  name: string;
  traffic_bytes: number;
  devices: number;
  reset: string;
  squads: string[];
  active: boolean;
  sort: number;
  periods: AdminPeriod[];
}

export interface AdminTariffList {
  tariffs: AdminTariff[];
  squads: AdminSquad[];
  squads_ok: boolean;
}

export interface AdminTeam {
  items: AdminMember[];
}

export interface AdminText {
  key: string;
  params: string[];
  default_ru: string;
  default_en: string;
  ru: string;
  en: string;
  changed_ru: boolean;
  changed_en: boolean;
}

export interface AdminTextList {
  items: AdminText[];
}

export interface AdminTopic {
  id: string;
  title: string;
  enabled: boolean;
  locked: boolean;
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
  detail?: string;
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
  topup_presets: number[];
  addons: AddonsView;
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

export interface PackView {
  gb: number;
  price: number;
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

export interface TrafficRequest {
  gb: number;
  key: string;
}
