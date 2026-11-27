import { mount } from 'svelte'
import '../lib/theme.css'
import '../lib/base.css'
import { createApi } from '../lib/api'
import { TEXTS } from '../lib/context'
import { createRouter } from '../lib/router'
import { bindBackButton, loadTelegram, watchTheme } from '../lib/telegram'
import { createTexts } from '../lib/texts'
import { followSystemTheme } from '../lib/theme'
import App from './App.svelte'
import { APP, type AppCtx } from './context'
import { endpoints } from './endpoints'
import { createSession } from './session'

async function start() {
  const root = document.documentElement
  const tg = await loadTelegram(window)
  if (tg) watchTheme(tg, root)
  else followSystemTheme(window, root)

  const texts = createTexts()
  texts.lang.subscribe((l) => (root.lang = l))
  const router = createRouter(window)
  if (tg) bindBackButton(tg, router)

  let storage: Storage | null = null
  try {
    storage = window.sessionStorage
  } catch {
    storage = null // закрыт в приватном режиме: токен живёт только в памяти страницы
  }
  const session = createSession({ initData: tg?.initData || null, storage, texts })
  const api = createApi({ base: '/api/app', token: () => session.token(), onUnauthorized: () => session.expired() })
  const ep = endpoints(api)
  session.attach(ep)

  const ctx: AppCtx = {
    ep,
    router,
    tg,
    win: window,
    session,
    get me() {
      return session.me!
    },
  }
  mount(App, {
    target: document.getElementById('root')!,
    context: new Map<unknown, unknown>([
      [TEXTS, texts.t],
      [APP, ctx],
    ]),
  })
  await session.boot()
}

void start()
