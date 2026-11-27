// Чистит build перед сборкой, оставляя README.txt: он лежит в git, чтобы go:embed собирался без фронта.
import { readdirSync, rmSync } from 'node:fs'

const dir = new URL('../build/', import.meta.url)
for (const name of readdirSync(dir)) {
  if (name !== 'README.txt') rmSync(new URL(name, dir), { recursive: true, force: true })
}
