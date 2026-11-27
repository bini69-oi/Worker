import { describe, expect, it } from 'vitest'
import type { AdminGatewayList, AdminSettingList, AdminSettingResult, AdminTextList } from '../lib/types.gen'
import { golden } from '../lib/testing/golden'
import { gatewayView, settingNote, settingView, textView } from './configView'

describe('настройки бота на эталонах API', () => {
  it('число дней пробного изменено и применяется сразу', () => {
    const s = golden<AdminSettingList>('admin_settings').items[0]!
    const v = settingView(s)
    expect(v.key).toBe('trial.days')
    expect(v.input).toBe('7')
    expect(v.def).toBe('3')
    expect(v.changed).toBe(true)
    expect(v.needsYes).toBe(false)
    expect(v.inputType).toBe('number')
  })

  it('секрет: поле пустое, значение только маской, нужно «Да»', () => {
    const v = settingView({ key: 'yookassa.secret_key', kind: 'string', group: 'pay', desc: '', default: '', value: '••••XYZ1',
      changed: true, secret: true, confirm: true })
    expect(v.input).toBe('')
    expect(v.placeholder).toContain('••••XYZ1')
    expect(v.inputType).toBe('password')
    expect(v.needsYes).toBe(true)
  })

  it('ответ о смене: ждём «Да» или применено', () => {
    const wait: AdminSettingResult = { pending: true, confirm_id: 5 }
    expect(settingNote(wait)).toContain('Подтвердите в Telegram')
    expect(settingNote({ pending: false, confirm_id: 0 })).toContain('5 секунд')
  })

  it('текст: подстановки и правка рядом с умолчанием', () => {
    const t = textView(golden<AdminTextList>('admin_texts').items[0]!)
    expect(t.key).toBe('buy.success')
    expect(t.params).toEqual(['{days}'])
    expect(t.changedRu).toBe(true)
    expect(t.changedEn).toBe(false)
    expect(t.ru).not.toBe(t.defaultRu)
    expect(t.limit).toBe(4096)
    expect(textView({ ...golden<AdminTextList>('admin_texts').items[0]!, key: 'btn.ban' }).limit).toBe(64)
  })

  it('кассы: состояние, ключи, последняя оплата', () => {
    const list = golden<AdminGatewayList>('admin_gateways').items.map(gatewayView)
    const manual = list.find((g) => g.name === 'manual')!
    expect(manual.state).toBe('включена')
    expect(manual.keys).toBe('ключи заполнены')
    expect(manual.lastPaid).toBe('оплат не было')
    expect(manual.settings.some((s) => s.key === 'manual.details' && s.needsYes)).toBe(true)
  })
})
