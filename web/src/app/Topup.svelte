<script lang="ts">
  // Пополнение: готовые суммы или своя, потом кассы. Stars видны только внутри Telegram.
  import Async from '../lib/Async.svelte'
  import { ApiError } from '../lib/api'
  import { useT } from '../lib/context'
  import { closePopup, finishPopup, openInvoice, openPopup } from '../lib/telegram'
  import { money } from '../lib/texts'
  import type { MeResponse, TopupResponse } from '../lib/types.gen'
  import { useApp } from './context'
  import Screen from './Screen.svelte'

  interface Props {
    params: Record<string, string>
  }
  let { params }: Props = $props()
  const t = useT()
  const app = useApp()
  const { ep, router, tg, win } = app
  const me = app.me

  /** Предел своей суммы в рублях, как на сервере. */
  const MAX_RUB = 1_000_000

  const amount = $derived(Number(params.amount ?? 0))
  const lang = $derived($me.status === 'ready' ? $me.data.lang : 'ru')
  let own = $state('')
  let busy = $state(false)
  let failure = $state('')
  let manual = $state<TopupResponse | null>(null)

  function ownAmount(): number {
    const n = Number(own)
    return Number.isInteger(n) && n >= 1 && n <= MAX_RUB && own.trim() === String(n) ? n * 100 : 0
  }

  function show(e: unknown) {
    if (e instanceof ApiError && e.limit) {
      failure = $t('app.topup_limits', { min: money($t, e.limit.min, lang), max: e.limit.max > 0 ? money($t, e.limit.max, lang) : $t('app.unlimited') })
    } else {
      failure = e instanceof ApiError ? `${e.message} (${e.code})` : $t('app.offline')
    }
  }

  async function create(gateway: string) {
    busy = true
    failure = ''
    // В браузере окно оплаты открывается тут же, до ответа сервера, иначе его блокирует Safari.
    const popup = openPopup(tg, win)
    let used = false
    try {
      const r = await ep.topup(gateway, amount)
      if (r.invoice_link && tg) {
        const status = await openInvoice(tg, r.invoice_link)
        if (status === 'paid' || status === 'pending') router.go(`/pay/${r.payment_id}`)
        else if (status === 'failed') failure = $t('err.internal')
      } else if (r.url) {
        used = true
        finishPopup(popup, tg, win, r.url)
        router.go(`/pay/${r.payment_id}`)
      } else if (r.details) {
        manual = r
      } else {
        router.go(`/pay/${r.payment_id}`)
      }
    } catch (e) {
      show(e)
    } finally {
      if (!used) closePopup(popup)
      busy = false
    }
  }

  function gateways(m: MeResponse) {
    return m.gateways.filter((g) => g.name !== 'stars' || tg !== null)
  }
</script>

<Screen title={$t('app.topup_title')}>
  <Async view={$me} retry={() => me.reload()}>
    {#snippet children(m)}
      {#if manual}
        <div class="card stack">
          <p class="details">{manual.details}</p>
          <button class="btn wide" type="button" onclick={() => router.go(`/pay/${manual?.payment_id}`)}>{$t('app.topup_check')}</button>
        </div>
      {:else if amount === 0}
        <div class="card stack">
          <h2>{$t('app.topup_amount')}</h2>
          <div class="presets">
            {#each m.topup_presets as p (p)}
              <button class="preset num" type="button" onclick={() => router.go(`/topup/${p}`)}>{money($t, p, m.lang)}</button>
            {/each}
          </div>
          <label class="stack">
            <span class="hint">{$t('app.topup_own')}</span>
            <input class="field num" inputmode="numeric" autocomplete="off" bind:value={own} placeholder="500" aria-invalid={own !== '' && ownAmount() === 0} />
          </label>
          {#if own !== '' && ownAmount() === 0}<p class="error" role="alert">{$t('app.topup_bad_amount', { max: MAX_RUB })}</p>{/if}
          <button class="btn wide" type="button" disabled={ownAmount() === 0} onclick={() => router.go(`/topup/${ownAmount()}`)}>{$t('app.connect_next')}</button>
        </div>
      {:else}
        {@const list = gateways(m)}
        <div class="card stack">
          <h2>{$t('app.topup_gateways', { amount: money($t, amount, m.lang) })}</h2>
          {#each list as g (g.name)}
            <button class="gw" type="button" disabled={busy} onclick={() => create(g.name)}>{g.title}</button>
          {:else}
            <p class="hint">{$t('app.topup_no_gateways')}</p>
          {/each}
          {#if failure}<p class="error" role="alert">{failure}</p>{/if}
        </div>
      {/if}
    {/snippet}
  </Async>
</Screen>

<style>
  .presets {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 8px;
  }
  .preset,
  .gw {
    appearance: none;
    min-height: 54px;
    padding: 0 16px;
    border: 1.5px solid var(--line);
    border-radius: var(--radius-sm);
    background: var(--surface);
    color: var(--text);
    font: 650 17px/1 var(--font);
    text-align: left;
    cursor: pointer;
  }
  .preset:active,
  .gw:active:not(:disabled) {
    background: var(--surface-2);
  }
  .gw:disabled {
    opacity: 0.5;
  }
  .details {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
