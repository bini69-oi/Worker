<script lang="ts">
  // Промокод: ввод, результат (деньги или дни) или ошибка с кодом.
  import { ApiError } from '../lib/api'
  import { useT } from '../lib/context'
  import { money } from '../lib/texts'
  import type { PromoResponse } from '../lib/types.gen'
  import { useApp } from './context'
  import Screen from './Screen.svelte'

  const t = useT()
  const app = useApp()
  const { ep } = app
  const me = app.me
  const lang = $derived($me.status === 'ready' ? $me.data.lang : 'ru')

  let code = $state('')
  let busy = $state(false)
  let failure = $state('')
  let result = $state<PromoResponse | null>(null)

  async function apply(ev: Event) {
    ev.preventDefault()
    if (busy || code.trim() === '') return
    busy = true
    failure = ''
    result = null
    try {
      result = await ep.promo(code.trim())
      code = ''
      await me.refresh()
    } catch (e) {
      failure = e instanceof ApiError ? `${e.message} (${e.code})` : $t('app.offline')
    } finally {
      busy = false
    }
  }
</script>

<Screen title={$t('app.promo_title')}>
  <form class="card stack" onsubmit={apply}>
    <label class="stack">
      <span class="hint">{$t('app.promo_label')}</span>
      <input class="field code" autocomplete="off" autocapitalize="characters" spellcheck="false" maxlength="64" bind:value={code} />
    </label>
    <button class="btn wide" type="submit" disabled={busy || code.trim() === ''}>{$t('app.promo_apply')}</button>
    {#if failure}<p class="error" role="alert">{failure}</p>{/if}
    {#if result}
      <p class="ok" role="status">
        {result.kind === 'money' ? $t('app.promo_money', { amount: money($t, result.value, lang) }) : $t('app.promo_days', { days: result.value })}
      </p>
    {/if}
  </form>
</Screen>

<style>
  .code {
    font-family: var(--mono);
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  .ok {
    color: var(--ok);
    font-weight: 650;
  }
</style>
