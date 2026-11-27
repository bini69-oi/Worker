// QR-код в виде пути SVG. Картинку не строим: CSP страницы не пускает data:-адреса, а путь рисуется прямо в разметке.
import qrcode from 'qrcode-generator'

export interface Qr {
  /** Сторона в модулях вместе с белой каймой. */
  size: number
  /** Путь SVG: по квадрату на каждый тёмный модуль. */
  path: string
}

const QUIET = 4 // кайма по стандарту: четыре модуля

/** QR для текста. Уровень исправления ошибок M: ссылка подписки длинная, а читаться должна с экрана. */
export function makeQr(text: string): Qr {
  const q = qrcode(0, 'M') // тип 0: размер по длине данных
  q.addData(text, 'Byte')
  q.make()
  const n = q.getModuleCount()
  let path = ''
  for (let r = 0; r < n; r++) {
    for (let c = 0; c < n; c++) {
      if (q.isDark(r, c)) path += `M${c + QUIET} ${r + QUIET}h1v1h-1z`
    }
  }
  return { size: n + QUIET * 2, path }
}
