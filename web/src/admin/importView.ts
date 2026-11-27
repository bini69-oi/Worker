import type { AdminImportReport } from '../lib/types.gen'

// importView это отчёт пробного прогона: сколько будет заведено, кого пропустят и почему.
export function importView(r: AdminImportReport) {
  return {
    summary: `Будет заведено подписок: ${r.subs}, из них новых абонентов: ${r.new_users}`,
    reasons: r.reasons.map((x) => `${x.reason}: ${x.count}`),
    examples: r.skipped.map((s) => `${s.username || '(без имени)'}: ${s.reason}`),
    // Записывать есть что, только если что-то будет заведено.
    canApply: r.subs > 0,
  }
}
