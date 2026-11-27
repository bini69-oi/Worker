<script lang="ts">
  // Подключение тремя шагами: устройство, приложение, «Добавить подписку». На последнем шаге QR для другого устройства.
  import Async from '../lib/Async.svelte'
  import { useT } from '../lib/context'
  import Qr from '../lib/Qr.svelte'
  import { openExternal } from '../lib/telegram'
  import { createLoader } from '../lib/view'
  import type { AppView } from '../lib/types.gen'
  import { useApp } from './context'
  import { PLATFORMS, detectPlatform, type Platform } from './platform'
  import Screen from './Screen.svelte'

  const t = useT()
  const app = useApp()
  const { ep, router, tg, win } = app
  const me = app.me

  const loader = createLoader(() => ep.connect())

  let step = $state(1)
  let platform = $state<Platform>(detectPlatform(win.navigator?.userAgent ?? ''))
  let chosen = $state('')
  let copied = $state(false)

  function pick(apps: AppView[]): AppView | undefined {
    return apps.find((a) => a.id === chosen) ?? apps.find((a) => a.stores[platform]) ?? apps[0]
  }

  async function copy(link: string) {
    try {
      await win.navigator.clipboard.writeText(link)
      copied = true
      setTimeout(() => (copied = false), 2000)
    } catch {
      copied = false // буфер закрыт: ссылка остаётся на экране, её можно выделить
    }
  }
</script>

<Screen title={$t('app.connect_title')}>
  {#if $me.status === 'ready' && !$me.data.sub}
    <div class="card stack">
      <p>{$t('app.connect_nosub')}</p>
      <button class="btn wide" type="button" onclick={() => router.go('/buy')}>{$t('app.home_buy')}</button>
    </div>
  {:else}
    <Async view={$loader} retry={() => loader.reload()}>
      {#snippet children(c)}
        {#if !c.sub_url}
          <div class="card stack">
            <p>{$t('app.connect_pending')}</p>
            <button class="btn wide soft" type="button" onclick={() => loader.reload()}>{$t('app.connect_check')}</button>
          </div>
        {:else}
          <ol class="steps" aria-label={$t('app.connect_title')}>
            {#each [1, 2, 3] as n (n)}
              <li class:on={step === n} class:done={step > n}>{$t('app.connect_step_' + n)}</li>
            {/each}
          </ol>

          {#if step === 1}
            <div class="card stack">
              <h2>{$t('app.connect_pick_platform')}</h2>
              <div class="choices" role="radiogroup">
                {#each PLATFORMS as p (p)}
                  <button class="choice" class:sel={platform === p} type="button" role="radio" aria-checked={platform === p} onclick={() => (platform = p)}>
                    {$t('app.platform_' + p)}
                  </button>
                {/each}
              </div>
              <button class="btn wide" type="button" onclick={() => (step = 2)}>{$t('app.connect_next')}</button>
            </div>
          {:else if step === 2}
            <div class="card stack">
              <h2>{$t('app.connect_pick_app')}</h2>
              <div class="apps" role="radiogroup">
                {#each c.apps as a (a.id)}
                  {@const sel = pick(c.apps)?.id === a.id}
                  <div class="app" class:sel>
                    <button class="appname" type="button" role="radio" aria-checked={sel} onclick={() => (chosen = a.id)}>{a.name}</button>
                    {#if a.stores[platform]}
                      <button class="btn soft small" type="button" onclick={() => openExternal(tg, win, a.stores[platform] ?? '')}>{$t('app.connect_store')}</button>
                    {:else}
                      <span class="hint">{$t('app.connect_no_store')}</span>
                    {/if}
                  </div>
                {/each}
              </div>
              <button class="btn wide" type="button" onclick={() => (step = 3)}>{$t('app.connect_next')}</button>
              <button class="btn wide line" type="button" onclick={() => (step = 1)}>{$t('app.back')}</button>
            </div>
          {:else}
            {@const a = pick(c.apps)}
            <div class="card stack">
              {#if a}
                <button class="btn wide" type="button" onclick={() => openExternal(tg, win, a.open_url)}>{$t('app.connect_add', { app: a.name })}</button>
              {/if}
              <div class="qrbox">
                <div class="qrimg"><Qr value={c.sub_url} label={$t('app.connect_qr_label')} /></div>
                <p class="hint">{$t('app.connect_qr_hint')}</p>
              </div>
              <button class="btn wide soft" type="button" onclick={() => copy(c.sub_url)}>{copied ? $t('app.connect_copied') : $t('app.connect_copy')}</button>
              <button class="btn wide line" type="button" onclick={() => (step = 2)}>{$t('app.back')}</button>
            </div>
          {/if}
        {/if}
      {/snippet}
    </Async>
  {/if}
</Screen>

<style>
  .steps {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 6px;
    margin: 0 0 4px;
    padding: 0;
    list-style: none;
    counter-reset: s;
  }
  .steps li {
    counter-increment: s;
    padding-top: 8px;
    border-top: 3px solid var(--line);
    font-size: 13px;
    color: var(--hint);
  }
  .steps li::before {
    content: counter(s) '. ';
  }
  .steps li.on,
  .steps li.done {
    border-top-color: var(--accent);
  }
  .steps li.on {
    color: var(--text);
    font-weight: 650;
  }
  .choices,
  .apps {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .choice,
  .app {
    appearance: none;
    border: 1.5px solid var(--line);
    border-radius: var(--radius-sm);
    background: var(--surface);
    color: var(--text);
    font: 600 16px/1.2 var(--font);
  }
  .choice {
    min-height: 52px;
    padding: 0 16px;
    text-align: left;
    cursor: pointer;
  }
  .choice.sel,
  .app.sel {
    border-color: var(--accent);
    background: color-mix(in srgb, var(--accent) 10%, var(--surface));
  }
  .app {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 6px 8px 6px 4px;
  }
  .appname {
    appearance: none;
    flex: 1;
    min-height: 44px;
    padding: 0 12px;
    border: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .small {
    min-height: 40px;
    padding: 0 14px;
    font-size: 14px;
  }
  .qrbox {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    text-align: center;
  }
  .qrimg {
    width: min(100%, 260px);
    padding: 8px;
    border-radius: var(--radius-sm);
    background: #fff;
  }
</style>
