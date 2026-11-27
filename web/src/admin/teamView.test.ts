import { describe, expect, it } from 'vitest'
import type { AdminAuditList, AdminTeam } from '../lib/types.gen'
import { golden } from '../lib/testing/golden'
import { actionTitle, auditView, memberView } from './teamView'

describe('команда и журнал на эталонах API', () => {
  it('владелец и помощник с правами', () => {
    const team = golden<AdminTeam>('admin_team').items.map(memberView)
    expect(team[0]).toMatchObject({ role: 'владелец', perms: 'все права', canRemove: false })
    const helper = team.find((m) => m.tg === 71)!
    expect(helper.role).toBe('помощник')
    expect(helper.perms).toBe('Абоненты: поиск и карточка, Сообщения абоненту')
    expect(helper.canRemove).toBe(true)
  })

  it('журнал: кто, кому, сколько дней и когда', () => {
    const row = auditView(golden<AdminAuditList>('admin_audit').items[0]!)
    expect(row.action).toBe('выданы дни')
    expect(row.details).toBe('days: 30')
    expect(row.admin).toBe(1)
    expect(row.ip).not.toBe('—')
  })

  it('служебные действия раскрываются', () => {
    expect(actionTitle('denied:tariff.save')).toBe('отказ по праву: тариф сохранён')
    expect(actionTitle('approve:secret')).toBe('подтверждено «Да»: approve:secret'.replace('approve:secret', 'secret'))
    expect(actionTitle('что-то новое')).toBe('что-то новое')
  })
})
