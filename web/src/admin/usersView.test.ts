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
