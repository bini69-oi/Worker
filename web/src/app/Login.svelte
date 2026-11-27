<script lang="ts">
  // Вход в браузере: сайт показывает код, абонент открывает бота и нажимает такой же код среди трёх.
  import { onDestroy, onMount } from 'svelte'
  import { useT } from '../lib/context'
  import { ApiError } from '../lib/api'
  import type { LoginStartResponse } from '../lib/types.gen'
  import type { Endpoints } from './endpoints'

  interface Props {
    ep: Pick<Endpoints, 'loginStart' | 'loginPoll'>
    /** Бот подтвердил вход, cookie уже стоит. */
    onDone: () => void
    /** Пауза между опросами, мс. */
    every?: number
  }
  let { ep, onDone, every = 2000 }: Props = $props()
  const t = useT()

  type Phase = 'starting' | 'waiting' | 'rejected' | 'expired' | 'failed'
  let phase = $state<Phase>('starting')
  let login = $state<LoginStartResponse | null>(null)
  let failure = $state('')
  let timer: ReturnType<typeof setTimeout> | undefined
  let alive = true

  async function start() {
    clearTimeout(timer)
    phase = 'starting'
    try {
      login = await ep.loginStart()
    } catch (e) {
      failure = e instanceof ApiError ? e.message : ''
      phase = 'failed'
      return
    }
    phase = 'waiting'
    schedule()
  }

  function schedule() {
    timer = setTimeout(poll, every)
  }

  async function poll() {
    if (!alive || !login || phase !== 'waiting') return
    if (Date.now() > new Date(login.expires_at).getTime()) {
      phase = 'expired'
      return
    }
    try {
      const r = await ep.loginPoll(login.id)
      if (!alive) return
      if (r.state === 'confirmed') return onDone()
      if (r.state === 'rejected' || r.state === 'expired') {
        phase = r.state
        return
      }
    } catch {
      // связь моргнула: опрос продолжается, окно входа всё равно ограничено пятью минутами
    }
    schedule()
  }

  onMount(start)
  onDestroy(() => {
    alive = false
    clearTimeout(timer)
  })
</script>

<main class="login">
  <div class="mark" aria-hidden="true"></div>
  <h1>{$t('app.login_title')}</h1>

  {#if phase === 'starting'}
    <p class="hint" role="status">{$t('app.loading')}</p>
  {:else if phase === 'failed'}
    <p class="error" role="alert">{failure || $t('app.offline')}</p>
    <button class="btn wide" type="button" onclick={start}>{$t('app.retry')}</button>
  {:else if phase === 'rejected' || phase === 'expired'}
    <p class="error" role="alert">{$t(phase === 'rejected' ? 'app.login_rejected' : 'app.login_expired')}</p>
    <button class="btn wide" type="button" onclick={start}>{$t('app.login_restart')}</button>
  {:else if login}
    <ol class="steps hint">
      <li>{$t('app.login_step1')}</li>
      <li>{$t('app.login_step2')}</li>
    </ol>
    <div class="plate">
      <span class="hint">{$t('app.login_code_label')}</span>
      <span class="code num" data-testid="code">{login.code}</span>
    </div>
    <a class="btn wide" href={login.link} target="_blank" rel="noopener noreferrer">{$t('app.login_button')}</a>
    <p class="hint wait" role="status"><span class="dot" aria-hidden="true"></span>{$t('app.login_waiting')}</p>
  {/if}
</main>

<style>
  .login {
    display: flex;
    flex-direction: column;
    gap: 18px;
    max-width: 420px;
    margin: 0 auto;
    padding: max(28px, env(safe-area-inset-top)) 20px 32px;
    min-height: 100vh;
    justify-content: center;
  }
  .mark {
    width: 44px;
    height: 44px;
    border-radius: 14px;
    background: var(--accent);
    box-shadow: 10px 10px 0 -4px var(--surface-2);
  }
  .steps {
    margin: 0;
    padding-left: 20px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .plate {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 22px 16px;
    border-radius: var(--radius);
    background: var(--surface);
    border: 1px solid var(--line);
  }
  .code {
    font: 700 56px/1 var(--mono);
    letter-spacing: 0.12em;
    padding-left: 0.12em;
  }
  .wait {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    animation: pulse 1.2s ease-in-out infinite;
  }
  @keyframes pulse {
    50% {
      opacity: 0.25;
    }
  }
</style>
