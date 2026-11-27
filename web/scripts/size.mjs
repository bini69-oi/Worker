// Считает вес JS и CSS кабинета и админки в gzip по манифесту сборки и падает, если больше бюджета:
// кабинет 80 КБ, админка 200 КБ.
import { readFileSync } from 'node:fs'
import { gzipSync } from 'node:zlib'

const BUDGET = 80 * 1024
const ADMIN_BUDGET = 200 * 1024
const dir = new URL('../build/', import.meta.url)
const manifest = JSON.parse(readFileSync(new URL('.vite/manifest.json', dir), 'utf8'))

function closure(entry) {
  const files = new Set()
  const seen = new Set()
  const walk = (key) => {
    if (seen.has(key)) return
    seen.add(key)
    const chunk = manifest[key]
    if (!chunk) throw new Error(`в манифесте нет ${key}`)
    files.add(chunk.file)
    for (const css of chunk.css ?? []) files.add(css)
    for (const imp of chunk.imports ?? []) walk(imp)
  }
  walk(entry)
  return [...files].filter((f) => /\.(js|css)$/.test(f))
}

function weigh(entry) {
  let total = 0
  for (const f of closure(entry)) total += gzipSync(readFileSync(new URL(f, dir))).length
  return total
}

const kb = (n) => `${(n / 1024).toFixed(1)} КБ`
const app = weigh('app/index.html')
const admin = weigh('admin/index.html')
console.log(`кабинет: ${kb(app)} из ${kb(BUDGET)} (gzip), админка: ${kb(admin)} из ${kb(ADMIN_BUDGET)}`)
if (app > BUDGET) {
  console.error(`кабинет тяжелее бюджета: ${kb(app)} > ${kb(BUDGET)}`)
  process.exit(1)
}
if (admin > ADMIN_BUDGET) {
  console.error(`админка тяжелее бюджета: ${kb(admin)} > ${kb(ADMIN_BUDGET)}`)
  process.exit(1)
}
