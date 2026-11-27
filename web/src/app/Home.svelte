<script lang="ts">
  // Главная: статус подписки, дни до конца, главное действие, баланс и разделы.
  import Async from '../lib/Async.svelte'
  import { ApiError } from '../lib/api'
  import { useT } from '../lib/context'
  import { openChat } from '../lib/telegram'
  import { date, money } from '../lib/texts'
  import { useApp } from './context'

  const t = useT()
  const app = useApp()
  const me = app.me
  const { ep, router } = app

  let busy = $state(false)
  let failure = $state('')

  async function startTrial() {
    busy = true
    failure = ''
    try {
      await ep.trial()
      await me.refresh()
      router.go('/connect')
    } catch (e) {
      failure = e instanceof ApiError ? `${e.message} (${e.code})` : $t('app.offline')
    } finally {
      busy = false
    }
  }

  const tiles = [
    ['/sub', 'app.tile_sub', 'M12 3l8 4v5c0 4.5-3.2 8.3-8 9-4.8-.7-8-4.5-8-9V7z'],
    ['/devices', 'app.tile_devices', 'M7 3h10a1 1 0 011 1v16a1 1 0 01-1 1H7a1 1 0 01-1-1V4a1 1 0 011-1zm5 15h.01'],
    ['/ref', 'app.tile_ref', 'M16 11a3 3 0 100-6 3 3 0 000 6zM8 12a3 3 0 100-6 3 3 0 000 6zm0 2c-3 0-5 1.5-5 3.5V19h10v-1.5C13 15.5 11 14 8 14zm8 0c-.6 0-1.2.1-1.7.2 1.1.9 1.7 2 1.7 3.3V19h5v-1.5c0-2-2-3.5-5-3.5z'],
    ['/promo', 'app.tile_promo', 'M4 8a2 2 0 012-2h12a2 2 0 012 2v2a2 2 0 000 4v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2a2 2 0 000-4zm8-2v12'],
    ['/history', 'app.tile_history', 'M12 7v5l3 2M4 12a8 8 0 108-8 8 8 0 00-6 2.7M4 4v4h4'],
  ] as const
</script>

<Async view={$me} retry={() => me.reload()}>
  {#snippet children(m)}
    <main class="home">
      <div class="top">
        <span class="brand" aria-hidden="true"></span>
        <button class="chip" type="button" onclick={() => router.go('/topup')}>
          <span class="hint">{$t('app.home_balance')}</span>
          <b class="num">{money($t, m.balance, m.lang)}</b>
        </button>
      </div>

      <section class="hero card" class:expired={m.sub?.state === 'expired'}>
        {#if m.sub}
          <span class="status {m.sub.state}">{$t('app.home_status_' + m.sub.state)}</span>
          {#if m.sub.state !== 'expired'}
            <p class="days num"><b>{m.sub.days_left}</b> <span>{$t('app.days_unit')}</span></p>
          {/if}
          <p class="hint">{$t('app.home_until', { date: date(m.sub.expires_at, m.lang) })}{m.sub.tariff ? ` · ${m.sub.tariff}` : ''}</p>
        {:else}
          <span class="status none">{$t('app.home_status_none')}</span>
          <p class="lead">{$t('app.home_none_hint')}</p>
        {/if}

        <div class="actions stack">
          {#if m.sub && m.sub.state !== 'expired'}
            <button class="btn wide" type="button" onclick={() => router.go('/connect')}>{$t('app.home_connect')}</button>
          {:else if m.sub}
            <button class="btn wide" type="button" onclick={() => router.go('/buy')}>{$t('app.home_renew')}</button>
          {:else}
            {#if m.trial_available}
              <button class="btn wide" type="button" disabled={busy} onclick={startTrial}>{$t('app.home_trial')}</button>
            {/if}
            <button class="btn wide" class:soft={m.trial_available} type="button" onclick={() => router.go('/buy')}>{$t('app.home_buy')}</button>
          {/if}
          {#if failure}<p class="error" role="alert">{failure}</p>{/if}
        </div>
      </section>

      <nav class="tiles" aria-label="menu">
        {#each tiles as [to, key, icon] (to)}
          <button class="tile" type="button" onclick={() => router.go(to)}>
            <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true"><path d={icon} fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" /></svg>
            <span>{$t(key)}</span>
          </button>
        {/each}
        {#if m.bot_link}
          <button class="tile" type="button" onclick={() => openChat(app.tg, app.win, m.bot_link)}>
            <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true"><path d="M21 12a8 8 0 01-11.6 7.1L4 20l1-4.6A8 8 0 1121 12z" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" /></svg>
            <span>{$t('app.tile_support')}</span>
          </button>
        {/if}
      </nav>
    </main>
  {/snippet}
</Async>

<style>
  .home {
    display: flex;
    flex-direction: column;
    gap: 16px;
    max-width: 520px;
    margin: 0 auto;
    padding: max(16px, env(safe-area-inset-top)) 16px max(32px, env(safe-area-inset-bottom));
  }
  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .brand {
    width: 34px;
    height: 34px;
    border-radius: 11px;
    background: var(--accent);
    box-shadow: 7px 7px 0 -3px var(--surface-2);
  }
  .chip {
    appearance: none;
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 8px 14px;
    border: 1px solid var(--line);
    border-radius: 999px;
    background: var(--surface);
    color: var(--text);
    font: 500 15px/1 var(--font);
    cursor: pointer;
  }
  .hero {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 22px 20px 20px;
  }
  .status {
    align-self: flex-start;
    padding: 4px 10px;
    border-radius: 999px;
    font-size: 13px;
    font-weight: 650;
    background: color-mix(in srgb, var(--ok) 16%, transparent);
    color: var(--ok);
  }
  .status.trial {
    background: color-mix(in srgb, var(--warn) 18%, transparent);
    color: var(--warn);
  }
  .status.expired,
  .status.none {
    background: var(--surface-2);
    color: var(--hint);
  }
  .days {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin-top: 6px;
  }
  .days b {
    font-size: 64px;
    line-height: 0.95;
    font-weight: 750;
    letter-spacing: -0.04em;
  }
  .days span {
    font-size: 20px;
    color: var(--hint);
  }
  .lead {
    margin-top: 6px;
    font-size: 17px;
  }
  .actions {
    margin-top: 16px;
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
    gap: 10px;
  }
  .tile {
    appearance: none;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 14px;
    padding: 14px;
    border: 1px solid var(--line);
    border-radius: var(--radius);
    background: var(--surface);
    color: var(--text);
    font: 600 15px/1.2 var(--font);
    text-align: left;
    cursor: pointer;
  }
  .tile svg {
    color: var(--accent);
  }
  .tile:active {
    background: var(--surface-2);
  }
</style>
