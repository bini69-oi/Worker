// Что экраны кабинета берут из контекста: вызовы API, роутер, данные главной, Telegram.
import { getContext, setContext } from 'svelte'
import type { Router } from '../lib/router'
import type { TgWebApp } from '../lib/telegram'
import type { Loader } from '../lib/view'
import type { MeResponse } from '../lib/types.gen'
import type { Endpoints } from './endpoints'
import type { Session } from './session'

export interface AppCtx {
  ep: Endpoints
  router: Router
  /** Главная: абонент, подписка, баланс, кассы. Экраны, что меняют деньги или подписку, зовут me.refresh(). Есть, когда кабинет открыт. */
  readonly me: Loader<MeResponse>
  tg: TgWebApp | null
  win: Window
  session: Session
}

const APP = Symbol('app')

export function provideApp(c: AppCtx): void {
  setContext(APP, c)
}

export function useApp(): AppCtx {
  return getContext<AppCtx>(APP)
}

export { APP }
