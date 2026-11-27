import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vitest/config'

// Две точки входа в одной сборке: кабинет (/app/) и админка (/admin/). Общие куски лежат в src/lib
// и попадают в общие файлы build/assets. Пути относительные: сервер сам решает, под каким адресом отдавать.
export default defineConfig({
  base: './',
  plugins: [svelte()],
  build: {
    outDir: 'build',
    emptyOutDir: false, // build чистит scripts/clean.mjs: в нём лежит README.txt из git
    manifest: true, // по манифесту scripts/size.mjs считает вес кабинета
    rollupOptions: {
      input: {
        app: 'app/index.html',
        admin: 'admin/index.html',
      },
    },
  },
  test: {
    include: ['src/**/*.test.ts'],
  },
})
