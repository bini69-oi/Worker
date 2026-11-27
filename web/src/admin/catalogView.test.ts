import { describe, expect, it } from 'vitest'
import type { AdminPromoList, AdminTariffList } from '../lib/types.gen'
import { golden } from '../lib/testing/golden'
import { emptyTariff, formToTariff, promoView, squadChoices, tariffToForm } from './catalogView'

describe('тарифы на эталоне API', () => {
  const list = golden<AdminTariffList>('admin_tariffs')

  it('тариф проходит форму туда и обратно без потерь', () => {
    const t = list.tariffs[0]!
    const back = formToTariff(tariffToForm(t))
    expect('ok' in back && back.ok).toEqual(t)
  })

  it('сквады панели с галочками, чужой сквад виден', () => {
    const t = list.tariffs[0]!
    const ch = squadChoices(list.squads, [...t.squads, 'old-uuid'])
    expect(ch.find((c) => c.uuid === 'sq-1')?.on).toBe(true)
    expect(ch.find((c) => c.uuid === 'sq-2')?.on).toBe(false)
    expect(ch.at(-1)?.name).toContain('нет в панели')
  })

  it('форма отказывает на плохих числах', () => {
    const f = { ...emptyTariff(), name: 'Месяц', periods: [{ days: '30', price: '200' }] }
    expect('ok' in formToTariff(f)).toBe(true)
    expect(formToTariff({ ...f, name: ' ' })).toHaveProperty('error')
    expect(formToTariff({ ...f, devices: 'много' })).toHaveProperty('error')
    expect(formToTariff({ ...f, periods: [{ days: '30', price: 'дёшево' }] })).toHaveProperty('error')
    expect(formToTariff({ ...f, periods: [] })).toHaveProperty('error')
  })

  it('цена 250 ₽ уходит как 25000 копеек', () => {
    const f = { ...emptyTariff(), name: 'Месяц', periods: [{ days: '30', price: '250' }] }
    const r = formToTariff(f)
    expect('ok' in r && r.ok.periods[0]?.price).toBe(25000)
  })
})

describe('промокоды на эталоне API', () => {
  it('срок, лимит и ссылка', () => {
    const p = golden<AdminPromoList>('admin_promos').items[0]!
    const v = promoView(p)
    expect(v.code).toBe('SPRING26')
    expect(v.gives).toBe('7 дн.')
    expect(v.uses).toBe('0 из 100')
    expect(v.until).toBe('без срока')
    expect(v.link).toBe('https://t.me/test_bot?start=promo_SPRING26')
  })
})
