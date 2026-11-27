<script lang="ts">
  // Покупка: тариф, срок, итог с ценой и балансом. Не хватает денег: недостача и «Пополнить на разницу».
  import Async from '../lib/Async.svelte'
  import { ApiError } from '../lib/api'
  import { useT } from '../lib/context'
  import { date, devices, money, traffic } from '../lib/texts'
  import { createLoader } from '../lib/view'
  import type { QuoteResponse, TariffView } from '../lib/types.gen'
  import { useApp } from './context'
  import Screen from './Screen.svelte'

  interface Props {
    params: Record<string, string>
  }
  let { params }: Props = $props()
  const t = useT()
  const app = useApp()
  const { ep, router } = app
  const me = app.me

  const tariffs = createLoader(() => ep.tariffs())
  const tariffId = $derived(Number(params.tariff ?? 0))
  const days = $derived(Number(params.days ?? 0))
  const lang = $derived($me.status === 'ready' ? $me.data.lang : 'ru')

  let quote = $state<{ view: 'loading' } | { view: 'ready'; q: QuoteResponse } | { view: 'error'; text: string }>({ view: 'loading' })
  let busy = $state(false)
  let failure = $state('')

  $effect(() => {
    if (tariffId > 0 && days > 0) void loadQuote(tariffId, days)
  })

  async function loadQuote(id: number, d: number) {
    quote = { view: 'loading' }
    try {
      quote = { view: 'ready', q: await ep.quote(id, d) }
    } catch (e) {
      quote = { view: 'error', text: e instanceof ApiError ? `${e.message} (${e.code})` : $t('app.offline') }
    }
  }

  const cheapest = (tr: TariffView) => Math.min(...tr.periods.map((p) => p.price))

  async function pay() {
    busy = true
    failure = ''
    try {
      const r = await ep.buy(tariffId, days)
      await me.refresh()
      router.go(r.queued ? '/sub' : '/connect')
    } catch (e) {
      if (e instanceof ApiError && e.money) {
        // Выбор запомнен сервером: после пополнения подписка выдастся сама.
        router.go(`/topup/${e.money.missing}`)
      } else {
        failure = e instanceof ApiError ? `${e.message} (${e.code})` : $t('app.offline')
      }
    } finally {
      busy = false
    }
  }
</script>

<Screen title={$t('app.buy_title')}>
  <Async view={$tariffs} retry={() => tariffs.reload()} isEmpty={(d) => d.tariffs.length === 0} emptyKey="app.buy_empty">
    {#snippet children(list)}
      {@const tr = list.tariffs.find((x) => x.id === tariffId)}
      {#if !tr}
        <div class="stack">
          {#each list.tariffs as x (x.id)}
            <button class="card plan" type="button" onclick={() => router.go(`/buy/${x.id}`)}>
              <span class="name">{x.name}</span>
              <span class="hint">{$t('app.buy_limits', { traffic: traffic($t, x.traffic_bytes), devices: devices($t, x.devices) })}</span>
              <span class="from num">{$t('app.buy_from', { price: money($t, cheapest(x), lang) })}</span>
            </button>
          {/each}
        </div>
      {:else if days === 0}
        <div class="card stack">
          <h2>{tr.name}</h2>
          <p class="hint">{$t('app.buy_limits', { traffic: traffic($t, tr.traffic_bytes), devices: devices($t, tr.devices) })}</p>
          <h2 class="sub">{$t('app.buy_period_title')}</h2>
          {#each tr.periods as p (p.days)}
            <button class="period" type="button" onclick={() => router.go(`/buy/${tr.id}/${p.days}`)}>
              <span>{$t('app.buy_days', { days: p.days })}</span>
              <b class="num">{money($t, p.price, lang)}</b>
            </button>
          {/each}
        </div>
      {:else}
        <div class="card stack">
          <h2>{tr.name}, {$t('app.buy_days', { days })}</h2>
          {#if quote.view === 'loading'}
            <p class="hint" role="status">{$t('app.loading')}</p>
          {:else if quote.view === 'error'}
            <p class="error" role="alert">{quote.text}</p>
            <button class="btn wide soft" type="button" onclick={() => loadQuote(tariffId, days)}>{$t('app.retry')}</button>
          {:else}
            {@const q = quote.q}
            <dl class="sum">
              <div><dt>{$t('app.buy_price')}</dt><dd class="num">{money($t, q.price, lang)}</dd></div>
              <div><dt>{$t('app.buy_balance')}</dt><dd class="num">{money($t, q.balance, lang)}</dd></div>
              {#if q.missing > 0}
                <div class="miss"><dt>{$t('app.buy_missing')}</dt><dd class="num">{money($t, q.missing, lang)}</dd></div>
              {/if}
            </dl>
            {#if q.queued && q.starts_at}
              <p class="hint">{$t('app.buy_queued', { date: date(q.starts_at, lang) })}</p>
            {/if}
            {#if q.missing > 0}
              <button class="btn wide" type="button" disabled={busy} onclick={pay}>{$t('app.buy_topup_diff', { amount: money($t, q.missing, lang) })}</button>
            {:else}
              <button class="btn wide" type="button" disabled={busy} onclick={pay}>{$t('app.buy_pay')}</button>
            {/if}
            {#if failure}<p class="error" role="alert">{failure}</p>{/if}
          {/if}
        </div>
      {/if}
    {/snippet}
  </Async>
</Screen>

<style>
  .plan {
    appearance: none;
    display: flex;
    flex-direction: column;
    gap: 4px;
    width: 100%;
    color: var(--text);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .plan:active {
    background: var(--surface-2);
  }
  .name {
    font-size: 19px;
    font-weight: 700;
  }
  .from {
    margin-top: 6px;
    font-weight: 650;
    color: var(--accent);
  }
  .sub {
    margin-top: 8px;
  }
  .period {
    appearance: none;
    display: flex;
    justify-content: space-between;
    align-items: center;
    min-height: 52px;
    padding: 0 16px;
    border: 1.5px solid var(--line);
    border-radius: var(--radius-sm);
    background: var(--surface);
    color: var(--text);
    font: 500 16px/1 var(--font);
    cursor: pointer;
  }
  .period:active {
    background: var(--surface-2);
  }
  .sum {
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .sum div {
    display: flex;
    justify-content: space-between;
    padding: 10px 0;
    border-bottom: 1px solid var(--line);
  }
  .sum dt {
    color: var(--hint);
  }
  .sum dd {
    margin: 0;
    font-weight: 650;
  }
  .sum .miss dd {
    color: var(--danger);
  }
</style>
