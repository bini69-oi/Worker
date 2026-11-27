import type { AdminGateway, AdminSetting, AdminSettingResult, AdminText } from '../lib/types.gen'
import { dt } from './format'

// settingView это настройка для экрана. У секрета поле ввода пустое, а маска идёт подсказкой.
export function settingView(s: AdminSetting) {
  return {
    key: s.key,
    desc: s.desc,
    group: s.group,
    kind: s.kind,
    input: s.secret ? '' : s.value,
    placeholder: s.secret ? (s.value ? `сейчас ${s.value}` : 'не задан') : '',
    inputType: s.secret ? 'password' : s.kind === 'int' || s.kind === 'money' ? 'number' : 'text',
    isBool: s.kind === 'bool',
    changed: s.changed,
    def: s.secret ? '' : s.default,
    needsYes: s.confirm,
    secret: s.secret,
  }
}

// settingNote говорит, что произошло после правки: применилось или ждёт «Да» в Telegram.
export function settingNote(res: AdminSettingResult): string {
  return res.pending
    ? 'Подтвердите в Telegram: до «Да» действует старое значение. Запрос живёт 2 минуты.'
    : 'Сохранено, применится в течение 5 секунд'
}

export function textView(t: AdminText) {
  return {
    key: t.key,
    params: t.params.map((p) => `{${p}}`),
    ru: t.ru,
    en: t.en,
    changedRu: t.changed_ru,
    changedEn: t.changed_en,
    defaultRu: t.default_ru,
    defaultEn: t.default_en,
    // Подпись кнопки до 64 знаков, сообщение до 4096.
    limit: t.key.startsWith('btn.') ? 64 : 4096,
  }
}

export function gatewayView(g: AdminGateway) {
  return {
    name: g.name,
    title: g.title,
    state: g.enabled ? (g.configured ? 'включена' : 'включена, но ключей нет') : 'выключена',
    keys: g.configured ? 'ключи заполнены' : 'ключи не заполнены',
    canEnable: g.configured,
    lastPaid: g.last_paid_at ? dt(g.last_paid_at) : 'оплат не было',
    pending: g.pending,
    enabled: g.enabled,
    settings: g.settings.map(settingView),
  }
}
