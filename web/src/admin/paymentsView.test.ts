import { describe, expect, it } from 'vitest'
import type { AdminPaymentList } from '../lib/types.gen'
import { golden } from '../lib/testing/golden'
import { payView } from './paymentsView'

describe('платежи на эталоне API', () => {
  const list = golden<AdminPaymentList>('admin_payments_list')

  it('ждущий ручной можно подтвердить, возврата нет', () => {
    const v = list.items.map((p) => payView(p, true))
    expect(v.length).toBeGreaterThan(0)
    for (const row of v) {
      expect(row.status).toBe('ожидает')
      expect(row.canConfirm).toBe(true)
      expect(row.canRefund).toBe(false)
      expect(row.user).toMatch(/^@user/)
    }
  })

  it('возврат только у оплаченного Stars и только владельцу', () => {
    const p = { ...list.items[0]!, gateway: 'stars', status: 'paid' }
    expect(payView(p, true).canRefund).toBe(true)
    expect(payView(p, false).canRefund).toBe(false)
    expect(payView({ ...p, status: 'refunded' }, true).canRefund).toBe(false)
  })
})
