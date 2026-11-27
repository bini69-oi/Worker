import { describe, expect, it } from 'vitest'
import type { AdminImportReport } from '../lib/types.gen'
import { golden } from '../lib/testing/golden'
import { importView } from './importView'

describe('импорт из панели на эталоне API', () => {
  it('отчёт пробного прогона', () => {
    const v = importView(golden<AdminImportReport>('admin_import_preview'))
    expect(v.summary).toBe('Будет заведено подписок: 1, из них новых абонентов: 1')
    expect(v.reasons).toEqual(['нет Telegram ID: 1'])
    expect(v.examples).toEqual(['no_tg: нет Telegram ID'])
    expect(v.canApply).toBe(true)
  })

  it('нечего заводить: записать нельзя', () => {
    const v = importView({ subs: 0, new_users: 0, reasons: [], skipped: [{ username: '', reason: 'отключён в панели' }] })
    expect(v.canApply).toBe(false)
    expect(v.examples).toEqual(['(без имени): отключён в панели'])
  })
})
