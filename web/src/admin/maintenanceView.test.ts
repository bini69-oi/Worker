import { describe, expect, it } from 'vitest'
import type { AdminMaintenance } from '../lib/types.gen'
import { golden } from '../lib/testing/golden'
import { maintenanceView } from './maintenanceView'

describe('техработы на эталоне API', () => {
  it('идут: кнопка выключает, текст виден', () => {
    const v = maintenanceView(golden<AdminMaintenance>('admin_maintenance'))
    expect(v.status).toContain('Техработы идут')
    expect(v.button).toBe('Выключить техработы')
    expect(v.next).toBe(false)
    expect(v.text).toBe('Работы до 15:00')
  })

  it('не идут: кнопка включает', () => {
    const v = maintenanceView({ on: false, text: 'x' })
    expect(v.button).toBe('Включить техработы')
    expect(v.next).toBe(true)
  })
})
