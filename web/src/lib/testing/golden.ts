// Эталоны ответов API из api/testdata: их пишут тесты Go, тесты фронта берут их как ответы сервера.
// Только для тестов, в сборку не попадает.
const files = import.meta.glob('../../../../api/testdata/*.json', { eager: true, import: 'default' })

export function golden<T>(name: string): T {
  const key = Object.keys(files).find((k) => k.endsWith(`/testdata/${name}.json`))
  if (key === undefined) {
    throw new Error(`нет эталона api/testdata/${name}.json: запустите go test ./api -update`)
  }
  return files[key] as T
}
