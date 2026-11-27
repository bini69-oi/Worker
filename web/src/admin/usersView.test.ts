import { describe, expect, it } from 'vitest'
import type { AdminUserCard, AdminUserList } from '../lib/types.gen'
import { golden } from '../lib/testing/golden'
import { cardView, rowView } from './usersView'
import { parseRub, rub } from './format'

describe('абоненты на эталонах API', () => {
  it('строка поиска', () => {
    const list = golden<AdminUserList>('admin_users_list')
    const v = list.items.map(rowView)
    expect(v.length).toBeGreaterThan(0)
    const ivan = v.find((r) => r.name === '@ivan')
    expect(ivan?.status).toBe('есть подписка')
    expect(ivan?.balance).toMatch(/₽$/)
    expect(list.items.every((r) => r.id > 0)).toBe(true)
  })

  it('карточка с устройствами', () => {
    const v = cardView(golden<AdminUserCard>('admin_user_card'))
    expect(v.title).toBe('@ivan')
    expect(v.sub?.tariff).toBe('Месяц')
    expect(v.devices).toEqual(['hw-1'])
    expect(v.devicesNote).toBe('')
  })

  it('панель не ответила', () => {
    const c = golden<AdminUserCard>('admin_user_card')
    const v = cardView({ ...c, devices_ok: false, devices: [] })
    expect(v.devices).toBeNull()
    expect(v.devicesNote).toBe('Панель не ответила')
  })
})

describe('подписка в карточке: смена, докупка, перенос', () => {
  it('ждущая смена тарифа и докупленное', () => {
    const v = cardView(golden<AdminUserCard>('admin_user_card_extras')).sub!
    expect(v.queued).toMatch(/^Год, 365 дн., .* уже списано$/)
    expect(v.extras).toHaveLength(2)
    expect(v.extras[0]).toMatch(/^трафик \+10 ГБ до /)
    expect(v.extras[1]).toBe('устройства +2')
    expect(v.imported).toBe('')
  })

  it('перенесённая подписка с лимитами из панели', () => {
    const v = cardView(golden<AdminUserCard>('admin_user_card_imported')).sub!
    expect(v.imported).toBe('перенесён из панели (old_moved): 50 ГБ, устройств 3')
    expect(v.traffic).toBe('50 ГБ')
    expect(v.queued).toBe('')
    expect(v.extras).toEqual([])
  })

  it('обычная подписка без пометок', () => {
    const v = cardView(golden<AdminUserCard>('admin_user_card')).sub!
    expect([v.queued, v.imported, v.extras.length]).toEqual(['', '', 0])
  })
})

describe('деньги', () => {
  it('копейки в рубли и обратно', () => {
    expect(rub(12345)).toBe('123,45 ₽')
    expect(rub(-5)).toBe('−0,05 ₽')
    expect(parseRub('100')).toBe(10000)
    expect(parseRub('100,5')).toBe(10050)
    expect(parseRub('-20.25')).toBe(-2025)
    expect(parseRub('много')).toBeNull()
    expect(parseRub('1.234')).toBeNull()
    expect(parseRub('')).toBeNull()
  })
})
