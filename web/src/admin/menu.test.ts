import { describe, expect, it } from 'vitest'
import type { AdminMe } from '../lib/types.gen'
import { golden } from '../lib/testing/golden'
import { sections, visible } from './menu'

describe('меню по правам', () => {
  it('помощник с правами users и support видит только Абонентов', () => {
    const helper = golden<AdminMe>('admin_me_helper')
    expect(helper.perms).toEqual(['users', 'support'])
    expect(visible(helper).map((s) => s.title)).toEqual(['Абоненты'])
  })

  it('владелец видит все двенадцать разделов', () => {
    const owner = golden<AdminMe>('admin_me_owner')
    expect(visible(owner)).toHaveLength(12)
    expect(visible(owner).map((s) => s.id)).toEqual(sections.map((s) => s.id))
  })

  it('раздел только владельца не открывается правом', () => {
    const me: AdminMe = { tg_id: 5, owner: false, perms: ['users', 'grant', 'ban', 'promo', 'tariffs', 'broadcast', 'stats', 'support', 'owner'] }
    const ids = visible(me).map((s) => s.id)
    for (const id of ['texts', 'settings', 'gateways', 'notifications', 'team', 'audit']) expect(ids).not.toContain(id)
    expect(ids).toEqual(['overview', 'users', 'payments', 'tariffs', 'promos', 'broadcasts'])
  })

  it('каждый раздел меню имеет экран', async () => {
    const { screens } = await import('./screens')
    for (const s of sections) expect(screens[s.id], s.id).toBeDefined()
  })
})
