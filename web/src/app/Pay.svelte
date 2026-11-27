<script lang="ts">
  // Статус платежа: страница спрашивает сервер раз в 3 секунды до 10 минут. Сервер сам сверяет платёж с кассой.
  import { onDestroy, onMount } from 'svelte'
  import { ApiError } from '../lib/api'
  import { useT } from '../lib/context'
  import { openChat } from '../lib/telegram'
  import { money } from '../lib/texts'
  import type { PaymentResponse } from '../lib/types.gen'
  import { useApp } from './context'
  import Screen from './Screen.svelte'

  interface Props {
    params: Record<string, string>
    /** Пауза между опросами и предел ожидания, мс. Для тестов. */
    every?: number
    limit?: number
  }
  let { params, every = 3000, limit = 10 * 60 * 1000 }: Props = $props()
  const t = useT()
  const app = useApp()
  const { ep, router, tg, win } = app
  const me = app.me

  let payment = $state<PaymentResponse | null>(null)
  let failure = $state('')
  let gaveUp = $state(false)
  let offline = $state(false)
  let timer: ReturnType<typeof setTimeout> | undefined
  let alive = true
  const startedAt = Date.now()

  async function poll() {
    if (!alive) return
    try {
      payment = await ep.payment(Number(params.id))
      offline = false
      if (payment.status === 'paid') {
        void me.refresh()
        return
      }
      if (payment.status !== 'pending') return
    } catch (e) {
      if (e instanceof ApiError) {
        failure = `${e.message} (${e.code})`
        return
      }
      offline = true // связь моргнула: пробуем дальше
    }
    if (Date.now() - startedAt >= limit) {
      gaveUp = true
      return
    }
    timer = setTimeout(poll, every)
  }

  onMount(poll)
  onDestroy(() => {
    alive = false
    clearTimeout(timer)
  })
  const lang = $derived($me.status === 'ready' ? $me.data.lang : 'ru')
  const botLink = $derived($me.status === 'ready' ? $me.data.bot_link : '')
</script>

<Screen title={$t('app.pay_title')}>
  <div class="card stack center" role="status" aria-live="polite">
    {#if failure}
      <p class="error" role="alert">{failure}</p>
    {:else if payment?.status === 'paid'}
      <p class="big ok">{$t('app.pay_paid')}</p>
      <p class="num hint">{money($t, payment.amount, lang)}</p>
      <button class="btn wide" type="button" onclick={() => router.go('/')}>{$t('app.pay_home')}</button>
    {:else if payment && (payment.status === 'expired' || payment.status === 'failed' || payment.status === 'refunded')}
      <p class="big">{$t('app.pay_' + payment.status)}</p>
      <button class="btn wide" type="button" onclick={() => router.go('/topup')}>{$t('app.pay_again')}</button>
    {:else if gaveUp}
      <p class="big">{$t('app.pay_gave_up')}</p>
      {#if botLink}<button class="btn wide soft" type="button" onclick={() => openChat(tg, win, botLink)}>{$t('app.tile_support')}</button>{/if}
    {:else}
      <span class="spinner" aria-hidden="true"></span>
      <p class="big">{$t('app.pay_waiting')}</p>
      <p class="hint">{$t(offline ? 'app.offline' : 'app.pay_waiting_hint')}</p>
    {/if}
  </div>
</Screen>

<style>
  .center {
    align-items: center;
    text-align: center;
    padding: 28px 20px;
  }
  .big {
    font-size: 20px;
    font-weight: 700;
  }
  .ok {
    color: var(--ok);
  }
  .spinner {
    width: 30px;
    height: 30px;
    border-radius: 50%;
    border: 3px solid var(--line);
    border-top-color: var(--accent);
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
