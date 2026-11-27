<script lang="ts">
  // Устройства: список, удалить одно, сбросить все. Удаление просит второе нажатие, список обновляется после ответа сервера.
  import Async from '../lib/Async.svelte'
  import { ApiError } from '../lib/api'
  import { useT } from '../lib/context'
  import { date, devices as limitText } from '../lib/texts'
  import { createLoader } from '../lib/view'
  import { useApp } from './context'
  import Screen from './Screen.svelte'

  const t = useT()
  const app = useApp()
  const { ep } = app
  const me = app.me
  const loader = createLoader(() => ep.devices())

  /** Что ждёт второго нажатия: hwid устройства, 'all' для сброса или ничего. */
  let asking = $state('')
  let busy = $state(false)
  let failure = $state('')
  const lang = $derived($me.status === 'ready' ? $me.data.lang : 'ru')
  const limit = $derived($me.status === 'ready' && $me.data.sub ? $me.data.sub.devices : 0)

  async function run(action: () => Promise<unknown>) {
    busy = true
    failure = ''
    try {
      await action()
      asking = ''
      await loader.refresh()
    } catch (e) {
      failure = e instanceof ApiError ? `${e.message} (${e.code})` : $t('app.offline')
    } finally {
      busy = false
    }
  }
</script>

<Screen title={$t('app.devices_title')}>
  <Async view={$loader} retry={() => loader.reload()} isEmpty={(d) => d.devices.length === 0} emptyKey="app.devices_empty">
    {#snippet children(d)}
      <p class="hint">{$t('app.devices_count', { n: d.devices.length, limit: limitText($t, limit) })}</p>
      <ul class="list">
        {#each d.devices as dev (dev.hwid)}
          <li class="card row">
            <div class="info">
              <b>{dev.model || dev.platform || '?'}</b>
              <span class="hint">{[dev.platform, date(dev.created_at, lang)].filter(Boolean).join(' · ')}</span>
            </div>
            {#if asking === dev.hwid}
              <button class="btn danger small" type="button" disabled={busy} onclick={() => run(() => ep.deleteDevice(dev.hwid))}>{$t('app.devices_confirm')}</button>
            {:else}
              <button class="btn line small" type="button" onclick={() => (asking = dev.hwid)}>{$t('app.devices_delete')}</button>
            {/if}
          </li>
        {/each}
      </ul>
      {#if asking === 'all'}
        <button class="btn danger wide" type="button" disabled={busy} onclick={() => run(() => ep.resetDevices())}>{$t('app.devices_reset_confirm')}</button>
      {:else}
        <button class="btn line wide" type="button" onclick={() => (asking = 'all')}>{$t('app.devices_reset')}</button>
      {/if}
      {#if failure}<p class="error" role="alert">{failure}</p>{/if}
    {/snippet}
  </Async>
</Screen>

<style>
  .list {
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .info {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .info b {
    overflow-wrap: anywhere;
  }
  .small {
    min-height: 40px;
    padding: 0 14px;
    font-size: 14px;
    flex-shrink: 0;
  }
</style>
