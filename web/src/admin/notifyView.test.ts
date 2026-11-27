import { describe, expect, it } from 'vitest'
import type { AdminNotify } from '../lib/types.gen'
import { golden } from '../lib/testing/golden'
import { bindHint, notifyView } from './notifyView'

describe('уведомления на эталоне API', () => {
  it('группа привязана, семь тем, у безопасности нет выключателя', () => {
    const v = notifyView(golden<AdminNotify>('admin_notifications'))
    expect(v.bound).toBe(true)
    expect(v.state).toBe('Группа привязана')
    expect(v.topics).toHaveLength(7)
    const sec = v.topics.find((t) => t.id === 'security')!
    expect(sec.canToggle).toBe(false)
    expect(sec.enabled).toBe(true)
    expect(v.topics.filter((t) => t.canToggle)).toHaveLength(6)
  })

  it('подсказка с кодом', () => {
    expect(bindHint('abc123')).toBe('Добавьте бота в группу-форум и отправьте там: /bind abc123')
  })
})
