<script lang="ts">
  // Подписка: тариф, срок, лимиты, автопродление, ждущая смена тарифа с отменой, докупка трафика и устройства.
  import Async from '../lib/Async.svelte'
  import { actionKey, withKey } from '../lib/actionKey'
  import { ApiError } from '../lib/api'
  import { useT } from '../lib/context'
  import { date, devices, money, traffic } from '../lib/texts'
  import { useApp } from './context'
  import Screen from './Screen.svelte'

  const t = useT()
  const app = useApp()
  const { ep, router, tg } = app
  const me = app.me

  /** Что ждёт второго нажатия: 'cancel' (отмена смены), 'revoke' (новая ссылка), 'all' (выйти везде). */
  // Ключ повтора на каждое действие: при обрыве связи нажатие повторяется с тем же ключом.
  const keys: Record<string, ReturnType<typeof actionKey>> = {}
  const keyFor = (name: string) => (keys[name] ??= actionKey())
  let asking = $state('')
  let busy = $state(false)
  let failure = $state('')
  let missing = $state(0)

  async function run(action: () => Promise<unknown>) {
    if (busy) return
    busy = true
    failure = ''
    missing = 0
    try {
      await action()
      asking = ''
      await me.refresh()
    } catch (e) {
      if (e instanceof ApiError && e.money) missing = e.money.missing
      failure = e instanceof ApiError ? `${e.message} (${e.code})` : $t('app.offline')
    } finally {
      busy = false
    }
  }
</script>

<Screen title={$t('app.sub_title')}>
  <Async view={$me} retry={() => me.reload()}>
    {#snippet children(m)}
      {@const s = m.sub}
      {#if !s}
        <div class="card stack">
          <p>{$t('app.home_none_hint')}</p>
          <button class="btn wide" type="button" onclick={() => router.go('/buy')}>{$t('app.home_buy')}</button>
        </div>
      {:else}
        <div class="card stack">
          <div class="head">
            <h2>{s.tariff || $t(s.imported ? 'app.sub_imported' : 'app.sub_trial')}</h2>
            <span class="pill {s.state}">{$t('app.home_status_' + s.state)}</span>
          </div>
          <dl class="facts">
            <div><dt>{$t('app.sub_until')}</dt><dd class="num">{date(s.expires_at, m.lang)}</dd></div>
            <div><dt>{$t('app.sub_traffic')}</dt><dd>{traffic($t, s.traffic_bytes)}</dd></div>
            <div><dt>{$t('app.sub_devices')}</dt><dd>{devices($t, s.devices)}</dd></div>
          </dl>
          <div class="two">
            <button class="btn soft" type="button" onclick={() => router.go('/connect')}>{$t('app.home_connect')}</button>
            <button class="btn" type="button" onclick={() => router.go('/buy')}>{$t('app.home_renew')}</button>
          </div>
          {#if !s.trial && !s.imported}
            <button class="btn line wide" type="button" disabled={busy} onclick={() => run(() => ep.autorenew(!s.auto_renew))}>
              {$t(s.auto_renew ? 'app.sub_autorenew_off' : 'app.sub_autorenew_on')}
            </button>
          {/if}
        </div>

        {#if s.next}
          <div class="card stack next">
            <p>{$t('app.sub_next', { date: date(s.next.starts_at, m.lang), tariff: s.next.tariff })}</p>
            {#if asking === 'cancel'}
              <button class="btn danger wide" type="button" disabled={busy} onclick={() => run(() => withKey(keyFor('cancel'), (k) => ep.cancelPlan(k)))}>{$t('app.sub_cancel_confirm')}</button>
            {:else}
              <button class="btn line wide" type="button" onclick={() => (asking = 'cancel')}>{$t('app.sub_cancel')}</button>
            {/if}
          </div>
        {/if}

        {#if m.addons.traffic.length > 0 || m.addons.device_price > 0}
          <div class="card stack">
            <h2>{$t('app.sub_addons')}</h2>
            {#each m.addons.traffic as p (p.gb)}
              <button class="addon" type="button" disabled={busy} onclick={() => run(() => withKey(keyFor('traffic' + p.gb), (k) => ep.buyTraffic(p.gb, k)))}>
                <span>{$t('app.sub_add_traffic', { gb: p.gb })}</span><b class="num">{money($t, p.price, m.lang)}</b>
              </button>
            {/each}
            {#if m.addons.device_price > 0}
              <button class="addon" type="button" disabled={busy} onclick={() => run(() => withKey(keyFor('device'), (k) => ep.buyDevice(k)))}>
                <span>{$t('app.sub_add_device')}</span><b class="num">{money($t, m.addons.device_price, m.lang)}</b>
              </button>
            {/if}
            <p class="hint">{$t('app.sub_addons_hint')}</p>
          </div>
        {/if}

        {#if failure}
          <div class="stack">
            <p class="error" role="alert">{failure}</p>
            {#if missing > 0}
              <button class="btn wide" type="button" onclick={() => router.go(`/topup/${missing}`)}>{$t('app.buy_topup_diff', { amount: money($t, missing, m.lang) })}</button>
            {/if}
          </div>
        {/if}

        <div class="card stack">
          {#if asking === 'revoke'}
            <button class="btn danger wide" type="button" disabled={busy} onclick={() => run(() => ep.revokeLink())}>{$t('app.sub_revoke_confirm')}</button>
          {:else}
            <button class="btn line wide" type="button" onclick={() => (asking = 'revoke')}>{$t('app.sub_revoke')}</button>
          {/if}
          <p class="hint">{$t('app.sub_revoke_hint')}</p>
        </div>

        {#if !tg}
          <div class="stack">
            <button class="btn quiet wide" type="button" onclick={() => app.session.logout(false)}>{$t('app.logout')}</button>
            {#if asking === 'all'}
              <button class="btn danger wide" type="button" onclick={() => app.session.logout(true)}>{$t('app.logout_all_confirm')}</button>
            {:else}
              <button class="btn quiet wide" type="button" onclick={() => (asking = 'all')}>{$t('app.logout_all')}</button>
            {/if}
          </div>
        {/if}
      {/if}
    {/snippet}
  </Async>
</Screen>

<style>
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
  }
  .pill {
    padding: 4px 10px;
    border-radius: 999px;
    font-size: 13px;
    font-weight: 650;
    background: color-mix(in srgb, var(--ok) 16%, transparent);
    color: var(--ok);
  }
  .pill.trial {
    background: color-mix(in srgb, var(--warn) 18%, transparent);
    color: var(--warn);
  }
  .pill.expired {
    background: var(--surface-2);
    color: var(--hint);
  }
  .facts {
    margin: 0;
  }
  .facts div {
    display: flex;
    justify-content: space-between;
    padding: 9px 0;
    border-bottom: 1px solid var(--line);
  }
  .facts dt {
    color: var(--hint);
  }
  .facts dd {
    margin: 0;
    font-weight: 600;
  }
  .two {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
  }
  .next {
    border-color: var(--accent);
  }
  .addon {
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
  .addon:disabled {
    opacity: 0.5;
  }
</style>
